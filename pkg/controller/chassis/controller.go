// SPDX-FileCopyrightText: 2022-present Intel Corporation
//
// SPDX-License-Identifier: Apache-2.0

// Package chassis configuration controller
package chassis

import (
	"context"
	"github.com/gogo/protobuf/proto"
	"github.com/onosproject/device-provisioner/pkg/controller/utils"
	"github.com/onosproject/device-provisioner/pkg/southbound"
	configstore "github.com/onosproject/device-provisioner/pkg/store/pipelineconfig"
	"github.com/onosproject/device-provisioner/pkg/store/topo"
	provisionerapi "github.com/onosproject/onos-api/go/onos/provisioner"
	topoapi "github.com/onosproject/onos-api/go/onos/topo"
	"github.com/onosproject/onos-lib-go/pkg/controller"
	"github.com/onosproject/onos-lib-go/pkg/errors"
	"github.com/onosproject/onos-lib-go/pkg/logging"
	"time"
)

var log = logging.GetLogger()

const (
	defaultTimeout = 30 * time.Second
)

// NewController returns a new pipeline and chassis configuration controller
func NewController(topo topo.Store, configStore configstore.ConfigStore, realmLabel string, realmValue string) *controller.Controller {
	c := controller.NewController("chassis-configuration")
	c.Watch(&TopoWatcher{
		topo:       topo,
		realmValue: realmValue,
		realmLabel: realmLabel,
	})

	c.Reconcile(&Reconciler{
		topo:        topo,
		configStore: configStore,
	})
	return c
}

// Reconciler reconciles chassis configuration
type Reconciler struct {
	topo        topo.Store
	configStore configstore.ConfigStore
}

// Reconcile reconciles device chassis configuration
func (r *Reconciler) Reconcile(id controller.ID) (controller.Result, error) {
	ctx, cancel := context.WithTimeout(context.Background(), defaultTimeout)
	defer cancel()

	targetID := id.Value.(topoapi.ID)
	log.Infow("Reconciling chassis config", "targetID", targetID)

	target, err := r.topo.Get(ctx, targetID)
	if err != nil {
		if !errors.IsNotFound(err) {
			log.Warnw("Failed reconciling chassis config", "targetID", targetID, "error", err)
			return controller.Result{}, err
		}
		return controller.Result{}, nil
	}

	err = r.reconcileChassisConfiguration(ctx, target)
	if err != nil {
		log.Warnw("Failed reconciling chassis config", "targetID", targetID, "error", err)
		return controller.Result{}, err
	}

	return controller.Result{}, nil
}

func (r *Reconciler) reconcileChassisConfiguration(ctx context.Context, target *topoapi.Object) error {
	deviceConfigAspect := &provisionerapi.DeviceConfig{}
	err := target.GetAspect(deviceConfigAspect)
	if err != nil {
		log.Warnw("Failed retrieving device config aspect", "targetID", target.ID, "error", err)
		return err
	}
	if deviceConfigAspect.ChassisConfigID == "" {
		log.Warnw("Chassis config ID is not set", "targetID", target.ID, "error", err)
		return nil
	}

	ccState := &provisionerapi.ChassisConfigState{}
	err = target.GetAspect(ccState)
	if err != nil {
		// Update ChassisConfigState aspect
		ccState.ConfigID = deviceConfigAspect.ChassisConfigID
		ccState.Updated = time.Now()
		ccState.Status.State = provisionerapi.ConfigStatus_PENDING
		err = r.updateObjectAspect(ctx, target, "chassis", ccState)
		if err != nil {
			return err
		}
		return nil
	}

	if ccState.Status.State != provisionerapi.ConfigStatus_PENDING {
		log.Debugw("Chassis config state is not in Pending state", "ConfigState", ccState.Status.State)
		return nil
	}

	// get chassis configuration artifact
	artifacts, err := utils.GetArtifacts(ctx, r.configStore, deviceConfigAspect.ChassisConfigID, 1)
	if err != nil {
		return err
	}

	// apply the chassis configuration to the device using gNMI
	err = southbound.SetChassisConfig(target, artifacts[provisionerapi.ChassisType])
	if err != nil {
		log.Warnw("Failed to apply Stratum gNMI chassis config", target.ID, err)
		ccState.ConfigID = deviceConfigAspect.ChassisConfigID
		ccState.Updated = time.Now()
		ccState.Status.State = provisionerapi.ConfigStatus_FAILED
		err = r.updateObjectAspect(ctx, target, "chassis", ccState)
		if err != nil {
			return err
		}
		return nil
	}

	// Update ChassisConfigState aspect
	ccState.ConfigID = deviceConfigAspect.ChassisConfigID
	ccState.Updated = time.Now()
	ccState.Status.State = provisionerapi.ConfigStatus_APPLIED
	err = r.updateObjectAspect(ctx, target, "chassis", ccState)
	if err != nil {
		return err
	}
	log.Infow("Chassis config is set successfully", "targetID", target.ID)

	return nil
}

// Update the topo object with the specified configuration aspect
func (r *Reconciler) updateObjectAspect(ctx context.Context, object *topoapi.Object, kind string, aspect proto.Message) error {
	log.Infow("Updating configuration aspect", "kind", kind, "targetID", object.ID)
	entity, err := r.topo.Get(ctx, object.ID)
	if err != nil {
		if !errors.IsNotFound(err) {
			log.Warnw("Unable to get object", "targetID", object.ID, "error", err)
			return err
		}
		log.Warnw("Cannot find target object", "targetID", object.ID)
		return nil
	}

	if err = entity.SetAspect(aspect); err != nil {
		log.Warnw("Unable to set aspect", "kind", kind, "targetID", object.ID, "error", err)
		return err
	}
	err = r.topo.Update(ctx, entity)
	if err != nil {
		if !errors.IsNotFound(err) && !errors.IsConflict(err) {
			log.Warnw("Unable to update configuration for object", "kind", kind, "targetID", object.ID, "error", err)
			return err
		}
		log.Warnw("Write conflict updating entity aspect", "kind", kind, "targetID", object.ID, "error", err)
		return nil
	}
	return nil
}
