// SPDX-FileCopyrightText: 2022-present Intel Corporation
//
// SPDX-License-Identifier: Apache-2.0

// Package target controller
package target

import (
	"context"
	"github.com/onosproject/device-provisioner/pkg/store/topo"
	topoapi "github.com/onosproject/onos-api/go/onos/topo"
	"github.com/onosproject/onos-lib-go/pkg/controller"
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

// NewController returns a new gNMI connection  controller
func NewController(topo topo.Store, conns p4rtclient.ConnManager, realmOptions *realm.Options) *controller.Controller {
	c := controller.NewController("target")
	c.Watch(&TopoWatcher{
		topo: topo,
	})
	c.Watch(&ConnWatcher{
		conns: conns,
	})
	c.Reconcile(&Reconciler{
		conns: conns,
		topo:  topo,
	})
	return c
}

// Reconciler reconciles P4RT connections
type Reconciler struct {
	conns p4rtclient.ConnManager
	topo  topo.Store
}

// Reconcile reconciles a connection for a P4RT target
func (r *Reconciler) Reconcile(id controller.ID) (controller.Result, error) {
	ctx, cancel := context.WithTimeout(context.Background(), defaultTimeout)
	defer cancel()

	targetID := id.Value.(topoapi.ID)
	log.Infow("Reconciling Target Connections", "targetID", targetID)
	target, err := r.topo.Get(ctx, targetID)
	if err != nil {
		if !errors.IsNotFound(err) {
			log.Errorw("Failed reconciling Target Connections", "targetID", targetID, "error", err)
			return controller.Result{}, err
		}
		return r.disconnect(ctx, targetID)
	}

	stratumAgents := &topoapi.StratumAgents{}
	if err := target.GetAspect(stratumAgents); err != nil {
		log.Errorw("Failed to extract stratum agents aspect", "targetID", targetID, "error", err)
		return controller.Result{}, err
	}

	if stratumAgents.P4RTEndpoint == nil {
		log.Errorw("Cannot find P4RT endpoint to make a connection to the target", "targetID", targetID)
		return controller.Result{}, err
	}

	// Connect to device using P4Runtime
	dest := &p4rtclient.Destination{
		TargetID: target.ID,
		Endpoint: stratumAgents.P4RTEndpoint,
		DeviceID: stratumAgents.DeviceID,
		RoleName: "provisioner",
	}

	return r.connect(ctx, dest)
}

func (r *Reconciler) connect(ctx context.Context, dest *p4rtclient.Destination) (controller.Result, error) {
	log.Infow("Connecting to Target", "targetID", dest.TargetID)
	if _, err := r.conns.Connect(ctx, dest); err != nil {
		if !errors.IsAlreadyExists(err) {
			log.Errorw("Failed connecting to Target", "targetID", dest.TargetID, "error", err)
			return controller.Result{}, err
		}
		log.Warnw("Failed connecting to Target", "targetID", dest.TargetID, "error", err)
		return controller.Result{}, nil
	}
	return controller.Result{}, nil
}

func (r *Reconciler) disconnect(ctx context.Context, targetID topoapi.ID) (controller.Result, error) {
	log.Infow("Disconnecting from Target", "targetID", targetID)
	if err := r.conns.Disconnect(ctx, targetID); err != nil {
		if !errors.IsNotFound(err) {
			log.Errorw("Failed disconnecting from Target", "targetID", targetID, "error", err)
			return controller.Result{}, err
		}
		log.Warnw("Failed disconnecting from Target", "targetID", targetID, "error", err)
		return controller.Result{}, nil
	}
	return controller.Result{}, nil
}
