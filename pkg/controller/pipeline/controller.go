// SPDX-FileCopyrightText: 2022-present Intel Corporation
//
// SPDX-License-Identifier: Apache-2.0

// Package pipeline configuration controller
package pipeline

import (
	"context"
	"github.com/onosproject/device-provisioner/pkg/controller/utils"
	configstore "github.com/onosproject/device-provisioner/pkg/store/pipelineconfig"
	"github.com/onosproject/device-provisioner/pkg/store/topo"
	provisionerapi "github.com/onosproject/onos-api/go/onos/provisioner"
	topoapi "github.com/onosproject/onos-api/go/onos/topo"
	"github.com/onosproject/onos-lib-go/pkg/controller"
	"github.com/onosproject/onos-lib-go/pkg/errors"
	"github.com/onosproject/onos-lib-go/pkg/logging"
	"github.com/onosproject/onos-net-lib/pkg/p4rtclient"
	"github.com/onosproject/onos-net-lib/pkg/p4utils"
	p4info "github.com/p4lang/p4runtime/go/p4/config/v1"
	p4api "github.com/p4lang/p4runtime/go/p4/v1"
	"google.golang.org/protobuf/encoding/prototext"

	"time"
)

var log = logging.GetLogger()

const (
	defaultTimeout      = 30 * time.Second
	provisionerRoleName = "provisioner"
	requeueTimeout      = 2 * time.Minute
)

// NewController returns a new pipeline and chassis configuration controller
func NewController(topo topo.Store, conns p4rtclient.ConnManager, configStore configstore.ConfigStore, realmLabel string, realmValue string) *controller.Controller {
	c := controller.NewController("configuration")
	c.Watch(&TopoWatcher{
		topo:       topo,
		realmValue: realmValue,
		realmLabel: realmLabel,
	})

	c.Reconcile(&Reconciler{
		topo:        topo,
		conns:       conns,
		configStore: configStore,
	})
	return c
}

// Reconciler reconciles pipeline configuration
type Reconciler struct {
	conns       p4rtclient.ConnManager
	topo        topo.Store
	configStore configstore.ConfigStore
}

// Reconcile reconciles device pipeline config
func (r *Reconciler) Reconcile(id controller.ID) (controller.Result, error) {
	ctx, cancel := context.WithTimeout(context.Background(), defaultTimeout)
	defer cancel()

	targetID := id.Value.(topoapi.ID)
	log.Infow("Reconciling device pipeline config", "targetID", targetID)

	target, err := r.topo.Get(ctx, targetID)
	if err != nil {
		if !errors.IsNotFound(err) {
			log.Warnw("Failed reconciling device pipeline config", "targetID", targetID, "error", err)
			return controller.Result{}, err
		}
		return controller.Result{}, nil
	}

	err = r.reconcilePipelineConfiguration(ctx, target)
	if err != nil {
		log.Warnw("Failed reconciling device pipeline configuration", "targetID", targetID, "error", err)
		return controller.Result{}, err
	}

	return controller.Result{
		RequeueAfter: requeueTimeout,
	}, nil
}

