// SPDX-FileCopyrightText: 2022-present Intel Corporation
//
// SPDX-License-Identifier: Apache-2.0

// Package target controller
package target

import (
	"context"
	"github.com/onosproject/device-provisioner/pkg/controller/watchers"
	"github.com/onosproject/device-provisioner/pkg/store/topo"
	topoapi "github.com/onosproject/onos-api/go/onos/topo"
	"github.com/onosproject/onos-lib-go/pkg/controller/v2"
	"github.com/onosproject/onos-lib-go/pkg/errors"
	"github.com/onosproject/onos-lib-go/pkg/logging"
	"github.com/onosproject/onos-net-lib/pkg/p4rtclient"
	"github.com/onosproject/onos-net-lib/pkg/realm"
	"time"
)

var log = logging.GetLogger()

const (
	defaultTimeout = 30 * time.Second
)

// NewReconciler returns a new p4rt connection reconciler
func NewReconciler(topo topo.Store, conns p4rtclient.ConnManager, realmOptions *realm.Options) *Reconciler {
	reconciler := &Reconciler{
		conns:        conns,
		topo:         topo,
		realmOptions: realmOptions,
	}

	return reconciler
}

// Reconciler reconciles P4RT connections
type Reconciler struct {
	conns        p4rtclient.ConnManager
	topo         topo.Store
	realmOptions *realm.Options
}

// Start starts the reconciler
func (r *Reconciler) Start() error {
	topoWatcher := watchers.TopoWatcher{
		Topo:         r.topo,
		RealmOptions: r.realmOptions,
	}
	err := topoWatcher.Start(r.Reconcile)
	if err != nil {
		return err
	}

	connWatcher := watchers.ConnWatcher{
		Conns: r.conns,
	}
	err = connWatcher.Start(r.Reconcile)
	if err != nil {
		return err
	}
	return nil

}

// Reconcile reconciles a connection for a P4RT target
func (r *Reconciler) Reconcile(ctx context.Context, request controller.Request[topoapi.ID]) controller.Directive[topoapi.ID] {
	ctx, cancel := context.WithTimeout(ctx, defaultTimeout)
	defer cancel()

	targetID := request.ID
	log.Infow("Reconciling Target Connections", "targetID", targetID)
	target, err := r.topo.Get(ctx, targetID)
	if err != nil {
		if !errors.IsNotFound(err) {
			log.Errorw("Failed reconciling Target Connections", "targetID", targetID, "error", err)
			return request.Retry(err)
		}
		return r.disconnect(ctx, request, targetID)
	}

	stratumAgents := &topoapi.StratumAgents{}
	if err := target.GetAspect(stratumAgents); err != nil {
		log.Errorw("Failed to extract stratum agents aspect", "targetID", targetID, "error", err)
		return request.Retry(err)
	}

	if stratumAgents.P4RTEndpoint == nil {
		log.Warnw("Cannot find P4RT endpoint to make a connection to the target", "targetID", targetID)
		return request.Retry(err)
	}

	// Connect to device using P4Runtime
	dest := &p4rtclient.Destination{
		TargetID: target.ID,
		Endpoint: stratumAgents.P4RTEndpoint,
		DeviceID: stratumAgents.DeviceID,
		RoleName: "provisioner",
	}

	return r.connect(ctx, request, dest)
}

func (r *Reconciler) connect(ctx context.Context, request controller.Request[topoapi.ID], dest *p4rtclient.Destination) controller.Directive[topoapi.ID] {
	log.Infow("Connecting to Target", "targetID", dest.TargetID)
	if _, err := r.conns.Connect(ctx, dest); err != nil {
		if !errors.IsAlreadyExists(err) {
			log.Errorw("Failed connecting to Target", "targetID", dest.TargetID, "error", err)
			return request.Retry(err)
		}
		log.Warnw("Failed connecting to Target", "targetID", dest.TargetID, "error", err)
		return request.Ack()
	}
	return request.Ack()
}

func (r *Reconciler) disconnect(ctx context.Context, request controller.Request[topoapi.ID], targetID topoapi.ID) controller.Directive[topoapi.ID] {
	log.Infow("Disconnecting from Target", "targetID", targetID)
	if err := r.conns.Disconnect(ctx, targetID); err != nil {
		if !errors.IsNotFound(err) {
			log.Errorw("Failed disconnecting from Target", "targetID", targetID, "error", err)
			return request.Retry(err)
		}
		log.Warnw("Failed disconnecting from Target", "targetID", targetID, "error", err)
		return request.Ack()
	}
	return request.Ack()
}
