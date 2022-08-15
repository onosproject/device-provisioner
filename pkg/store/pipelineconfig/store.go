// SPDX-FileCopyrightText: 2022-present Intel Corporation
//
// SPDX-License-Identifier: Apache-2.0

package pipelineconfig

import (
	"context"
	atomixclient "github.com/atomix/go-client/pkg/client"
	"github.com/atomix/go-client/pkg/generic"
	"github.com/atomix/go-client/pkg/primitive"
	atomicmap "github.com/atomix/go-client/pkg/primitive/atomic/map"
	p4rtapi "github.com/onosproject/onos-api/go/onos/p4rt/v1"
	"github.com/onosproject/onos-lib-go/pkg/errors"
	"github.com/onosproject/onos-lib-go/pkg/logging"
	"io"
	"time"
)

var log = logging.GetLogger()

// Store P4 pipeline pipelineconfig store interface
type Store interface {
	// Get gets the pipelineconfig intended for a given target ID
	Get(ctx context.Context, id p4rtapi.PipelineConfigID) (*p4rtapi.PipelineConfig, error)

	// Create creates a p4 pipeline pipeline config
	Create(ctx context.Context, pipelineConfig *p4rtapi.PipelineConfig) error

	// Update updates a p4 pipeline pipeline config
	Update(ctx context.Context, pipelineConfig *p4rtapi.PipelineConfig) error

	// List lists all the pipeline config
	List(ctx context.Context) ([]*p4rtapi.PipelineConfig, error)

	// Watch watches pipeline config changes
	Watch(ctx context.Context, ch chan<- *p4rtapi.PipelineConfig, opts ...WatchOption) error

	// UpdateStatus updates a pipeline config status
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
	pipelineConfigs atomicmap.Map[p4rtapi.PipelineConfigID, *p4rtapi.PipelineConfig]
}

// NewAtomixStore creates a new Atomix store for device pipeline configurations
func NewAtomixStore(client primitive.Client) (Store, error) {
	pipelineConfigsAtomicMap, err := atomixclient.AtomicMap[p4rtapi.PipelineConfigID, *p4rtapi.PipelineConfig](client)("device-provisioner-pipeline-configurations").
		Codec(generic.GoGoProto[*p4rtapi.PipelineConfig](&p4rtapi.PipelineConfig{})).
		Get(context.Background())

	if err != nil {
		return nil, errors.FromAtomix(err)
	}

	store := &configurationStore{
		pipelineConfigs: pipelineConfigsAtomicMap,
	}

	return store, nil

}

func (s *configurationStore) Get(ctx context.Context, id p4rtapi.PipelineConfigID) (*p4rtapi.PipelineConfig, error) {
	// If the pipeline config is not already in the cache, get it from the underlying primitive.
	entry, err := s.pipelineConfigs.Get(ctx, id)
	if err != nil {
		return nil, errors.FromAtomix(err)
	}

	// Decode and return the Configuration.
	configuration := entry.Value
	if err := decodePipelineConfiguration(entry, configuration); err != nil {
		return nil, errors.NewInvalid("pipeline config decoding failed: %v", err)
	}
	return entry.Value, nil
}

func (s *configurationStore) Create(ctx context.Context, pipelineConfig *p4rtapi.PipelineConfig) error {
	if pipelineConfig.ID == "" {
		return errors.NewInvalid("no pipeline pipelineconfig ID specified")
	}
	if pipelineConfig.TargetID == "" {
		return errors.NewInvalid("no target ID specified")
	}
	if pipelineConfig.Revision != 0 {
		return errors.NewInvalid("cannot create pipeline pipeline config with revision")
	}
	if pipelineConfig.Version != 0 {
		return errors.NewInvalid("cannot create pipeline pipeline config with version")
	}
	pipelineConfig.Revision = 1
	pipelineConfig.Created = time.Now()
	pipelineConfig.Updated = time.Now()
	// Create the entry in the underlying map primitive.
	entry, err := s.pipelineConfigs.Put(ctx, pipelineConfig.ID, pipelineConfig)
	if err != nil {
		return errors.FromAtomix(err)
	}

	// Decode the pipeline config from the returned entry.
	if err := decodePipelineConfiguration(entry, pipelineConfig); err != nil {
		return errors.NewInvalid("pipelineConfig decoding failed: %v", err)
	}
	return nil
}

func (s *configurationStore) Update(ctx context.Context, pipelineConfig *p4rtapi.PipelineConfig) error {
	if pipelineConfig.ID == "" {
		return errors.NewInvalid("no pipelineConfig ID specified")
	}
	if pipelineConfig.TargetID == "" {
		return errors.NewInvalid("no target ID specified")
	}
	if pipelineConfig.Revision == 0 {
		return errors.NewInvalid("pipelineConfig must contain a revision on update")
	}
	if pipelineConfig.Version == 0 {
		return errors.NewInvalid("pipelineconfig must contain a version on update")
	}
	pipelineConfig.Revision++
	pipelineConfig.Updated = time.Now()

	// Update the entry in the underlying map primitive using the pipelineconfig version
	// as an optimistic lock.
	entry, err := s.pipelineConfigs.Put(ctx, pipelineConfig.ID, pipelineConfig)
	if err != nil {
		return errors.FromAtomix(err)
	}

	// Decode the pipelineconfig from the returned entry bytes.
	if err := decodePipelineConfiguration(entry, pipelineConfig); err != nil {
		return errors.NewInvalid("pipeline config decoding failed: %v", err)
	}

	return nil
}

func (s *configurationStore) List(ctx context.Context) ([]*p4rtapi.PipelineConfig, error) {
	entryStream, err := s.pipelineConfigs.List(ctx)
	if err != nil {
		return nil, errors.FromAtomix(err)
	}

	var entries []*p4rtapi.PipelineConfig

	for {
		entry, err := entryStream.Next()
		if err == io.EOF {
			log.Warn("Entry stream is closed")
			break
		}
		entries = append(entries, entry.Value)
	}

	return entries, nil

}

func (s *configurationStore) Watch(ctx context.Context, ch chan<- *p4rtapi.PipelineConfig, opts ...WatchOption) error {
	entryStream, err := s.pipelineConfigs.Watch(ctx)
	if err != nil {
		return errors.FromAtomix(err)
	}

	go func() {
		for {
			entry, err := entryStream.Next()
			if err == io.EOF {
				log.Warn("Entry stream is closed")
				break
			}
			ch <- entry.Value
		}
	}()

	return nil

}

func (s *configurationStore) UpdateStatus(ctx context.Context, pipelineConfig *p4rtapi.PipelineConfig) error {
	//TODO implement me
	panic("implement me")
}

func (s *configurationStore) Close(ctx context.Context) error {
	err := s.pipelineConfigs.Close(ctx)
	if err != nil {
		return errors.FromAtomix(err)
	}
	return nil
}

func decodePipelineConfiguration(entry *atomicmap.Entry[p4rtapi.PipelineConfigID, *p4rtapi.PipelineConfig], pipelineConfig *p4rtapi.PipelineConfig) error {
	pipelineConfig.ID = entry.Key
	pipelineConfig.Key = string(entry.Key)
	pipelineConfig.Version = uint64(entry.Value.Revision)
	return nil
}
