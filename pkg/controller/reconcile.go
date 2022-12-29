// SPDX-FileCopyrightText: 2022-present Intel Corporation
//
// SPDX-License-Identifier: Apache-2.0

// Package controller implements the device provisioning controller
package controller

import (
	"context"
	"github.com/gogo/protobuf/proto"
	"github.com/onosproject/device-provisioner/pkg/southbound"
	"github.com/onosproject/onos-api/go/onos/provisioner"
	topoapi "github.com/onosproject/onos-api/go/onos/topo"
	"io"
	"time"
)

const (
	provisionerRoleName = "provisioner"
)

// Handles processing for the Initialized state
func (c *Controller) runInitialReconciliationSweep() {
	for c.getState() == Connected {
		if err := c.runFullReconciliationSweep(); err == nil {
			c.setState(Initialized)
		} else {
			log.Warnf("Unable to query onos-topo: %+v", err)
			c.pauseIf(Disconnected, connectionRetryPause)
		}
	}
}

// Runs reconciliation sweep for all objects in our realm
func (c *Controller) runFullReconciliationSweep() error {
	log.Info("Starting full reconciliation sweep...")
	if entities, err := c.topoClient.Query(c.ctx, &topoapi.QueryRequest{Filters: queryFilter(c.realm)}); err == nil {
		for c.getState() != Stopped {
			if entity, err := entities.Recv(); err == nil {
				c.queue <- entity.Object
			} else {
				if err == io.EOF {
					log.Info("Completed full reconciliation sweep")
					return nil
				}
				log.Warnf("Unable to read query response: %+v", err)
				return err
			}
		}
	} else {
		return err
	}
	return nil
}

// Returns filters for matching objects on realm label, entity type and with DeviceConfig aspect.
func queryFilter(realm string) *topoapi.Filters {
	return &topoapi.Filters{
		LabelFilters: []*topoapi.Filter{{
			Filter: &topoapi.Filter_Equal_{Equal_: &topoapi.EqualFilter{Value: realm}},
			Key:    "pod", // TODO: make this configurable to allow racks, etc.
		}},
		ObjectTypes: []topoapi.Object_Type{topoapi.Object_ENTITY},
		WithAspects: []string{"onos.provisioner.DeviceConfig"},
	}
}

// Setup watch for updates using onos-topo API
func (c *Controller) prepareForMonitoring() {
	filter := queryFilter(c.realm)
	log.Infof("Starting to watch onos-topo via %+v", filter)
	stream, err := c.topoClient.Watch(c.ctx, &topoapi.WatchRequest{Filters: filter})
	if err != nil {
		log.Warnf("Unable to start onos-topo watch: %+v", err)
		c.setState(Disconnected)
	} else {
		go func() {
			for c.getState() == Monitoring {
				resp, err := stream.Recv()
				if err == nil && isRelevant(resp.Event) {
					c.queue <- &resp.Event.Object
				} else if err != nil {
					log.Warnf("Watch stream has been stopped: %+v", err)
					c.setStateIf(Monitoring, Disconnected)
				}
			}
		}()
		c.setState(Monitoring)
	}
}

// Returns true if the object is relevant to the reconciler
func isRelevant(event topoapi.Event) bool {
	return event.Type != topoapi.EventType_REMOVED
}

// Handles processing for the Monitoring state
func (c *Controller) monitorDeviceConfigChanges() {
	tPeriodic := time.NewTicker(2 * time.Minute)
	tCheckState := time.NewTicker(2 * time.Second)

	for c.getState() == Monitoring {
		select {
		// Periodically scan and reconcile all device configurations
		case <-tPeriodic.C:
			_ = c.runFullReconciliationSweep()

		// Periodically pop-out to check state
		case <-tCheckState.C:
		}
	}
}

// Reconciliation worker
func (c *Controller) reconcile(workerID int) {
	for object := range c.queue {
		// Make sure that the config store contains configuration matching the DeviceConfig, if not, simply bail
		dcfg := &provisioner.DeviceConfig{}
		if err := object.GetAspect(dcfg); err == nil {
			log.Infof("Reconciler #%d is processing device %s...", workerID, object.ID)
			if len(dcfg.PipelineConfigID) > 0 {
				c.reconcilePipelineConfiguration(object, dcfg)
			}
			if len(dcfg.ChassisConfigID) > 0 {
				c.reconcileChassisConfiguration(object, dcfg)
			}
		}
	}
}

// Runs pipeline configuration reconciliation logic
func (c *Controller) reconcilePipelineConfiguration(object *topoapi.Object, dcfg *provisioner.DeviceConfig) {
	log.Infof("Reconciling pipeline configuration for %s...", object.ID)

	// If the DeviceConfig matches PipelineConfigState and cookie is not 0, we're done.
	pcState := &provisioner.PipelineConfigState{}
	if err := object.GetAspect(pcState); err == nil {
		if pcState.ConfigID == dcfg.PipelineConfigID {
			log.Infof("Pipeline configuration is up-to-date for %s", object.ID)
			return
		}
	}

	// Otherwise... get the config for the artifacts
	artifacts, err := c.configStore.GetArtifacts(context.Background(), &provisioner.ConfigRecord{ConfigID: dcfg.PipelineConfigID})
	if err != nil {
		return
	}

	// Connect to device using P4Runtime
	device, err := southbound.NewStratumDevice(object, provisionerRoleName)
	if err != nil {
		return
	}

	// Run the reconciliation against the device
	pcState.Cookie, err = device.ReconcilePipelineConfig(artifacts["p4info"], artifacts["p4bin"], pcState.Cookie)
	if err != nil {
		return
	}
	_ = device.Disconnect()

	// Update PipelineConfigState aspect
	pcState.ConfigID = dcfg.PipelineConfigID
	_ = c.updateObjectAspect(object, "pipeline", pcState)
}

// Runs chassis configuration reconciliation logic
func (c *Controller) reconcileChassisConfiguration(object *topoapi.Object, dcfg *provisioner.DeviceConfig) {
	log.Infof("Reconciling chassis configuration for %s...", object.ID)

	// If the DeviceConfig matches ChassisConfigState, we're done
	ccState := &provisioner.ChassisConfigState{}
	if err := object.GetAspect(ccState); err == nil {
		if ccState.ConfigID == dcfg.ChassisConfigID {
			log.Infof("Chassis configuration is up-to-date for %s", object.ID)
			return
		}
	}

	// Otherwise...

	// Connect to the device using gNMI
	// Issue Set request on the empty path
	// Update ChassisConfigState aspect
	ccState.ConfigID = dcfg.ChassisConfigID
	_ = c.updateObjectAspect(object, "chassis", ccState)
}

func (c *Controller) updateObjectAspect(object *topoapi.Object, kind string, aspect proto.Message) error {
	log.Infof("Updating %s configuration for %s", kind, object.ID)
	if err := object.SetAspect(aspect); err != nil {
		log.Warnf("Unable to set %s aspect for %s", kind, object.ID)
		return err
	}
	if _, err := c.topoClient.Update(c.ctx, &topoapi.UpdateRequest{Object: object}); err != nil {
		log.Warnf("Unable to update %s configuration for object %s", kind, object.ID)
		return err
	}
	return nil
}
