// SPDX-FileCopyrightText: 2022-present Intel Corporation
//
// SPDX-License-Identifier: Apache-2.0

// Package controller implements the device provisioning controller
package controller

import (
	"context"
	"github.com/atomix/runtime/sdk/pkg/grpc/retry"
	"github.com/onosproject/device-provisioner/pkg/store"
	topoapi "github.com/onosproject/onos-api/go/onos/topo"
	"github.com/onosproject/onos-lib-go/pkg/logging"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"sync"
	"time"
)

var log = logging.GetLogger("controller")

// State represents the various states of controller lifecycle
type State int

const (
	// Disconnected represents the default/initial state
	Disconnected State = iota
	// Connected represents state where connection to onos-topo has been established
	Connected
	// Initialized represents state after the initial reconciliation pass has completed
	Initialized
	// Monitoring represents state of monitoring topology changes
	Monitoring
	// Stopped represents state where the controller has been issued a stop command
	Stopped
)

const (
	connectionRetryPause = 5 * time.Second

	queueDepth  = 128
	workerCount = 16
)

// Controller drives the device provisioning control logic
type Controller struct {
	realmLabel string
	realmValue string
	state      State

	lock        sync.RWMutex
	configStore store.ConfigStore

	topoAddress string
	topoOpts    []grpc.DialOption
	conn        *grpc.ClientConn
	topoClient  topoapi.TopoClient
	ctx         context.Context
	ctxCancel   context.CancelFunc
	queue       chan *topoapi.Object
	workingOn   map[topoapi.ID]*topoapi.Object
}

// NewController creates a new device provisioner controller
func NewController(realmLabel string, realmValue string, configStore store.ConfigStore, topoAddress string, topoOpts ...grpc.DialOption) *Controller {
	opts := append(topoOpts,
		grpc.WithStreamInterceptor(retry.RetryingStreamClientInterceptor(retry.WithRetryOn(codes.Unavailable, codes.Unknown))),
		grpc.WithUnaryInterceptor(retry.RetryingUnaryClientInterceptor(retry.WithRetryOn(codes.Unavailable, codes.Unknown))))
	return &Controller{
		realmLabel:  realmLabel,
		realmValue:  realmValue,
		configStore: configStore,
		topoAddress: topoAddress,
		topoOpts:    opts,
		workingOn:   make(map[topoapi.ID]*topoapi.Object),
	}
}

// Start starts the controller
func (c *Controller) Start() {
	log.Infof("Starting...")

	// Crate reconciliation job queue and workers
	c.queue = make(chan *topoapi.Object, queueDepth)
	for i := 0; i < workerCount; i++ {
		go c.reconcile(i)
	}

	go c.run()
}

// Stop stops the controller
func (c *Controller) Stop() {
	log.Infof("Stopping...")
	c.setState(Stopped)
	close(c.queue)
}

// Get the current operational state
func (c *Controller) getState() State {
	c.lock.RLock()
	defer c.lock.RUnlock()
	state := c.state
	return state
}

// Change state to the new state
func (c *Controller) setState(state State) {
	c.lock.Lock()
	defer c.lock.Unlock()
	c.state = state
}

// Change state to the new state, but only if in the given condition state
func (c *Controller) setStateIf(condition State, state State) {
	c.lock.Lock()
	defer c.lock.Unlock()
	if c.state == condition {
		c.state = state
	}
}

// Pause for the specified duration, but only if in the given condition state
func (c *Controller) pauseIf(condition State, pause time.Duration) {
	if c.getState() == condition {
		time.Sleep(pause)
	}
}

// Runs the main controller event loop
func (c *Controller) run() {
	log.Infof("Started")
	for state := c.getState(); state != Stopped; state = c.getState() {
		switch state {
		case Disconnected:
			c.waitForTopoConnection()
		case Connected:
			c.runInitialReconciliationSweep()
		case Initialized:
			c.prepareForMonitoring()
		case Monitoring:
			c.monitorDeviceConfigChanges()
		}
	}
	log.Infof("Stopped")
}

// Handles processing for Disconnected state by attempting to establish connection to onos-topo
func (c *Controller) waitForTopoConnection() {
	log.Infof("Connecting to onos-topo at %s...", c.topoAddress)
	for c.getState() == Disconnected {
		if conn, err := grpc.DialContext(context.Background(), c.topoAddress, c.topoOpts...); err == nil {
			c.conn = conn
			c.topoClient = topoapi.CreateTopoClient(conn)
			c.ctx, c.ctxCancel = context.WithCancel(context.Background())
			c.setState(Connected)
			log.Infof("Connected")
		} else {
			log.Warnf("Unable to connect to onos-topo: %+v", err)
			c.pauseIf(Disconnected, connectionRetryPause)
		}
	}
}
