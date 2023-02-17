// SPDX-FileCopyrightText: 2022-present Intel Corporation
//
// SPDX-License-Identifier: Apache-2.0

// Package chassis configuration controller
package chassis

import (
	"context"
	"github.com/onosproject/device-provisioner/pkg/controller/utils"
	"github.com/onosproject/device-provisioner/pkg/southbound"
	configstore "github.com/onosproject/device-provisioner/pkg/store/configs"
	"github.com/onosproject/device-provisioner/pkg/store/topo"
	provisionerapi "github.com/onosproject/onos-api/go/onos/provisioner"
	topoapi "github.com/onosproject/onos-api/go/onos/topo"
	"github.com/onosproject/onos-lib-go/pkg/controller/v2"
	"github.com/onosproject/onos-lib-go/pkg/errors"
	"github.com/onosproject/onos-lib-go/pkg/logging"
	"github.com/onosproject/onos-net-lib/pkg/realm"
	"sync"
	"time"
)

var log = logging.GetLogger()

const (
	defaultTimeout = 30 * time.Second
	queryPeriod    = 2 * time.Minute
	queueSize      = 100
)

// NewManager returns a new chassis controller manager
func NewManager(topo topo.Store, configStore configstore.ConfigStore, realmOptions *realm.Options) *Manager {
	manager := &Manager{
		topo:         topo,
		configStore:  configStore,
		realmOptions: realmOptions,
	}
	return manager

}

// Manager reconciles chassis configuration
type Manager struct {
	topo         topo.Store
	configStore  configstore.ConfigStore
	realmOptions *realm.Options
	cancel       context.CancelFunc
	mu           sync.Mutex
}

// Start starts manager
func (m *Manager) Start() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.cancel != nil {
		return nil
	}
	chassisController := controller.NewController(m.reconcile)

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
				err := chassisController.Reconcile(event.Object.ID)
				if err != nil {
					log.Warnw("Failed to reconcile object", "objectID", event.Object.ID, "error", err)
				}

			}
		}
	}()

	return nil

}

// Stop stops the manager
func (m *Manager) Stop() {
	m.mu.Lock()
	if m.cancel != nil {
		m.cancel()
		m.cancel = nil
	}
	m.mu.Unlock()
}

// Reconcile reconciles device chassis configuration
func (m *Manager) reconcile(ctx context.Context, request controller.Request[topoapi.ID]) controller.Directive[topoapi.ID] {
	targetID := request.ID
	log.Infow("Reconciling chassis config", "targetID", targetID)

	target, err := m.topo.Get(ctx, targetID)
	if err != nil {
		if !errors.IsNotFound(err) {
			log.Warnw("Failed reconciling chassis config", "targetID", targetID, "error", err)
			return request.Retry(err)
		}
		return request.Ack()
	}

	err = m.reconcileChassisConfiguration(ctx, target)
	if err != nil {
		log.Warnw("Failed reconciling chassis config", "targetID", targetID, "error", err)
		return request.Retry(err)
	}
	return request.Ack()
}

func (m *Manager) reconcileChassisConfiguration(ctx context.Context, target *topoapi.Object) error {
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
		err = utils.UpdateObjectAspect(ctx, m.topo, target, "chassis", ccState)
		if err != nil {
			return err
		}
		return nil
	}
	if ccState.ConfigID != deviceConfigAspect.ChassisConfigID {
		ccState.ConfigID = deviceConfigAspect.ChassisConfigID
		ccState.Updated = time.Now()
		ccState.Status.State = provisionerapi.ConfigStatus_PENDING
		err = utils.UpdateObjectAspect(ctx, m.topo, target, "chassis", ccState)
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
	artifacts, err := utils.GetArtifacts(ctx, m.configStore, deviceConfigAspect.ChassisConfigID, 1)
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
		err = utils.UpdateObjectAspect(ctx, m.topo, target, "chassis", ccState)
		if err != nil {
			return err
		}
		return nil
	}

	// Update ChassisConfigState aspect
	ccState.ConfigID = deviceConfigAspect.ChassisConfigID
	ccState.Updated = time.Now()
	ccState.Status.State = provisionerapi.ConfigStatus_APPLIED
	err = utils.UpdateObjectAspect(ctx, m.topo, target, "chassis", ccState)
	if err != nil {
		return err
	}
	log.Infow("Chassis config is set successfully", "targetID", target.ID)
	return nil
}
