// SPDX-FileCopyrightText: 2020-present Open Networking Foundation <info@opennetworking.org>
//
// SPDX-License-Identifier: Apache-2.0

package target

import (
	"context"
	"github.com/onosproject/onos-net-lib/pkg/p4rtclient"
	"sync"

	topoapi "github.com/onosproject/onos-api/go/onos/topo"

	"github.com/onosproject/device-provisioner/pkg/store/topo"
	"github.com/onosproject/onos-lib-go/pkg/controller"
)

const queueSize = 100

// TopoWatcher is a topology watcher
type TopoWatcher struct {
	topo       topo.Store
	cancel     context.CancelFunc
	mu         sync.Mutex
	realmLabel string
	realmValue string
}

// Start starts the topo store watcher
func (w *TopoWatcher) Start(ch chan<- controller.ID) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.cancel != nil {
		return nil
	}

	eventCh := make(chan topoapi.Event, queueSize)
	ctx, cancel := context.WithCancel(context.Background())

	filter := queryFilter(w.realmLabel, w.realmValue)
	err := w.topo.Watch(ctx, eventCh, filter)
	if err != nil {
		cancel()
		return err
	}
	w.cancel = cancel
	go func() {
		for event := range eventCh {
			if _, ok := event.Object.Obj.(*topoapi.Object_Entity); ok {
				ch <- controller.NewID(event.Object.ID)

			}
		}
	}()
	return nil
}

// Stop stops the topology watcher
func (w *TopoWatcher) Stop() {
	w.mu.Lock()
	if w.cancel != nil {
		w.cancel()
		w.cancel = nil
	}
	w.mu.Unlock()
}

// Returns filters for matching objects on realm label, entity type and with DeviceConfig aspect.
func queryFilter(realmLabel string, realmValue string) *topoapi.Filters {
	return &topoapi.Filters{
		LabelFilters: []*topoapi.Filter{{
			Filter: &topoapi.Filter_Equal_{Equal_: &topoapi.EqualFilter{Value: realmValue}},
			Key:    realmLabel,
		}},
		ObjectTypes: []topoapi.Object_Type{topoapi.Object_ENTITY},
		WithAspects: []string{"onos.provisioner.DeviceConfig", "onos.topo.StratumAgents"},
	}
}

// ConnWatcher is a P4RT connection watcher
type ConnWatcher struct {
	conns  p4rtclient.ConnManager
	cancel context.CancelFunc
	mu     sync.Mutex
	connCh chan p4rtclient.Conn
}

// Start starts the connection watcher
func (c *ConnWatcher) Start(ch chan<- controller.ID) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.cancel != nil {
		return nil
	}

	c.connCh = make(chan p4rtclient.Conn, queueSize)
	ctx, cancel := context.WithCancel(context.Background())
	err := c.conns.Watch(ctx, c.connCh)
	if err != nil {
		cancel()
		return err
	}
	c.cancel = cancel

	go func() {
		for conn := range c.connCh {
			log.Debugw("Received P4RT Connection event for connection", "connectionID", conn.ID())
			ch <- controller.NewID(conn.TargetID())
		}
		close(ch)
	}()
	return nil
}

// Stop stops the connection watcher
func (c *ConnWatcher) Stop() {
	c.mu.Lock()
	if c.cancel != nil {
		c.cancel()
		c.cancel = nil
	}
	c.mu.Unlock()
}
