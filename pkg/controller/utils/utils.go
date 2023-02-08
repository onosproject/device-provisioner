// SPDX-FileCopyrightText: 2022-present Intel Corporation
//
// SPDX-License-Identifier: Apache-2.0

// Package utils utility functions for controllers
package utils

import (
	"context"
	configstore "github.com/onosproject/device-provisioner/pkg/store/pipelineconfig"
	"github.com/onosproject/onos-api/go/onos/provisioner"
	topoapi "github.com/onosproject/onos-api/go/onos/topo"
	"github.com/onosproject/onos-lib-go/pkg/errors"
	"github.com/onosproject/onos-lib-go/pkg/logging"
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
func RealmQueryFilter(realmLabel string, realmValue string) *topoapi.Filters {
	return &topoapi.Filters{
		LabelFilters: []*topoapi.Filter{{
			Filter: &topoapi.Filter_Equal_{Equal_: &topoapi.EqualFilter{Value: realmValue}},
			Key:    realmLabel,
		}},
		ObjectTypes: []topoapi.Object_Type{topoapi.Object_ENTITY},
		WithAspects: []string{"onos.provisioner.DeviceConfig", "onos.topo.StratumAgents"},
	}
}
