// SPDX-FileCopyrightText: 2022-present Intel Corporation
//
// SPDX-License-Identifier: Apache-2.0

package watchers

import (
	"context"
	"github.com/onosproject/device-provisioner/pkg/controller/utils"
	"github.com/onosproject/device-provisioner/pkg/store/topo"
	topoapi "github.com/onosproject/onos-api/go/onos/topo"
	"github.com/onosproject/onos-lib-go/pkg/controller/v2"
	"github.com/onosproject/onos-lib-go/pkg/logging"
	"github.com/onosproject/onos-net-lib/pkg/realm"
	"sync"
)

var log = logging.GetLogger()

const queueSize = 100

// TopoWatcher is a topology watcher
type TopoWatcher struct {
	Topo         topo.Store
	cancel       context.CancelFunc
	mu           sync.Mutex
	RealmOptions *realm.Options
}

// Start starts the topo store watcher
func (w *TopoWatcher) Start(reconcile controller.Reconciler[topoapi.ID]) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.cancel != nil {
		return nil
	}

	eventCh := make(chan topoapi.Event, queueSize)
	ctx, cancel := context.WithCancel(context.Background())

	filter := utils.RealmQueryFilter(w.RealmOptions)
	err := w.Topo.Watch(ctx, eventCh, filter)
	if err != nil {
		cancel()
		return err
	}
	w.cancel = cancel
	go func() {
		for event := range eventCh {
			if _, ok := event.Object.Obj.(*topoapi.Object_Entity); ok {
				reconcile(context.Background(), controller.Request[topoapi.ID]{
					ID: event.Object.ID,
				})

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
