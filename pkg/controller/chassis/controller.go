// SPDX-FileCopyrightText: 2022-present Intel Corporation
//
// SPDX-License-Identifier: Apache-2.0

// Package chassis configuration controller
package chassis

import (
	"context"
	"github.com/onosproject/device-provisioner/pkg/controller/utils"
	"github.com/onosproject/device-provisioner/pkg/controller/watchers"
	"github.com/onosproject/device-provisioner/pkg/southbound"
	configstore "github.com/onosproject/device-provisioner/pkg/store/pipelineconfig"
	"github.com/onosproject/device-provisioner/pkg/store/topo"
	provisionerapi "github.com/onosproject/onos-api/go/onos/provisioner"
	topoapi "github.com/onosproject/onos-api/go/onos/topo"
	"github.com/onosproject/onos-lib-go/pkg/controller/v2"
	"github.com/onosproject/onos-lib-go/pkg/errors"
	"github.com/onosproject/onos-lib-go/pkg/logging"
	"github.com/onosproject/onos-net-lib/pkg/realm"
	"time"
)

var log = logging.GetLogger()

const (
	defaultTimeout = 30 * time.Second
	requeueTimeout = 2 * time.Minute
)

// NewReconciler returns a new chassis reconciler
func NewReconciler(topo topo.Store, configStore configstore.ConfigStore, realmOptions *realm.Options) *Reconciler {
	reconciler := &Reconciler{
		topo:         topo,
		configStore:  configStore,
		realmOptions: realmOptions,
	}
	return reconciler

}

// Reconciler reconciles chassis configuration
type Reconciler struct {
	topo         topo.Store
	configStore  configstore.ConfigStore
	realmOptions *realm.Options
}

// Start starts new reconciler
func (r *Reconciler) Start() error {
	topoWatcher := watchers.TopoWatcher{
		Topo:         r.topo,
		RealmOptions: r.realmOptions,
	}
	err := topoWatcher.Start(r.Reconcile)
	if err != nil {
		return err
	}
	return nil
}

// Reconcile reconciles device chassis configuration
func (r *Reconciler) Reconcile(ctx context.Context, request controller.Request[topoapi.ID]) controller.Directive[topoapi.ID] {
	ctx, cancel := context.WithTimeout(ctx, defaultTimeout)
	defer cancel()

	targetID := request.ID
	log.Infow("Reconciling chassis config", "targetID", targetID)

	target, err := r.topo.Get(ctx, targetID)
	if err != nil {
		if !errors.IsNotFound(err) {
			log.Warnw("Failed reconciling chassis config", "targetID", targetID, "error", err)
			return request.Retry(err)
		}
		return request.Ack()
	}

	err = r.reconcileChassisConfiguration(ctx, target)
	if err != nil {
		log.Warnw("Failed reconciling chassis config", "targetID", targetID, "error", err)
		return request.Retry(err)
	}
	return request.Retry(nil).After(requeueTimeout)
}

func (r *Reconciler) reconcileChassisConfiguration(ctx context.Context, target *topoapi.Object) error {
	deviceConfigAspect := &provisionerapi.DeviceConfig{}
	err := target.GetAspect(deviceConfigAspect)
	if err != nil {
		log.Warnw("Failed retrieving device config aspect", "targetID", target.ID, "error", err)
		return err
	}
	if deviceConfigAspect.ChassisConfigID == "" {
		log.Warnw("Chassis config ID is not set", "targetID", target.ID)
		return nil
	}

	ccState := &provisionerapi.ChassisConfigState{}
	err = target.GetAspect(ccState)
	if err != nil {
		// Create ChassisConfigState aspect
		ccState.ConfigID = deviceConfigAspect.ChassisConfigID
		ccState.Updated = time.Now()
		ccState.Status.State = provisionerapi.ConfigStatus_PENDING
		err = utils.UpdateObjectAspect(ctx, r.topo, target, "chassis", ccState)
		if err != nil {
			return err
		}
		return nil
	}
	if ccState.ConfigID != deviceConfigAspect.ChassisConfigID {
		ccState.ConfigID = deviceConfigAspect.ChassisConfigID
		ccState.Updated = time.Now()
		ccState.Status.State = provisionerapi.ConfigStatus_PENDING
		err = utils.UpdateObjectAspect(ctx, r.topo, target, "chassis", ccState)
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
		err = utils.UpdateObjectAspect(ctx, r.topo, target, "chassis", ccState)
		if err != nil {
			return err
		}
		return nil
	}

	// Update ChassisConfigState aspect
	ccState.ConfigID = deviceConfigAspect.ChassisConfigID
	ccState.Updated = time.Now()
	ccState.Status.State = provisionerapi.ConfigStatus_APPLIED
	err = utils.UpdateObjectAspect(ctx, r.topo, target, "chassis", ccState)
	if err != nil {
		return err
	}
	log.Infow("Chassis config is set successfully", "targetID", target.ID)
	return nil
}