func (r *Reconciler) reconcilePipelineConfiguration(ctx context.Context, target *topoapi.Object) error {
	targetID := target.ID
	deviceConfigAspect := &provisionerapi.DeviceConfig{}
	err := target.GetAspect(deviceConfigAspect)
	if err != nil {
		log.Warnw("Failed retrieving device config aspect", "targetID", targetID, "error", err)
		return err
	}

	if deviceConfigAspect.PipelineConfigID == "" {
		log.Warnw("Pipeline config ID is not set", "targetID", targetID)
		return nil
	}

	pcState := &provisionerapi.PipelineConfigState{}
	err = target.GetAspect(pcState)
	if err != nil {
		pcState.ConfigID = deviceConfigAspect.PipelineConfigID
		pcState.Updated = time.Now()
		pcState.Status.State = provisionerapi.ConfigStatus_PENDING
		err = utils.UpdateObjectAspect(ctx, r.topo, target, "pipeline", pcState)
		if err != nil {
			return err
		}
	}

	if pcState.ConfigID != deviceConfigAspect.PipelineConfigID {
		pcState.ConfigID = deviceConfigAspect.PipelineConfigID
		pcState.Updated = time.Now()
		pcState.Status.State = provisionerapi.ConfigStatus_PENDING
		err = utils.UpdateObjectAspect(ctx, r.topo, target, "pipeline", pcState)
		if err != nil {
			return err
		}
		return nil
	}

	if pcState.Status.State != provisionerapi.ConfigStatus_PENDING {
		log.Debugw("Device Pipeline config state is not in Pending state", "ConfigState", pcState.Status.State)
		return nil
	}

	// Otherwise... get the pipeline config artifacts
	artifacts, err := utils.GetArtifacts(ctx, r.configStore, deviceConfigAspect.PipelineConfigID, 2)
	if err != nil {
		log.Warnw("Failed to retrieve artifacts", "targetID", targetID, "pipelineConfigID", deviceConfigAspect.PipelineConfigID, "error", err)
		return err
	}

	newCookie, err := r.setPipelineConfig(ctx, target, artifacts[provisionerapi.P4InfoType], artifacts[provisionerapi.P4BinaryType], pcState.Cookie)
	if err != nil {
		log.Warnw("Failed reconcile Stratum P4Runtime pipeline config", "targetID", targetID, "error", err)
		pcState.ConfigID = deviceConfigAspect.PipelineConfigID
		pcState.Updated = time.Now()
		pcState.Status.State = provisionerapi.ConfigStatus_FAILED
		pcState.Cookie = 0
		err = utils.UpdateObjectAspect(ctx, r.topo, target, "pipeline", pcState)
		if err != nil {
			return err
		}
		return nil
	}

	if newCookie == pcState.Cookie {
		return nil
	}

	// Update PipelineConfigState aspect
	pcState.ConfigID = deviceConfigAspect.PipelineConfigID
	pcState.Updated = time.Now()
	pcState.Status.State = provisionerapi.ConfigStatus_APPLIED
	pcState.Cookie = newCookie
	err = utils.UpdateObjectAspect(ctx, r.topo, target, "pipeline", pcState)
	if err != nil {
		return err
	}
	log.Infow("Device pipeline config is set successfully", "targetID", targetID)
	return nil
}

// setPipelineConfig makes sure that the device has the given P4 pipeline configuration applied
func (r *Reconciler) setPipelineConfig(ctx context.Context, target *topoapi.Object, info []byte, binary []byte, cookie uint64) (uint64, error) {
	stratumAgents := &topoapi.StratumAgents{}
	if err := target.GetAspect(stratumAgents); err != nil {
		log.Warnw("Failed to extract stratum agents aspect", "error", err)
		return 0, err
	}

	p4rtConn, err := r.conns.GetByTarget(ctx, target.ID)
	if err != nil {
		log.Warnw("Connection not found for target", "target ID", target.ID)
		return 0, err
	}

	role := p4utils.NewStratumRole(provisionerRoleName, 0, []byte{}, false, true)
	arbitrationResponse, err := p4rtConn.PerformMasterArbitration(role)
	if err != nil {
		log.Warnw("Failed to perform master arbitration", "error", err)
		return 0, err
	}
	electionID := arbitrationResponse.Arbitration.ElectionId
	// ask for the pipeline config cookie
	gr, err := p4rtConn.GetForwardingPipelineConfig(ctx, &p4api.GetForwardingPipelineConfigRequest{
		DeviceId:     stratumAgents.DeviceID,
		ResponseType: p4api.GetForwardingPipelineConfigRequest_COOKIE_ONLY,
	})
	if err != nil {
		log.Warnw("Failed to retrieve pipeline configuration", "error", err)
		return 0, err
	}

	// if that matches our cookie, we're good
	if cookie == gr.Config.Cookie.Cookie && cookie > 0 {
		return cookie, nil
	}

	// otherwise unmarshal the P4Info
	p4i := &p4info.P4Info{}
	if err = prototext.Unmarshal(info, p4i); err != nil {
		log.Warnw("Failed to unmarshal p4info", "error", err)
		return 0, err
	}

	// and then apply it to the device
	newCookie := uint64(time.Now().UnixNano())
	_, err = p4rtConn.SetForwardingPipelineConfig(ctx, &p4api.SetForwardingPipelineConfigRequest{
		DeviceId:   stratumAgents.DeviceID,
		Role:       provisionerRoleName,
		ElectionId: electionID,
		Action:     p4api.SetForwardingPipelineConfigRequest_VERIFY_AND_COMMIT,
		Config: &p4api.ForwardingPipelineConfig{
			P4Info:         p4i,
			P4DeviceConfig: binary,
			Cookie:         &p4api.ForwardingPipelineConfig_Cookie{Cookie: newCookie},
		},
	})
	if err != nil {
		return 0, err
	}
	log.Infow("pipeline configured", "targetID", target.ID, "cookie", newCookie)
	return newCookie, nil
}
