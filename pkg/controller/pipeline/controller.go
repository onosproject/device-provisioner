// SPDX-FileCopyrightText: 2022-present Intel Corporation
//
// SPDX-License-Identifier: Apache-2.0

// Package pipeline configuration controller
package pipeline

import (
	"context"
	"github.com/onosproject/device-provisioner/pkg/controller/utils"
	configstore "github.com/onosproject/device-provisioner/pkg/store/configs"
	"github.com/onosproject/device-provisioner/pkg/store/topo"
	provisionerapi "github.com/onosproject/onos-api/go/onos/provisioner"
	topoapi "github.com/onosproject/onos-api/go/onos/topo"
	"github.com/onosproject/onos-lib-go/pkg/controller/v2"
	"github.com/onosproject/onos-lib-go/pkg/errors"
	"github.com/onosproject/onos-lib-go/pkg/logging"
	"github.com/onosproject/onos-net-lib/pkg/p4rtclient"
	"github.com/onosproject/onos-net-lib/pkg/p4utils"
	"github.com/onosproject/onos-net-lib/pkg/realm"
	p4info "github.com/p4lang/p4runtime/go/p4/config/v1"
	p4api "github.com/p4lang/p4runtime/go/p4/v1"
	"google.golang.org/protobuf/encoding/prototext"
	"sync"

	"time"
)

var log = logging.GetLogger()

const (
	defaultTimeout      = 45 * time.Second
	provisionerRoleName = "provisioner"
	queryPeriod         = 10 * time.Second
	pipelineKind        = "pipeline"
	queueSize           = 100
)

// NewManager returns a new pipeline controller manager
func NewManager(topo topo.Store, conns p4rtclient.ConnManager, configStore configstore.ConfigStore, realmOptions *realm.Options) *Manager {
	manager := &Manager{
		conns:        conns,
		topo:         topo,
		configStore:  configStore,
		realmOptions: realmOptions,
	}
	return manager

}

// Manager reconciles pipeline configuration
type Manager struct {
	conns        p4rtclient.ConnManager
	topo         topo.Store
	configStore  configstore.ConfigStore
	realmOptions *realm.Options
	cancel       context.CancelFunc
	mu           sync.Mutex
}

// Start starts new reconciler
func (m *Manager) Start() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.cancel != nil {
		return nil
	}
	pipelineController := controller.NewController(m.reconcile)

	eventCh := make(chan topoapi.Event, queueSize)
	ctx, cancel := context.WithCancel(context.Background())

	filter := utils.RealmQueryFilter(m.realmOptions)
	err := m.topo.Watch(ctx, eventCh, filter)
	if err != nil {
		cancel()
		return err
	}
	m.cancel = cancel
	go func() {
		for event := range eventCh {
			if _, ok := event.Object.Obj.(*topoapi.Object_Entity); ok {
				err := pipelineController.Reconcile(event.Object.ID)
				if err != nil {
					log.Warnw("Failed to reconcile object", "objectID", event.Object.ID, "error", err)
				}

			}
		}
	}()
	return nil
}

// Reconcile reconciles device pipeline config
func (m *Manager) reconcile(ctx context.Context, request controller.Request[topoapi.ID]) controller.Directive[topoapi.ID] {
	targetID := request.ID
	log.Infow("Reconciling device pipeline config", "targetID", targetID)

	target, err := m.topo.Get(ctx, targetID)
	if err != nil {
		if !errors.IsNotFound(err) {
			log.Warnw("Failed reconciling device pipeline config", "targetID", targetID, "error", err)
			return request.Retry(err)
		}
		return request.Ack()
	}

	err = m.reconcilePipelineConfiguration(ctx, target)
	if err != nil {
		log.Warnw("Failed reconciling device pipeline configuration", "targetID", targetID, "error", err)
		return request.Retry(err)
	}

	return request.Ack()
}

