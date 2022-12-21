// SPDX-FileCopyrightText: 2022-present Intel Corporation
//
// SPDX-License-Identifier: Apache-2.0

// Package controller implements the device provisioning controller
package controller

import (
	"github.com/onosproject/onos-api/go/onos/provisioner"
	topoapi "github.com/onosproject/onos-api/go/onos/topo"
	"io"
	"time"
)

// Handles processing for the Initialized state
func (c *Controller) runInitialReconciliationSweep() {
	for c.getState() == Initialized {
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
	stream, err := c.topoClient.Watch(c.ctx, &topoapi.WatchRequest{Filters: queryFilter(c.realm)})
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
			c.reconcilePipelineConfiguration(object, dcfg)
			c.reconcileChassisConfiguration(object, dcfg)
		} else {
			log.Warnf("Object %s does not have device config aspect. Skipping...", object.ID)
		}
	}
}

// Runs pipeline configuration reconciliation logic
func (c *Controller) reconcilePipelineConfiguration(object *topoapi.Object, dcfg *provisioner.DeviceConfig) {
	// TODO: implement me
	log.Infof("Reconciling pipeline configuration for %+v...", object)

	// If the DeviceConfig matches PipelineConfigState and cookie is not 0, we're done.
	pcState := &provisioner.PipelineConfigState{}
	if err := object.GetAspect(pcState); err == nil {
		if pcState.ConfigID == dcfg.PipelineConfigID {
			log.Infof("Pipeline configuration is up-to-date for %s", object.ID)
			return
		}
	}

	// Otherwise...

	// Connect to device using P4Runtime
	// Establish message stream
	// Negotiate mastership for our role
	// Retrieve pipeline configuration - cookie only
	// If there is a difference between the cookie on the device and the cookie in the object aspect, push new configuration
	// Update PipelineConfigState aspect
}

// Runs chassis configuration reconciliation logic
func (c *Controller) reconcileChassisConfiguration(object *topoapi.Object, dcfg *provisioner.DeviceConfig) {
	// TODO: implement me
	log.Infof("Reconciling chassis configuration for %+v...", object)

	// If the DeviceConfig matches ChassisConfigState, we're done
	ccState := &provisioner.ChassisConfigState{}
	if err := object.GetAspect(ccState); err == nil {
		if ccState.ConfigID == dcfg.PipelineConfigID {
			log.Infof("Chassis configuration is up-to-date for %s", object.ID)
			return
		}
	}

	// Otherwise...

	// Connect to the device using gNMI
	// Issue Set request on the empty path
	// Update ChassisConfigState aspect
}
