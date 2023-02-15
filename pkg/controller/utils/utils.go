// SPDX-FileCopyrightText: 2022-present Intel Corporation
//
// SPDX-License-Identifier: Apache-2.0

// Package utils utility functions for controllers
package utils

import (
	"context"
	"github.com/gogo/protobuf/proto"
	configstore "github.com/onosproject/device-provisioner/pkg/store/configs"
	"github.com/onosproject/device-provisioner/pkg/store/topo"
	"github.com/onosproject/onos-api/go/onos/provisioner"
	topoapi "github.com/onosproject/onos-api/go/onos/topo"
	"github.com/onosproject/onos-lib-go/pkg/errors"
	"github.com/onosproject/onos-lib-go/pkg/logging"
	"github.com/onosproject/onos-net-lib/pkg/realm"
)

var log = logging.GetLogger()

// GetArtifacts Retrieves the required configuration artifacts from the store
func GetArtifacts(ctx context.Context, configStore configstore.ConfigStore, configID provisioner.ConfigID, expectedNumber int) (configstore.Artifacts, error) {
	record, err := configStore.Get(ctx, configID)
	if err != nil {
		log.Warnw("Unable to retrieve pipeline configuration", "configID", configID, "error", err)
		return nil, err
	}

	// ... and the associated artifacts
	artifacts, err := configStore.GetArtifacts(ctx, record)
	if err != nil {
		log.Warnw("Unable to retrieve pipeline config artifacts", "configID", configID, "error", err)
		return nil, err
	}

	// Make sure the number of artifacts is sufficient
	if len(artifacts) < expectedNumber {
		log.Warnw("Insufficient number of config artifacts found", "Number of artifacts", len(artifacts))
		return nil, errors.NewInvalid("Insufficient number of config artifacts found")
	}
	return artifacts, err
}

// RealmQueryFilter Returns filters for matching objects on realm label, entity type and with DeviceConfig aspect.
func RealmQueryFilter(realmOptions *realm.Options) *topoapi.Filters {
	return realmOptions.QueryFilter("onos.provisioner.DeviceConfig", "onos.topo.StratumAgents")
}

// UpdateObjectAspect the topo object with the specified configuration aspect
func UpdateObjectAspect(ctx context.Context, topo topo.Store, object *topoapi.Object, kind string, aspect proto.Message) error {
	log.Infow("Updating  aspect", "kind", kind, "targetID", object.ID)
	entity, err := topo.Get(ctx, object.ID)
	if err != nil {
		if !errors.IsNotFound(err) {
			log.Warnw("Unable to get object", "targetID", object.ID, "error", err)
			return err
		}
		log.Warnw("Cannot find target object", "targetID", object.ID)
		return nil
	}

	if err = entity.SetAspect(aspect); err != nil {
		log.Warnw("Unable to set aspect", "kind", kind, "targetID", object.ID, "error", err)
		return err
	}
	err = topo.Update(ctx, entity)
	if err != nil {
		if !errors.IsNotFound(err) && !errors.IsConflict(err) {
			log.Warnw("Unable to update configuration for object", "kind", kind, "targetID", object.ID, "error", err)
			return err
		}
		log.Warnw("Write conflict updating entity aspect", "kind", kind, "targetID", object.ID, "error", err)
		return nil
	}
	return nil
}
