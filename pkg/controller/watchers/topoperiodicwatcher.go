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
	"github.com/onosproject/onos-net-lib/pkg/realm"
	"sync"
	"time"
)

// TopoPeriodicWatcher is a topology watcher
type TopoPeriodicWatcher struct {
	Topo         topo.Store
	cancel       context.CancelFunc
	mu           sync.Mutex
	RealmOptions *realm.Options
	QueryPeriod  time.Duration
}

// Start starts the topo store watcher
func (w *TopoPeriodicWatcher) Start(reconcile controller.Reconciler[topoapi.ID]) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.cancel != nil {
		return nil
	}

	ctx, cancel := context.WithCancel(context.Background())
	filter := utils.RealmQueryFilter(w.RealmOptions)

	queryPeriod := time.NewTicker(w.QueryPeriod)

	go func() {
		for {
			select {
			case <-queryPeriod.C:
				eventCh := make(chan *topoapi.Object, queueSize)
				err := w.Topo.Query(ctx, eventCh, filter)
				if err != nil {
					cancel()
					break
				}
				w.cancel = cancel
				for event := range eventCh {
					if _, ok := event.Obj.(*topoapi.Object_Entity); ok {
						reconcile(context.Background(), controller.Request[topoapi.ID]{
							ID: event.ID,
						})

					}
				}

			}
		}
	}()

	return nil

}

// Stop stops the topology watcher
func (w *TopoPeriodicWatcher) Stop() {
	w.mu.Lock()
	if w.cancel != nil {
		w.cancel()
		w.cancel = nil
	}
	w.mu.Unlock()
}
