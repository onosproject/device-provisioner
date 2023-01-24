// SPDX-FileCopyrightText: 2022-present Intel Corporation
//
// SPDX-License-Identifier: Apache-2.0

// Package store contains the stores for tracking pipeline and chassis configurations and their deployments
package store

import (
	"context"
	"fmt"
	"github.com/atomix/go-sdk/pkg/generic"
	"github.com/atomix/go-sdk/pkg/primitive"
	_map "github.com/atomix/go-sdk/pkg/primitive/map"
	"os"
	"time"

	"github.com/onosproject/onos-api/go/onos/provisioner"
	"github.com/onosproject/onos-lib-go/pkg/errors"
	"github.com/onosproject/onos-lib-go/pkg/logging"
	"io"
)

var log = logging.GetLogger()

const (
	// PipelineConfigKind represents pipeline configurations
	PipelineConfigKind = "pipeline"
	// ChassisConfigKind represents chassis configurations
	ChassisConfigKind = "chassis"
)

const (
	artifactFormat   = "%s/%s/%s.%s" // root/kind/id.type
	artifactDirPerms = 0755
	artifactPerms    = 0644
)

// Artifacts is a map of artifact type to artifact content
type Artifacts map[string][]byte

// ConfigStore is an abstraction for tracking inventory of pipeline and chassis configurations
type ConfigStore interface {
	io.Closer

	// Add registers a new configuration in the inventory
	Add(ctx context.Context, record *provisioner.ConfigRecord, artifacts Artifacts) error

	// Delete removes the specified configuration from the inventory
	Delete(ctx context.Context, configID provisioner.ConfigID) error

	// Get returns the specified configuration record
	Get(ctx context.Context, configID provisioner.ConfigID) (*provisioner.ConfigRecord, error)

	// GetArtifacts returns the specified configuration artifacts
	GetArtifacts(ctx context.Context, record *provisioner.ConfigRecord) (Artifacts, error)

	// List streams all registered configuration records of the requested kind (pipeline or chassis)
	List(ctx context.Context, kind string, ch chan *provisioner.ConfigRecord) error
}

// NewAtomixStore returns a new persistent store for configuration records whose artifacts
// are stored in the give artifacts directory
func NewAtomixStore(client primitive.Client, artifactsDirPath string) (ConfigStore, error) {
	configs, err := _map.NewBuilder[provisioner.ConfigID, *provisioner.ConfigRecord](client, "onos-device-configs").
		Tag("device-provisioner", "device-configs").
		Codec(generic.Proto[*provisioner.ConfigRecord](&provisioner.ConfigRecord{})).
		Get(context.Background())
	if err != nil {
		return nil, errors.FromAtomix(err)
	}

	_ = os.Mkdir(fmt.Sprintf("%s/%s", artifactsDirPath, PipelineConfigKind), artifactDirPerms)
	_ = os.Mkdir(fmt.Sprintf("%s/%s", artifactsDirPath, ChassisConfigKind), artifactDirPerms)

	store := &atomixStore{
		configs: configs,
		path:    artifactsDirPath,
	}
	return store, nil
}

// atomixStore is the object implementation of the ConfigStore
type atomixStore struct {
	configs _map.Map[provisioner.ConfigID, *provisioner.ConfigRecord]
	path    string
}

// Add registers a new configuration in the inventory
func (s *atomixStore) Add(ctx context.Context, record *provisioner.ConfigRecord, artifacts Artifacts) error {
	if record == nil || len(artifacts) == 0 {
		return errors.NewInvalid("Record or Artifacts cannot be empty")
	}
	if record.ConfigID == "" {
		return errors.NewInvalid("ConfigID cannot be empty")
	}
	if record.Kind != PipelineConfigKind && record.Kind != ChassisConfigKind {
		return errors.NewInvalid("Kind must be 'pipeline' or 'chassis'")
	}

	log.Infof("Adding configuration '%s'", record.ConfigID)
	if err := s.saveArtifacts(record, artifacts); err != nil {
		_, _ = s.configs.Remove(ctx, record.ConfigID)
		return err
	}

	_, err := s.configs.Insert(ctx, record.ConfigID, record)
	if err != nil {
		err = errors.FromAtomix(err)
		if !errors.IsAlreadyExists(err) {
			log.Errorf("Failed to add configuration %+v: %v", record, err)
		} else {
			log.Warnf("Failed to add configuration %+v: %v", record, err)
		}
		return err
	}
	return nil
}

