// SPDX-FileCopyrightText: 2022-present Intel Corporation
//
// SPDX-License-Identifier: Apache-2.0

package pipelineconfig

import (
	"context"
	"github.com/atomix/go-client/pkg/atomix"
	"github.com/atomix/go-client/pkg/generic"
	atomicmap "github.com/atomix/go-client/pkg/primitive/atomic/map"
	p4rtapi "github.com/onosproject/onos-api/go/onos/p4rt/v1"
	"github.com/onosproject/onos-lib-go/pkg/errors"
	"github.com/onosproject/onos-lib-go/pkg/logging"
	"time"
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
	pipelineconfigs atomicmap.Map[p4rtapi.PipelineConfigID, *p4rtapi.PipelineConfig]
}

func NewAtomixStore() (Store, error) {
	m1, err := atomix.AtomicMap[p4rtapi.PipelineConfigID, *p4rtapi.PipelineConfig]("device-provisioner-pipeline-configurations").
		Codec(generic.GoGoProto[*p4rtapi.PipelineConfig](&p4rtapi.PipelineConfig{})).
		Get(context.Background())

	if err != nil {
		return nil, errors.FromAtomix(err)
	}

	store := &configurationStore{
		pipelineconfigs: m1,
	}

	return store, nil

}

func (s *configurationStore) Get(ctx context.Context, id p4rtapi.PipelineConfigID) (*p4rtapi.PipelineConfig, error) {
	//TODO implement me
	panic("implement me")
}

func (c *configurationStore) Create(ctx context.Context, pipelineConfig *p4rtapi.PipelineConfig) error {
	if pipelineConfig.ID == "" {
		return errors.NewInvalid("no pipeline pipelineconfig ID specified")
	}
	if pipelineConfig.TargetID == "" {
		return errors.NewInvalid("no target ID specified")
	}
	if pipelineConfig.Revision != 0 {
		return errors.NewInvalid("cannot create pipeline pipelineconfig with revision")
	}
	if pipelineConfig.Version != 0 {
		return errors.NewInvalid("cannot create pipeline pipelineconfig with version")
	}
	pipelineConfig.Revision = 1
	pipelineConfig.Created = time.Now()
	pipelineConfig.Updated = time.Now()

	// Create the entry in the underlying map primitive.
	entry, err := c.pipelineconfigs.Put(ctx, pipelineConfig.ID, pipelineConfig)
	if err != nil {
		return errors.FromAtomix(err)
	}

	// Decode the pipleline pipelineconfig from the returned entry bytes.
	if err := decodePipelineConfiguration(entry, pipelineConfig); err != nil {
		return errors.NewInvalid("pipelineconfig decoding failed: %v", err)
	}

	return nil
}

func (s *configurationStore) Update(ctx context.Context, pipelineConfig *p4rtapi.PipelineConfig) error {
	//TODO implement me
	panic("implement me")
}

func (s *configurationStore) List(ctx context.Context) ([]*p4rtapi.PipelineConfig, error) {
	//TODO implement me
	panic("implement me")
}

func (s *configurationStore) Watch(ctx context.Context, ch chan<- p4rtapi.ConfigurationEvent, opts ...WatchOption) error {
	//TODO implement me
	panic("implement me")
}

func (s *configurationStore) UpdateStatus(ctx context.Context, pipelineConfig *p4rtapi.PipelineConfig) error {
	//TODO implement me
	panic("implement me")
}

func (s *configurationStore) Close(ctx context.Context) error {
	//TODO implement me
	panic("implement me")
}

func decodePipelineConfiguration(entry *atomicmap.Entry[p4rtapi.PipelineConfigID, *p4rtapi.PipelineConfig], pipelineConfig *p4rtapi.PipelineConfig) error {
	pipelineConfig.ID = entry.Key
	pipelineConfig.Key = string(entry.Key)
	pipelineConfig.Version = uint64(entry.Version)
	return nil
}