func (m *Manager) reconcilePipelineConfiguration(ctx context.Context, target *topoapi.Object) error {
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
		log.Warnw("Pipeline config state aspect not found", "targetID", targetID, "error", err)
		pcState.ConfigID = deviceConfigAspect.PipelineConfigID
		pcState.Updated = time.Now()
		pcState.Status.State = provisionerapi.ConfigStatus_PENDING
		pcState.Cookie = 0
		err = utils.UpdateObjectAspect(ctx, m.topo, target, pipelineKind, pcState)
		if err != nil {
			return err
		}
		return nil
	}

	if pcState.ConfigID != deviceConfigAspect.PipelineConfigID {
		pcState.ConfigID = deviceConfigAspect.PipelineConfigID
		pcState.Updated = time.Now()
		pcState.Status.State = provisionerapi.ConfigStatus_PENDING
		pcState.Cookie = 0
		err = utils.UpdateObjectAspect(ctx, m.topo, target, pipelineKind, pcState)
		if err != nil {
			return err
		}
		return nil
	}

	if pcState.Status.State != provisionerapi.ConfigStatus_PENDING {
		log.Infow("Device Pipeline config state is not in Pending state", "targetID", targetID, "ConfigState", pcState.Status.State)
		return nil
	}

	// Otherwise... get the pipeline config artifacts
	artifacts, err := utils.GetArtifacts(ctx, m.configStore, deviceConfigAspect.PipelineConfigID, 2)
	if err != nil {
		log.Warnw("Failed to retrieve artifacts", "targetID", targetID, "pipelineConfigID", deviceConfigAspect.PipelineConfigID, "error", err)
		return err
	}

	stratumAgents := &topoapi.StratumAgents{}
	if err := target.GetAspect(stratumAgents); err != nil {
		log.Warnw("Failed to extract stratum agents aspect", "error", err)
		return err
	}

	p4rtConn, err := m.conns.GetByTarget(ctx, target.ID)
	if err != nil {
		log.Warnw("Connection not found for target", "target ID", target.ID)
		return err
	}

	role := p4utils.NewStratumRole(provisionerRoleName, 0, []byte{}, false, true)
	arbitrationResponse, err := p4rtConn.PerformMasterArbitration(ctx, role)
	if err != nil {
		log.Warnw("Failed to perform master arbitration", "error", err)
		return err
	}

	electionID := arbitrationResponse.Arbitration.ElectionId
	log.Info(electionID)
	// ask for the pipeline config cookie
	gr, err := p4rtConn.GetForwardingPipelineConfig(ctx, &p4api.GetForwardingPipelineConfigRequest{
		DeviceId:     stratumAgents.DeviceID,
		ResponseType: p4api.GetForwardingPipelineConfigRequest_COOKIE_ONLY,
	})
	if err != nil {
		log.Warnw("Failed to retrieve pipeline configuration", "error", err)
		return err
	}

	// if that matches our cookie, we're good
	if pcState.Cookie == gr.Config.Cookie.Cookie && pcState.Cookie > 0 {
		return nil
	}

	info := artifacts[provisionerapi.P4InfoType]
	binary := artifacts[provisionerapi.P4BinaryType]
	// otherwise unmarshal the P4Info
	p4i := &p4info.P4Info{}
	if err = prototext.Unmarshal(info, p4i); err != nil {
		log.Warnw("Failed to unmarshal p4info", "error", err)
		return err
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
		log.Warnw("Failed to Set forwarding pipeline config", "targetID", targetID, "error", err)
		return err
	}

	// Update PipelineConfigState aspect
	pcState.ConfigID = deviceConfigAspect.PipelineConfigID
	pcState.Updated = time.Now()
	pcState.Status.State = provisionerapi.ConfigStatus_APPLIED
	pcState.Cookie = newCookie
	err = utils.UpdateObjectAspect(ctx, m.topo, target, "pipeline", pcState)
	if err != nil {
		return err
	}
	log.Infow("Device pipeline config is set successfully", "targetID", targetID, "Status", pcState.Status.State)
	return nil
}
