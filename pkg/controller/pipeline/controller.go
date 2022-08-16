// SPDX-FileCopyrightText: 2022-present Intel Corporation
//
// SPDX-License-Identifier: Apache-2.0

package pipeline

import (
	"context"
	"github.com/onosproject/device-provisioner/pkg/pluginregistry"
	pipelineConfigStore "github.com/onosproject/device-provisioner/pkg/store/pipelineconfig"
	"github.com/onosproject/device-provisioner/pkg/store/topo"
	p4rtapi "github.com/onosproject/onos-api/go/onos/p4rt/v1"
	topoapi "github.com/onosproject/onos-api/go/onos/topo"
	"github.com/onosproject/onos-lib-go/pkg/controller"
	"github.com/onosproject/onos-lib-go/pkg/errors"
	"github.com/onosproject/onos-lib-go/pkg/logging"
	"github.com/onosproject/onos-p4-sdk/pkg/p4rt/admin"
	p4configapi "github.com/p4lang/p4runtime/go/p4/config/v1"
	p4api "github.com/p4lang/p4runtime/go/p4/v1"
	"google.golang.org/protobuf/proto"
	"time"
)

var log = logging.GetLogger()

const (
	defaultTimeout = 30 * time.Second
)

// NewController returns a new device pipeline pipelineconfig controller
func NewController(topo topo.Store, pipelineConfigStore pipelineConfigStore.Store, adminController *admin.Controller) *controller.Controller {
	c := controller.NewController("pipeline")
	c.Watch(&TopoWatcher{
		topo: topo,
	})
	c.Watch(&Watcher{
		pipelineConfigs: pipelineConfigStore,
	})
	c.Reconcile(&Reconciler{
		topo:                topo,
		pipelineConfigStore: pipelineConfigStore,
		adminController:     adminController,
	})
	return c
}

// Reconciler reconciles device pipeline config
type Reconciler struct {
	topo                topo.Store
	p4PluginRegistry    pluginregistry.P4PluginRegistry
	pipelineConfigStore pipelineConfigStore.Store
	adminController     *admin.Controller
}

