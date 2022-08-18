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
	"strings"
	"time"
)

var log = logging.GetLogger()

const (
	defaultTimeout = 30 * time.Second
)

// NewController returns a new device pipeline pipeline config controller
func NewController(topo topo.Store, pipelineConfigs pipelineConfigStore.Store, adminController *admin.Controller, p4pluginRegistry pluginregistry.P4PluginRegistry) *controller.Controller {
	c := controller.NewController("pipeline")
	c.Watch(&TopoWatcher{
		topo: topo,
	})
	c.Watch(&Watcher{
		pipelineConfigs: pipelineConfigs,
	})
	c.Reconcile(&Reconciler{
		topo:             topo,
		pipelineConfigs:  pipelineConfigs,
		adminController:  adminController,
		p4PluginRegistry: p4pluginRegistry,
	})
	return c
}

// Reconciler reconciles device pipeline config
type Reconciler struct {
	topo             topo.Store
	p4PluginRegistry pluginregistry.P4PluginRegistry
	pipelineConfigs  pipelineConfigStore.Store
	adminController  *admin.Controller
}

func (r *Reconciler) createPipelineConfig(ctx context.Context, pipelineConfigID p4rtapi.PipelineConfigID) (controller.Result, error) {
	pipelineConfigIDItems := strings.Split(string(pipelineConfigID), "-")
	targetID := topoapi.ID(pipelineConfigIDItems[0])
	pipelineName := pipelineConfigIDItems[1]
	pipelineVersion := pipelineConfigIDItems[2]
	pipelineArch := pipelineConfigIDItems[3]

	target, err := r.topo.Get(ctx, targetID)
	if err != nil {
		if !errors.IsNotFound(err) {
			return controller.Result{}, err
		}
		return controller.Result{}, nil
	}

	p4rtServerInfo := &topoapi.P4RTServerInfo{}
	err = target.GetAspect(p4rtServerInfo)
	if err != nil {
		log.Errorw("Failed creating device pipeline config for target", "targetID", targetID, "error", err)
		return controller.Result{}, err
	}
	pipelines := p4rtServerInfo.Pipelines
	if len(pipelines) == 0 {
		log.Warnw("Failed creating device pipeline config for target", "targetID", targetID, "error", err)
		return controller.Result{}, err
	}
	pipelineInfo := &topoapi.P4PipelineInfo{}
	foundPipeline := false
	for _, pipeline := range pipelines {
		if pipeline.Name == pipelineName && pipeline.Version == pipelineVersion && pipeline.Architecture == pipelineArch {
			pipelineInfo = pipeline
			foundPipeline = true
		}
	}
	if !foundPipeline {
		err = errors.NewNotFound("pipeline not found")
		log.Warnw("Failed creating device pipeline config for target", "targetID", targetID, "error", err)
		return controller.Result{}, nil

	}

	pluginID := p4rtapi.NewP4PluginID(pipelineName, pipelineVersion, pipelineArch)
	p4Plugin, err := r.p4PluginRegistry.GetPlugin(pluginID)
	if err != nil {
		log.Errorw("Failed creating device pipeline config for target", "pipelineConfigID", pipelineConfigID, "targetID", targetID, "error", err)
		return controller.Result{}, err
	}

	deviceConfig, err := p4Plugin.GetP4DeviceConfig()
	if err != nil {
		if !errors.IsNotFound(err) {
			log.Errorw("Failed Reconciling device pipeline config for target", "pipelineConfigID", pipelineConfigID, "targetID", targetID, "error", err)
			return controller.Result{}, err
		}
		log.Warnw("Failed Reconciling device pipeline config for target; device config not found", "pipelineConfigID", pipelineConfigID, "targetID", targetID, "error", err)
		return controller.Result{}, nil
	}
	// If device config is nil, we can initialize it with an empty byte array
	if deviceConfig == nil {
		deviceConfig = []byte{}
	}
	p4Info, err := p4Plugin.GetP4Info()
	if err != nil {
		if !errors.IsNotFound(err) {
			log.Errorw("Failed creating device pipeline config for target", "pipelineConfigID", pipelineConfigID, "targetID", targetID, "error", err)
			return controller.Result{}, err
		}
		log.Warn(err)
		return controller.Result{}, nil
	}
	p4InfoBytes, err := proto.Marshal(p4Info)
	if err != nil {
		log.Errorw("Failed creating device pipeline config for target", "pipelineConfigID", pipelineConfigID, "targetID", targetID, "error", err)
		return controller.Result{}, err
	}

	pipelineConfigEntry := &p4rtapi.PipelineConfig{
		ID:       pipelineConfigID,
		TargetID: p4rtapi.TargetID(targetID),
		Spec: &p4rtapi.PipelineConfigSpec{
			P4DeviceConfig: deviceConfig,
			P4Info:         p4InfoBytes,
		},
	}
	pipelineConfigEntry.Action = ToConfigAction(pipelineInfo.ConfigurationAction)
	err = r.pipelineConfigs.Create(ctx, pipelineConfigEntry)
	if err != nil {
		if !errors.IsAlreadyExists(err) {
			log.Errorw("Failed Reconciling creating pipeline config for target", "pipelineConfigID", pipelineConfigID, "targetID", targetID, "error", err)
			return controller.Result{}, err
		}
		log.Warn(err)
		return controller.Result{}, nil
	}
	log.Infow("Device Pipeline config is created successfully in pipeline config data store", "pipelineConfigID", pipelineConfigID, "target ID", targetID, "action", pipelineConfigEntry.Action.String())
	return controller.Result{}, nil

}

