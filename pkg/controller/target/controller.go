// SPDX-FileCopyrightText: 2022-present Intel Corporation
//
// SPDX-License-Identifier: Apache-2.0

// Package target controller
package target

import (
	"context"
	"github.com/onosproject/device-provisioner/pkg/controller/utils"
	"github.com/onosproject/device-provisioner/pkg/store/topo"
	topoapi "github.com/onosproject/onos-api/go/onos/topo"
	"github.com/onosproject/onos-lib-go/pkg/controller/v2"
	"github.com/onosproject/onos-lib-go/pkg/errors"
	"github.com/onosproject/onos-lib-go/pkg/logging"
	"github.com/onosproject/onos-net-lib/pkg/p4rtclient"
	"github.com/onosproject/onos-net-lib/pkg/realm"
	"sync"
	"time"
)

var log = logging.GetLogger()

const queueSize = 100

const (
	defaultTimeout = 30 * time.Second
)

// NewManager returns a new p4rt connection reconciler
func NewManager(topo topo.Store, conns p4rtclient.ConnManager, realmOptions *realm.Options) *Manager {
	manager := &Manager{
		conns:        conns,
		topo:         topo,
		realmOptions: realmOptions,
	}

	return manager
}

// Manager reconciles P4RT connections
type Manager struct {
	conns        p4rtclient.ConnManager
	topo         topo.Store
	realmOptions *realm.Options
	cancel       context.CancelFunc
	mu           sync.Mutex
	connCh       chan p4rtclient.Conn
}

// Start starts the reconciler
func (m *Manager) Start() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.cancel != nil {
		return nil
	}

	targetController := controller.NewController(m.reconcile)
	m.connCh = make(chan p4rtclient.Conn, queueSize)
	ctx, cancel := context.WithCancel(context.Background())
	err := m.conns.Watch(ctx, m.connCh)
	m.cancel = cancel
	if err != nil {
		cancel()
		return err
	}

	go func() {
		for conn := range m.connCh {
			log.Debugw("Received P4RT Connection event for connection", "connectionID", conn.ID())
			err = targetController.Reconcile(conn.TargetID())
			if err != nil {
				log.Warnw("Failed to reconcile connection", "TargetID", conn.TargetID(), "error", err)
			}
		}

	}()

	eventCh := make(chan topoapi.Event, queueSize)
	filter := utils.RealmQueryFilter(m.realmOptions)
	err = m.topo.Watch(ctx, eventCh, filter)
	if err != nil {
		cancel()
		return err
	}
	go func() {
		for event := range eventCh {
			if _, ok := event.Object.Obj.(*topoapi.Object_Entity); ok {
				err := targetController.Reconcile(event.Object.ID)
				if err != nil {
					log.Warnw("Failed to reconcile object", "objectID", event.Object.ID, "error", err)
				}

			}
		}
	}()
	return nil
}

// Reconcile reconciles a connection for a P4RT target
func (m *Manager) reconcile(ctx context.Context, request controller.Request[topoapi.ID]) controller.Directive[topoapi.ID] {
	ctx, cancel := context.WithTimeout(ctx, defaultTimeout)
	defer cancel()

	targetID := request.ID
	log.Infow("Reconciling Target Connections", "targetID", targetID)
	target, err := m.topo.Get(ctx, targetID)
	if err != nil {
		if !errors.IsNotFound(err) {
			log.Errorw("Failed reconciling Target Connections", "targetID", targetID, "error", err)
			return request.Retry(err)
		}
		return m.disconnect(ctx, request, targetID)
	}

	stratumAgents := &topoapi.StratumAgents{}
	if err := target.GetAspect(stratumAgents); err != nil {
		log.Errorw("Failed to extract stratum agents aspect", "targetID", targetID, "error", err)
		return request.Retry(err)
	}

	if stratumAgents.P4RTEndpoint == nil {
		log.Warnw("Cannot find P4RT endpoint to make a connection to the target", "targetID", targetID)
		return request.Ack()
	}

	// Connect to device using P4Runtime
	dest := &p4rtclient.Destination{
		TargetID: target.ID,
		Endpoint: stratumAgents.P4RTEndpoint,
		DeviceID: stratumAgents.DeviceID,
		RoleName: "provisioner",
	}

	return m.connect(ctx, request, dest)
}

func (m *Manager) connect(ctx context.Context, request controller.Request[topoapi.ID], dest *p4rtclient.Destination) controller.Directive[topoapi.ID] {
	log.Infow("Connecting to Target", "targetID", dest.TargetID)
	if _, err := m.conns.Connect(ctx, dest); err != nil {
		if !errors.IsAlreadyExists(err) {
			log.Errorw("Failed connecting to Target", "targetID", dest.TargetID, "error", err)
			return request.Retry(err)
		}
		log.Warnw("Failed connecting to Target", "targetID", dest.TargetID, "error", err)
		return request.Ack()
	}
	return request.Ack()
}

func (m *Manager) disconnect(ctx context.Context, request controller.Request[topoapi.ID], targetID topoapi.ID) controller.Directive[topoapi.ID] {
	log.Infow("Disconnecting from Target", "targetID", targetID)
	if err := m.conns.Disconnect(ctx, targetID); err != nil {
		if !errors.IsNotFound(err) {
			log.Errorw("Failed disconnecting from Target", "targetID", targetID, "error", err)
			return request.Retry(err)
		}
		log.Warnw("Failed disconnecting from Target", "targetID", targetID, "error", err)
		return request.Ack()
	}
	return request.Ack()
}
