// SPDX-FileCopyrightText: 2022-present Intel Corporation
//
// SPDX-License-Identifier: Apache-2.0

// Package controller implements the device provisioning controller
package controller

import (
	"context"
	"github.com/gogo/protobuf/proto"
	"github.com/onosproject/device-provisioner/pkg/southbound"
	"github.com/onosproject/device-provisioner/pkg/store"
	"github.com/onosproject/onos-api/go/onos/provisioner"
	"github.com/onosproject/onos-api/go/onos/topo"
	"github.com/onosproject/onos-lib-go/pkg/errors"
	"github.com/onosproject/onos-net-lib/pkg/p4rtclient"
	"github.com/onosproject/onos-net-lib/pkg/p4utils"
	p4info "github.com/p4lang/p4runtime/go/p4/config/v1"
	p4api "github.com/p4lang/p4runtime/go/p4/v1"
	"google.golang.org/protobuf/encoding/prototext"
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
	filter := queryFilter(c.realmLabel, c.realmValue)
	if entities, err := c.topoClient.Query(c.ctx, &topo.QueryRequest{Filters: filter}); err == nil {
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
func queryFilter(realmLabel string, realmValue string) *topo.Filters {
	return &topo.Filters{
		LabelFilters: []*topo.Filter{{
			Filter: &topo.Filter_Equal_{Equal_: &topo.EqualFilter{Value: realmValue}},
			Key:    realmLabel,
		}},
		ObjectTypes: []topo.Object_Type{topo.Object_ENTITY},
		WithAspects: []string{"onos.provisioner.DeviceConfig", "onos.topo.StratumAgents"},
	}
}

// Setup watch for updates using onos-topo API
func (c *Controller) prepareForMonitoring() {
	filter := queryFilter(c.realmLabel, c.realmValue)
	log.Infof("Starting to watch onos-topo via %+v", filter)
	stream, err := c.topoClient.Watch(c.ctx, &topo.WatchRequest{Filters: filter})
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
func isRelevant(event topo.Event) bool {
	return event.Type != topo.EventType_REMOVED
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
		c.lock.Lock()

		// Is this object being worked on already?
		_, busy := c.workingOn[object.ID]
		if !busy {
			// If not, mark it as being worked on.
			c.workingOn[object.ID] = object
		}
		c.lock.Unlock()
		if !busy {
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

			// We're done working on this object
			c.lock.Lock()
			delete(c.workingOn, object.ID)
			c.lock.Unlock()
		}
	}
}

// Runs pipeline configuration reconciliation logic
func (c *Controller) reconcilePipelineConfiguration(object *topo.Object, dcfg *provisioner.DeviceConfig) {
	log.Infof("Reconciling pipeline configuration for %s...", object.ID)

	// If the DeviceConfig matches PipelineConfigState and cookie is not 0, we're done.
	pcState := &provisioner.PipelineConfigState{}
	if err := object.GetAspect(pcState); err == nil {
		if pcState.ConfigID == dcfg.PipelineConfigID && pcState.Cookie > 0 {
			log.Infof("Pipeline configuration is up-to-date for %s", object.ID)
			return
		}
	}

	// Otherwise... get the pipeline config artifacts
	artifacts, err := c.getArtifacts(dcfg.PipelineConfigID, 2)
	if err != nil {
		return
	}

	// Run the reconciliation against the device
	pcState.Cookie, err = c.reconcilePipelineConfig(object, artifacts[provisioner.P4InfoType], artifacts[provisioner.P4BinaryType], pcState.Cookie)
	if err != nil {
		log.Warnf("Unable to reconcile Stratum P4Runtime pipeline config for %s: %+v", object.ID, err)
		return
	}

	// Update PipelineConfigState aspect
	pcState.ConfigID = dcfg.PipelineConfigID
	pcState.Updated = time.Now()
	_ = c.updateObjectAspect(object, "pipeline", pcState)
}

// setPipelineConfig makes sure that the device has the given P4 pipeline configuration applied
func (c *Controller) reconcilePipelineConfig(object *topo.Object, info []byte, binary []byte, cookie uint64) (uint64, error) {
	// ask for the pipeline config cookie

	stratumAgents := &topo.StratumAgents{}
	if err := object.GetAspect(stratumAgents); err != nil {
		return 0, err
	}
	// Connect to device using P4Runtime
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	dest := &p4rtclient.Destination{
		TargetID: object.ID,
		Endpoint: stratumAgents.P4RTEndpoint,
		DeviceID: stratumAgents.DeviceID,
		RoleName: provisionerRoleName,
	}

	p4rtConn, err := c.conns.Connect(ctx, dest)
	if err != nil {
		log.Warnw("Cannot connect to dest", "dest", dest, "error", err)
		return 0, err
	}

	role := p4utils.NewStratumRole(dest.RoleName, 0, []byte{}, false, true)
	arbitrationResponse, err := p4rtConn.PerformMasterArbitration(role)
	if err != nil {
		log.Warnw("Failed to perform master arbitration", "error", err)
		return 0, err
	}
	electionID := arbitrationResponse.Arbitration.ElectionId

	gr, err := p4rtConn.GetForwardingPipelineConfig(ctx, &p4api.GetForwardingPipelineConfigRequest{
		DeviceId:     dest.DeviceID,
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
		return 0, err
	}

	// and then apply it to the device
	newCookie := uint64(time.Now().UnixNano())
	_, err = p4rtConn.SetForwardingPipelineConfig(ctx, &p4api.SetForwardingPipelineConfigRequest{
		DeviceId:   dest.DeviceID,
		Role:       dest.RoleName,
		ElectionId: electionID,
		Action:     p4api.SetForwardingPipelineConfigRequest_VERIFY_AND_COMMIT,
		Config: &p4api.ForwardingPipelineConfig{
			P4Info:         p4i,
			P4DeviceConfig: binary,
			Cookie:         &p4api.ForwardingPipelineConfig_Cookie{Cookie: newCookie},
		},
	})
	if err != nil {
		return 0, nil
	}
	log.Infof("%s: pipeline configured with cookie %d", dest.TargetID, newCookie)
	return newCookie, err
}

// Runs chassis configuration reconciliation logic
func (c *Controller) reconcileChassisConfiguration(object *topo.Object, dcfg *provisioner.DeviceConfig) {
	log.Infof("Reconciling chassis configuration for %s...", object.ID)

	// If the DeviceConfig matches ChassisConfigState, we're done
	ccState := &provisioner.ChassisConfigState{}
	if err := object.GetAspect(ccState); err == nil {
		if ccState.ConfigID == dcfg.ChassisConfigID {
			log.Infof("Chassis configuration is up-to-date for %s", object.ID)
			return
		}
	}

	// Otherwise... get chassis configuration artifact
	artifacts, err := c.getArtifacts(dcfg.ChassisConfigID, 1)
	if err != nil {
		return
	}

	// ... and apply the chassis configuration to the device using gNMI
	err = southbound.SetChassisConfig(object, artifacts[provisioner.ChassisType])
	if err != nil {
		log.Warnf("Unable to apply Stratum gNMI chassis config for %s: %+v", object.ID, err)
		return
	}

	// Update ChassisConfigState aspect
	ccState.ConfigID = dcfg.ChassisConfigID
	ccState.Updated = time.Now()
	_ = c.updateObjectAspect(object, "chassis", ccState)
}

// Retrieves the required configuration artifacts from the store
func (c *Controller) getArtifacts(configID provisioner.ConfigID, expectedNumber int) (store.Artifacts, error) {
	record, err := c.configStore.Get(context.Background(), configID)
	if err != nil {
		log.Warnf("Unable to retrieve pipeline configuration for %s: %+v", configID, err)
		return nil, err
	}

	// ... and the associated artifacts
	artifacts, err := c.configStore.GetArtifacts(context.Background(), record)
	if err != nil {
		log.Warnf("Unable to retrieve pipeline config artifacts for %s: %+v", configID, err)
		return nil, err
	}

	// Make sure the number of artifacts is sufficient
	if len(artifacts) < expectedNumber {
		log.Warnf("Insufficient number of config artifacts found: %d", len(artifacts))
		return nil, errors.NewInvalid("Insufficient number of config artifacts found")
	}
	return artifacts, err
}

// Update the topo object with the specified configuration aspect
func (c *Controller) updateObjectAspect(object *topo.Object, kind string, aspect proto.Message) error {
	log.Infof("Updating %s configuration for %s", kind, object.ID)
	gresp, err := c.topoClient.Get(c.ctx, &topo.GetRequest{ID: object.ID})
	if err != nil {
		log.Warnf("Unable to get object %s: %+v", object.ID, err)
		return err
	}

	if err = gresp.Object.SetAspect(aspect); err != nil {
		log.Warnf("Unable to set %s aspect for %s: %+v", kind, object.ID, err)
		return err
	}
	if _, err = c.topoClient.Update(c.ctx, &topo.UpdateRequest{Object: gresp.Object}); err != nil {
		log.Warnf("Unable to update %s configuration for object %s: %+v", kind, object.ID, err)
		return err
	}
	return nil
}