// Reconcile reconciles pipeline configuration
func (r *Reconciler) Reconcile(id controller.ID) (controller.Result, error) {
	ctx, cancel := context.WithTimeout(context.Background(), defaultTimeout)
	defer cancel()

	pipelineConfigID := id.Value.(p4rtapi.PipelineConfigID)
	pipelineConfig, err := r.pipelineConfigStore.Get(ctx, pipelineConfigID)
	if err != nil {
		if !errors.IsNotFound(err) {
			log.Warnw("Failed to reconcile Pipeline Configuration", "pipelineConfig ID", pipelineConfigID, "error", err)
			return controller.Result{}, err
		}
		log.Warnw("Failed to reconcile pipeline configuration; Pipeline Configuration not found", "pipelineConfigID", pipelineConfigID)
		return controller.Result{}, nil
	}

	log.Infow("Reconcile device pipeline configuration", "pipelineConfig ID", pipelineConfig.ID, "targetID", pipelineConfig.TargetID)
	if pipelineConfig.Spec == nil || len(pipelineConfig.Spec.P4Info) == 0 {
		log.Errorw("Failed Reconciling device pipeline config", "pipelineConfig ID", pipelineConfig.ID, "targetID", pipelineConfig.TargetID, "error", err)
		return controller.Result{}, nil
	}

	log.Infow("Reconciling Device Pipeline Config", "pipelineConfig ID", pipelineConfig.ID, "targetID", pipelineConfig.TargetID)

	switch pipelineConfig.Action {
	case p4rtapi.ConfigurationAction_VERIFY_AND_COMMIT:
		log.Infow("Reconciling device pipeline config for VERIFY_AND_COMMIT action", "pipelineConfig ID", pipelineConfig.ID, "targetID", pipelineConfig.TargetID)
		return r.reconcileVerifyAndCommitAction(ctx, pipelineConfig)
	}

	return controller.Result{}, nil

}
func (r *Reconciler) reconcileVerifyAndCommitAction(ctx context.Context, pipelineConfig *p4rtapi.PipelineConfig) (controller.Result, error) {
	targetID := topoapi.ID(pipelineConfig.TargetID)
	target, err := r.topo.Get(ctx, targetID)
	if err != nil {
		if !errors.IsNotFound(err) {
			log.Errorw("Failed Reconciling device pipeline config", "pipelineConfig ID", pipelineConfig.ID, "targetID", targetID, "error", err)
			return controller.Result{}, err
		}
		pipelineConfig.Status.State = p4rtapi.PipelineConfigStatus_UNKNOWN
		pipelineConfig.Status.Mastership.Master = ""
		pipelineConfig.Status.Mastership.Term = 0
		if err := r.updateConfigurationStatus(ctx, pipelineConfig); err != nil {
			return controller.Result{}, err
		}

		return controller.Result{}, nil
	}
	p4rtServerInfo := &topoapi.P4RTServerInfo{}
	err = target.GetAspect(p4rtServerInfo)
	if err != nil {
		log.Errorw("Failed Reconciling device pipeline config", "pipelineConfig ID", pipelineConfig.ID, "targetID", targetID, "error", err)
		return controller.Result{}, err
	}

	mastership := topoapi.P4RTMastershipState{}
	_ = target.GetAspect(&mastership)
	mastershipTerm := p4rtapi.MastershipTerm(mastership.Term)

	if mastershipTerm > pipelineConfig.Status.Mastership.Term {
		log.Infow("Mastership is changed; Pipeline Configuration state is changing to PENDING", "pipelineConfig ID", pipelineConfig.ID, "targetID", targetID)
		pipelineConfig.Status.State = p4rtapi.PipelineConfigStatus_PENDING
		pipelineConfig.Status.Mastership.Master = mastership.NodeId
		pipelineConfig.Status.Mastership.Term = mastershipTerm
		if err := r.updateConfigurationStatus(ctx, pipelineConfig); err != nil {
			return controller.Result{}, err
		}
		return controller.Result{}, nil
	}

	if pipelineConfig.Status.State != p4rtapi.PipelineConfigStatus_PENDING {
		log.Warnw("Failed reconciling device pipeline config", "pipelineConfig ID", pipelineConfig.ID, "targetID", pipelineConfig.TargetID, "state", pipelineConfig.Status.State)
		return controller.Result{}, nil
	}

	// If the master node ID is not set, skip reconciliation.
	if mastership.NodeId == "" {
		log.Infow("No master for target", "pipelineConfig ID", pipelineConfig.ID, "targetID", pipelineConfig.TargetID)
		return controller.Result{}, nil
	}

	if len(p4rtServerInfo.Pipelines) == 0 {
		log.Errorw("No pipeline information found for target", "pipelineConfig ID", pipelineConfig.ID, "targetID", pipelineConfig.TargetID, "error", err)
		return controller.Result{}, errors.NewNotFound("Device pipeline config info is not found", targetID)
	}

	targetClient, err := r.adminController.Client(ctx, targetID)
	if err != nil {
		log.Warn(err)
		return controller.Result{}, nil
	}

	p4InfoBytes := pipelineConfig.Spec.P4Info
	p4DeviceConfig := pipelineConfig.Spec.P4DeviceConfig

	p4Info := &p4configapi.P4Info{}
	err = proto.Unmarshal(p4InfoBytes, p4Info)
	if err != nil {
		log.Errorw("Failed reconciling device pipeline config", "pipelineConfig ID", pipelineConfig.ID, "targetID", pipelineConfig.TargetID, "error", err)
		return controller.Result{}, err
	}

	pipelineConfigResponse, err := targetClient.SetForwardingPipelineConfig(ctx, &admin.PipelineConfigSpec{
		P4Info:         p4Info,
		P4DeviceConfig: p4DeviceConfig,
		Action:         p4api.SetForwardingPipelineConfigRequest_VERIFY_AND_COMMIT,
	})

	if err != nil {
		log.Errorw("Failed Reconciling device pipeline config", "pipelineConfig ID", pipelineConfig.ID, "targetID", pipelineConfig.TargetID, "error", err)
		pipelineConfig.Status.State = p4rtapi.PipelineConfigStatus_FAILED
		if err := r.updateConfigurationStatus(ctx, pipelineConfig); err != nil {
			return controller.Result{}, err
		}
		return controller.Result{}, nil
	}
	log.Infow("Pipeline config response", "pipeline config response", pipelineConfigResponse)
	pipelineConfig.Status.State = p4rtapi.PipelineConfigStatus_COMPLETE
	pipelineConfig.Status.Mastership.Master = mastership.NodeId
	pipelineConfig.Status.Mastership.Term = mastershipTerm
	if err := r.updateConfigurationStatus(ctx, pipelineConfig); err != nil {
		return controller.Result{}, err
	}
	log.Infow("Device pipelineConfig is Set Successfully", "pipelineConfig ID", pipelineConfig.ID, "targetID", pipelineConfig.TargetID)
	return controller.Result{}, nil
}

func (r *Reconciler) updateConfigurationStatus(ctx context.Context, pipelineConfig *p4rtapi.PipelineConfig) error {
	log.Debug(pipelineConfig.Status)
	err := r.pipelineConfigStore.UpdateStatus(ctx, pipelineConfig)
	if err != nil {
		if !errors.IsNotFound(err) && !errors.IsConflict(err) {
			log.Errorw("Failed updating pipeline configuration status", "pipelineConfig ID", pipelineConfig.ID, "targetID", pipelineConfig.TargetID, "error", err)
			return err
		}
		log.Warnw("Write conflict updating pipeline configuration status", "pipelineConfig ID", pipelineConfig.ID, "error", err)
		return nil
	}
	return nil
}