// Delete removes the specified configuration from the inventory
func (s *atomixStore) Delete(ctx context.Context, configID provisioner.ConfigID) error {
	if configID == "" {
		return errors.NewInvalid("ConfigID cannot be empty")
	}

	log.Infof("Deleting configuration '%s'", configID)
	entry, err := s.configs.Remove(ctx, configID)
	if err != nil {
		err = errors.FromAtomix(err)
		if !errors.IsNotFound(err) && !errors.IsConflict(err) {
			log.Errorf("Failed to delete configuration '%s': %v", configID, err)
		} else {
			log.Warnf("Failed to delete configuration '%s': %v", configID, err)
		}
		return err
	}
	s.deleteArtifacts(entry.Value)
	return nil
}

// Get returns the specified configuration
func (s *atomixStore) Get(ctx context.Context, configID provisioner.ConfigID) (*provisioner.ConfigRecord, error) {
	if configID == "" {
		return nil, errors.NewInvalid("ConfigID cannot be empty")
	}

	entry, err := s.configs.Get(ctx, configID)
	if err != nil {
		err = errors.FromAtomix(err)
		if !errors.IsNotFound(err) {
			log.Errorf("Failed to get configuration '%s': %v", configID, err)
		} else {
			log.Warnf("Failed to get configuration '%s': %v", configID, err)
		}
		return nil, err
	}
	record := entry.Value
	return record, nil
}

// GetArtifacts returns the specified configuration artifacts
func (s *atomixStore) GetArtifacts(ctx context.Context, record *provisioner.ConfigRecord) (Artifacts, error) {
	return s.loadArtifacts(record)
}

// List streams all registered configurations of the requested kind (pipeline or chassis)
func (s *atomixStore) List(ctx context.Context, kind string, ch chan *provisioner.ConfigRecord) error {
	stream, err := s.configs.List(ctx)
	if err != nil {
		return errors.FromAtomix(err)
	}

	for {
		entry, err := stream.Next()
		if err == io.EOF {
			close(ch)
			return nil
		}
		if err != nil {
			return errors.FromAtomix(err)
		}
		record := entry.Value
		if len(kind) == 0 || record.Kind == kind {
			ch <- entry.Value
		}
	}
}

// Close closes the store and any backing assets
func (s *atomixStore) Close() error {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	err := s.configs.Close(ctx)
	if err != nil {
		return errors.FromAtomix(err)
	}
	return nil

}

// Utilities for storing and reading configuration artifacts to/from files

// Generates artifact file path from the record and the specified artifact type
func (s *atomixStore) artifactPath(record *provisioner.ConfigRecord, artifactType string) string {
	return fmt.Sprintf(artifactFormat, s.path, record.Kind, record.ConfigID, artifactType)
}

// Saves all artifact bytes into files and updates the record of artifacts
func (s *atomixStore) saveArtifacts(record *provisioner.ConfigRecord, artifacts Artifacts) error {
	// TODO: add lock file
	record.Artifacts = make([]string, 0, len(artifacts))
	for artifact, data := range artifacts {
		path := s.artifactPath(record, artifact)
		if err := os.WriteFile(path, data, artifactPerms); err != nil {
			log.Errorf("Unable to write artifact: %+v", err)
			s.deleteArtifacts(record)
			return err
		}
		record.Artifacts = append(record.Artifacts, artifact)
	}
	return nil
}

// Loads artifact files using the configuration record into memory as artifact bytes
func (s *atomixStore) loadArtifacts(record *provisioner.ConfigRecord) (Artifacts, error) {
	artifacts := make(map[string][]byte, len(record.Artifacts))
	for _, artifact := range record.Artifacts {
		path := s.artifactPath(record, artifact)
		data, err := os.ReadFile(path)
		if err != nil {
			log.Errorf("Unable to load artifact: %+v", err)
			return nil, err
		}
		artifacts[artifact] = data
	}
	return artifacts, nil
}

// Deletes all artifacts associated with the specified configuration record
func (s *atomixStore) deleteArtifacts(record *provisioner.ConfigRecord) {
	for _, artifact := range record.Artifacts {
		_ = os.Remove(s.artifactPath(record, artifact))
	}
}
