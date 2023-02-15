// SPDX-FileCopyrightText: 2022-present Intel Corporation
//
// SPDX-License-Identifier: Apache-2.0

// Package watchers list of common watchers
package watchers

import (
	"context"
	topoapi "github.com/onosproject/onos-api/go/onos/topo"
	"github.com/onosproject/onos-lib-go/pkg/controller/v2"
	"github.com/onosproject/onos-net-lib/pkg/p4rtclient"
	"sync"
)

// ConnWatcher is a P4RT connection watcher
type ConnWatcher struct {
	Conns  p4rtclient.ConnManager
	cancel context.CancelFunc
	mu     sync.Mutex
	connCh chan p4rtclient.Conn
}

// Start starts the connection watcher
func (c *ConnWatcher) Start(reconcile controller.Reconciler[topoapi.ID]) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.cancel != nil {
		return nil
	}

	c.connCh = make(chan p4rtclient.Conn, queueSize)
	ctx, cancel := context.WithCancel(context.Background())
	err := c.Conns.Watch(ctx, c.connCh)
	if err != nil {
		cancel()
		return err
	}
	c.cancel = cancel

	go func() {
		for conn := range c.connCh {
			log.Debugw("Received P4RT Connection event for connection", "connectionID", conn.ID())
			reconcile(context.Background(), controller.Request[topoapi.ID]{
				ID: conn.TargetID(),
			})
		}

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