// Reconcile reconciles pipeline configuration
func (r *Reconciler) Reconcile(id controller.ID) (controller.Result, error) {
	ctx, cancel := context.WithTimeout(context.Background(), defaultTimeout)
	defer cancel()
	pipelineConfigID := id.Value.(p4rtapi.PipelineConfigID)
	pipelineConfig, err := r.pipelineConfigs.Get(ctx, pipelineConfigID)
	if err != nil {
		if !errors.IsNotFound(err) {
			log.Warnw("Failed to reconcile Pipeline Configuration", "pipelineConfig ID", pipelineConfigID, "error", err)
			return controller.Result{}, err
		}
		return r.createPipelineConfig(ctx, pipelineConfigID)
	}

	return r.reconcileConfiguration(ctx, pipelineConfig)

}
func (r *Reconciler) reconcileConfiguration(ctx context.Context, pipelineConfigEntry *p4rtapi.PipelineConfig) (controller.Result, error) {
	log.Infow("Reconcile device pipeline configuration", "pipelineConfig ID", pipelineConfigEntry.ID, "targetID", pipelineConfigEntry.TargetID)
	if pipelineConfigEntry.Spec == nil || len(pipelineConfigEntry.Spec.P4Info) == 0 {
		log.Warn("Failed Reconciling device pipeline config; pipeline config spec is not initialized", "pipelineConfig ID", pipelineConfigEntry.ID, "targetID", pipelineConfigEntry.TargetID)
		return controller.Result{}, nil
	}

	targetID := topoapi.ID(pipelineConfigEntry.TargetID)
	targetClient, target, err := r.adminController.Client(ctx, targetID)
	if err != nil {
		if !errors.IsNotFound(err) {
			log.Errorw("Failed Reconciling device pipeline config", "pipelineConfig ID", pipelineConfigEntry.ID, "targetID", targetID, "error", err)
			return controller.Result{}, err
		}
		pipelineConfigEntry.Status.State = p4rtapi.PipelineConfigStatus_UNKNOWN
		pipelineConfigEntry.Status.Mastership.Master = ""
		pipelineConfigEntry.Status.Mastership.Term = 0
		if err := r.updateConfigurationStatus(ctx, pipelineConfigEntry); err != nil {
			return controller.Result{}, err
		}
		return controller.Result{}, nil
	}

	p4rtServerInfo := &topoapi.P4RTServerInfo{}
	err = target.GetAspect(p4rtServerInfo)
	if err != nil {
		log.Errorw("Failed Reconciling device pipeline config", "pipelineConfig ID", pipelineConfigEntry.ID, "targetID", targetID, "error", err)
		return controller.Result{}, err
	}

	pipelineConfigIDItems := strings.Split(string(pipelineConfigEntry.ID), "-")
	pipelineName := pipelineConfigIDItems[1]
	pipelineVersion := pipelineConfigIDItems[2]
	pipelineArch := pipelineConfigIDItems[3]
	for _, pipeline := range p4rtServerInfo.Pipelines {
		if pipeline.Name == pipelineName && pipeline.Version == pipelineVersion && pipeline.Architecture == pipelineArch {
			if pipeline.ConfigurationAction.String() != pipelineConfigEntry.Action.String() {
				pipelineConfigEntry.Status.State = p4rtapi.PipelineConfigStatus_PENDING
				pipelineConfigEntry.Action = ToConfigAction(pipeline.ConfigurationAction)
				if err := r.updateConfigurationStatus(ctx, pipelineConfigEntry); err != nil {
					return controller.Result{}, err
				}
				log.Infow("Updating pipeline configuration action", "pipeline config action", pipelineConfigEntry.Action)
				return controller.Result{}, nil

			}
		}
	}

	mastership := topoapi.P4RTMastershipState{}
	_ = target.GetAspect(&mastership)
	mastershipTerm := p4rtapi.MastershipTerm(mastership.Term)

	if mastershipTerm > pipelineConfigEntry.Status.Mastership.Term {
		pipelineConfigEntry.Status.State = p4rtapi.PipelineConfigStatus_PENDING
		pipelineConfigEntry.Status.Mastership.Master = mastership.NodeId
		pipelineConfigEntry.Status.Mastership.Term = mastershipTerm
		if err := r.updateConfigurationStatus(ctx, pipelineConfigEntry); err != nil {
			return controller.Result{}, err
		}
		log.Infow("Mastership is changed; Pipeline Configuration state is changed to PENDING", "pipelineConfig ID", pipelineConfigEntry.ID, "targetID", targetID)
		return controller.Result{}, nil
	}

	if pipelineConfigEntry.Status.State != p4rtapi.PipelineConfigStatus_PENDING {
		log.Warnw("Failed reconciling device pipeline config", "pipelineConfig ID", pipelineConfigEntry.ID, "targetID", pipelineConfigEntry.TargetID, "state", pipelineConfigEntry.Status.State)
		return controller.Result{}, nil
	}

	// If the master node ID is not set, skip reconciliation.
	if mastership.NodeId == "" {
		log.Infow("No master for target", "pipelineConfig ID", pipelineConfigEntry.ID, "targetID", pipelineConfigEntry.TargetID)
		return controller.Result{}, nil
	}

	if len(p4rtServerInfo.Pipelines) == 0 {
		log.Errorw("No pipeline information found for target", "pipelineConfig ID", pipelineConfigEntry.ID, "targetID", pipelineConfigEntry.TargetID, "error", err)
		return controller.Result{}, errors.NewNotFound("Device pipeline config info is not found", targetID)
	}

	p4InfoBytes := pipelineConfigEntry.Spec.P4Info
	p4DeviceConfig := pipelineConfigEntry.Spec.P4DeviceConfig

	p4Info := &p4configapi.P4Info{}
	err = proto.Unmarshal(p4InfoBytes, p4Info)
	if err != nil {
		log.Errorw("Failed reconciling device pipeline config", "pipelineConfig ID", pipelineConfigEntry.ID, "targetID", pipelineConfigEntry.TargetID, "error", err)
		return controller.Result{}, err
	}

	action := ToSetPipelineConfigReqAction(pipelineConfigEntry.Action)
	log.Infow("Reconciling device pipeline config", "action", action.String())

	pipelineConfigSpec := &admin.PipelineConfigSpec{
		P4Info:         p4Info,
		P4DeviceConfig: p4DeviceConfig,
		Action:         action,
	}

	pipelineConfigResponse, err := targetClient.SetForwardingPipelineConfig(ctx, pipelineConfigSpec)

	if err != nil {
		log.Errorw("Failed Reconciling device pipeline config", "pipelineConfig ID", pipelineConfigEntry.ID, "targetID", pipelineConfigEntry.TargetID, "error", err)
		pipelineConfigEntry.Status.State = p4rtapi.PipelineConfigStatus_FAILED
		if err := r.updateConfigurationStatus(ctx, pipelineConfigEntry); err != nil {
			return controller.Result{}, err
		}
		return controller.Result{}, nil
	}
	log.Infow("Pipeline config response", "pipeline config response", pipelineConfigResponse)
	pipelineConfigEntry.Status.State = p4rtapi.PipelineConfigStatus_COMPLETE
	pipelineConfigEntry.Status.Mastership.Master = mastership.NodeId
	pipelineConfigEntry.Status.Mastership.Term = mastershipTerm
	if err := r.updateConfigurationStatus(ctx, pipelineConfigEntry); err != nil {
		return controller.Result{}, err
	}
	log.Infow("Device pipelineConfig is completed successfully", "pipelineConfig ID", pipelineConfigEntry.ID, "targetID",
		pipelineConfigEntry.TargetID, "Action", pipelineConfigEntry.Action.String())
	return controller.Result{}, nil
}

