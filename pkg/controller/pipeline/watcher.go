// SPDX-FileCopyrightText: 2022-present Intel Corporation
//
// SPDX-License-Identifier: Apache-2.0

package pipeline

import (
	"context"
	"github.com/onosproject/device-provisioner/pkg/store/pipelineconfig"
	"github.com/onosproject/device-provisioner/pkg/store/topo"
	p4rtapi "github.com/onosproject/onos-api/go/onos/p4rt/v1"
	topoapi "github.com/onosproject/onos-api/go/onos/topo"
	"github.com/onosproject/onos-lib-go/pkg/controller"
	"sync"
)

const queueSize = 100

// Watcher pipeline config store watcher
type Watcher struct {
	pipelineConfigs pipelineconfig.Store
	cancel          context.CancelFunc
	mu              sync.Mutex
}

// Start starts the watcher
func (w *Watcher) Start(ch chan<- controller.ID) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.cancel != nil {
		return nil
	}

	eventCh := make(chan *p4rtapi.PipelineConfig, queueSize)
	ctx, cancel := context.WithCancel(context.Background())

	err := w.pipelineConfigs.Watch(ctx, eventCh, pipelineconfig.WithReplay())
	if err != nil {
		cancel()
		return err
	}
	w.cancel = cancel
	go func() {
		for pipelineConfig := range eventCh {
			log.Infow("Received config event", "config ID", pipelineConfig.ID)
			ch <- controller.NewID(pipelineConfig.ID)
		}
	}()
	return nil
}

// Stop stops the watcher
func (w *Watcher) Stop() {
	w.mu.Lock()
	if w.cancel != nil {
		log.Info("cancel is called")
		w.cancel()
		w.cancel = nil
	}
	w.mu.Unlock()
}

// TopoWatcher is a topology watcher
type TopoWatcher struct {
	topo   topo.Store
	cancel context.CancelFunc
	mu     sync.Mutex
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

	err := w.topo.Watch(ctx, eventCh, nil)
	if err != nil {
		cancel()
		return err
	}
	w.cancel = cancel
	go func() {
		for event := range eventCh {
			log.Debugw("Received topo event", "topo object ID", event.Object.ID, "event type", event.Type)
			if _, ok := event.Object.Obj.(*topoapi.Object_Entity); ok {
				log.Debugw("Event entity", "entity", event.Object)
				p4rtServerInfo := &topoapi.P4RTServerInfo{}
				err = event.Object.GetAspect(p4rtServerInfo)
				if err == nil {
					for _, pipelineInfo := range p4rtServerInfo.Pipelines {
						ch <- controller.NewID(pipelineconfig.NewPipelineConfigID(p4rtapi.TargetID(event.Object.ID),
							pipelineInfo.Name, pipelineInfo.Version,
							pipelineInfo.Architecture))
					}

				}
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
