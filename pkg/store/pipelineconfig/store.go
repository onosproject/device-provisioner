// SPDX-FileCopyrightText: 2022-present Intel Corporation
//
// SPDX-License-Identifier: Apache-2.0

package pipelineconfig

import (
	"context"
	_map "github.com/atomix/go-client/pkg/atomix/map"
	api "github.com/atomix/runtime/api/atomix/runtime/map/v1"
	p4rtapi "github.com/onosproject/onos-api/go/onos/p4rt/v1"
	"github.com/onosproject/onos-lib-go/pkg/errors"
	"github.com/onosproject/onos-lib-go/pkg/logging"
)

var log = logging.GetLogger()

// Store P4 pipeline pipelineconfig store interface
type Store interface {
	// Get gets the pipelineconfig intended for a given target ID
	Get(ctx context.Context, id p4rtapi.PipelineConfigID) (*p4rtapi.PipelineConfig, error)

	// Create creates a p4 pipeline pipelineconfig
	Create(ctx context.Context, pipelineConfig *p4rtapi.PipelineConfig) error

	// Update updates a p4 pipeline pipelineconfig
	Update(ctx context.Context, pipelineConfig *p4rtapi.PipelineConfig) error

	// List lists all the pipelineconfig
	List(ctx context.Context) ([]*p4rtapi.PipelineConfig, error)

	// Watch watches pipelineconfig changes
	Watch(ctx context.Context, ch chan<- p4rtapi.ConfigurationEvent, opts ...WatchOption) error

	// UpdateStatus updates a pipelineconfig status
	UpdateStatus(ctx context.Context, pipelineConfig *p4rtapi.PipelineConfig) error

	Close(ctx context.Context) error
}

type watchOptions struct {
	configurationID p4rtapi.PipelineConfigID
	replay          bool
}

// WatchOption is a pipelineconfig option for Watch calls
type WatchOption interface {
	apply(*watchOptions)
}

// watchReplyOption is an option to replay events on watch
type watchReplayOption struct {
}

func (o watchReplayOption) apply(options *watchOptions) {
	options.replay = true
}

// WithReplay returns a WatchOption that replays past changes
func WithReplay() WatchOption {
	return watchReplayOption{}
}

type watchIDOption struct {
	id p4rtapi.PipelineConfigID
}

func (o watchIDOption) apply(options *watchOptions) {
	options.configurationID = o.id
}

// WithPipelineConfigID returns a Watch option that watches for configurations based on a given pipeline pipelineconfig ID
func WithPipelineConfigID(id p4rtapi.PipelineConfigID) WatchOption {
	return watchIDOption{id: id}
}

type configurationStore struct {
	pipelineConfigs _map.Map[p4rtapi.PipelineConfigID, *p4rtapi.PipelineConfig]
}

func NewAtomixStore(client api.MapClient) (Store, error) {
	pipelineConfigs, err := _map.New[p4rtapi.PipelineConfigID, *p4rtapi.PipelineConfig](client)(context.Background(),
		"device-provisioner-pipeline-configurations",
		_map.WithKeyType[p4rtapi.PipelineConfigID, *p4rtapi.PipelineConfig]())
	log.Warn(err)
	if err != nil {
		return nil, errors.FromAtomix(err)
	}
	store := &configurationStore{
		pipelineConfigs: pipelineConfigs,
	}

	return store, nil

}

func (c *configurationStore) Get(ctx context.Context, id p4rtapi.PipelineConfigID) (*p4rtapi.PipelineConfig, error) {
	//TODO implement me
	panic("implement me")
}

func (c *configurationStore) Create(ctx context.Context, pipelineConfig *p4rtapi.PipelineConfig) error {
	log.Infow("Creating a pipelineconfig", "pipelineconfig", pipelineConfig)
	return nil
}

func (c *configurationStore) Update(ctx context.Context, pipelineConfig *p4rtapi.PipelineConfig) error {
	//TODO implement me
	panic("implement me")
}

func (c *configurationStore) List(ctx context.Context) ([]*p4rtapi.PipelineConfig, error) {
	//TODO implement me
	panic("implement me")
}

func (c *configurationStore) Watch(ctx context.Context, ch chan<- p4rtapi.ConfigurationEvent, opts ...WatchOption) error {
	//TODO implement me
	panic("implement me")
}

func (c *configurationStore) UpdateStatus(ctx context.Context, pipelineConfig *p4rtapi.PipelineConfig) error {
	//TODO implement me
	panic("implement me")
}

func (c *configurationStore) Close(ctx context.Context) error {
	//TODO implement me
	panic("implement me")
}