func (r *Reconciler) updateConfigurationStatus(ctx context.Context, pipelineConfig *p4rtapi.PipelineConfig) error {
	log.Debug(pipelineConfig.Status)
	err := r.pipelineConfigs.UpdateStatus(ctx, pipelineConfig)
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

func ToConfigAction(action topoapi.P4PipelineInfo_ConfigurationAction) p4rtapi.ConfigurationAction {
	var configAction p4rtapi.ConfigurationAction
	switch action {
	case topoapi.P4PipelineInfo_VERIFY_AND_COMMIT:
		configAction = p4rtapi.ConfigurationAction_VERIFY_AND_COMMIT
	case topoapi.P4PipelineInfo_RECONCILE_AND_COMMIT:
		configAction = p4rtapi.ConfigurationAction_RECONCILE_AND_COMMIT
	case topoapi.P4PipelineInfo_COMMIT:
		configAction = p4rtapi.ConfigurationAction_COMMIT
	case topoapi.P4PipelineInfo_VERIFY_AND_SAVE:
		configAction = p4rtapi.ConfigurationAction_VERIFY_AND_SAVE
	case topoapi.P4PipelineInfo_VERIFY:
		configAction = p4rtapi.ConfigurationAction_VERIFY

	}

	return configAction
}

func ToSetPipelineConfigReqAction(configAction p4rtapi.ConfigurationAction) p4api.SetForwardingPipelineConfigRequest_Action {
	var action p4api.SetForwardingPipelineConfigRequest_Action
	switch configAction {
	case p4rtapi.ConfigurationAction_VERIFY_AND_COMMIT:
		action = p4api.SetForwardingPipelineConfigRequest_VERIFY_AND_COMMIT
	case p4rtapi.ConfigurationAction_VERIFY:
		action = p4api.SetForwardingPipelineConfigRequest_VERIFY
	case p4rtapi.ConfigurationAction_RECONCILE_AND_COMMIT:
		action = p4api.SetForwardingPipelineConfigRequest_RECONCILE_AND_COMMIT
	case p4rtapi.ConfigurationAction_VERIFY_AND_SAVE:
		action = p4api.SetForwardingPipelineConfigRequest_RECONCILE_AND_COMMIT
	case p4rtapi.ConfigurationAction_COMMIT:
		action = p4api.SetForwardingPipelineConfigRequest_COMMIT
	}
	return action
}
