// SPDX-FileCopyrightText: 2022-present Intel Corporation
//
// SPDX-License-Identifier: Apache-2.0

package pipelineconfig

import (
	"context"
	"github.com/atomix/go-sdk/pkg/test"
	"github.com/onosproject/onos-api/go/onos/provisioner"
	"github.com/stretchr/testify/assert"
	"os"
	"testing"
)

const (
	depth        = 16
	artifactsDir = "/tmp/artifacts"
)

func TestConfigStore(t *testing.T) {
	cluster := test.NewClient()
	defer cluster.Close()

	_ = os.RemoveAll(artifactsDir)
	assert.NoError(t, os.Mkdir(artifactsDir, 0755))

	store, err := NewAtomixStore(cluster, artifactsDir)
	assert.NoError(t, err)

	ctx := context.TODO()

	// List the objects; there should be none
	ch := make(chan *provisioner.ConfigRecord, depth)
	assert.NoError(t, store.List(ctx, "", ch))
	assert.Len(t, read(ch), 0)

	// Create few new configs
	pr1 := &provisioner.ConfigRecord{ConfigID: "fp_foo_spine", Kind: PipelineConfigKind, Artifacts: nil}
	pa1 := Artifacts{"p4info": []byte("p4info content"), "bin": []byte("device binary")}
	err = store.Add(ctx, pr1, pa1)
	assert.NoError(t, err)
	assert.Len(t, pr1.Artifacts, 2)

	pr2 := &provisioner.ConfigRecord{ConfigID: "fp_foo_leaf", Kind: PipelineConfigKind, Artifacts: nil}
	pa2 := Artifacts{"p4info": []byte("p4info content"), "bin": []byte("different device binary")}
	err = store.Add(ctx, pr2, pa2)
	assert.NoError(t, err)
	assert.Len(t, pr2.Artifacts, 2)

	cr1 := &provisioner.ConfigRecord{ConfigID: "ch_foo_leaf", Kind: ChassisConfigKind, Artifacts: nil}
	ca1 := Artifacts{"json": []byte("json content")}
	err = store.Add(ctx, cr1, ca1)
	assert.NoError(t, err)
	assert.Len(t, cr1.Artifacts, 1)

	// List all configurations; there should be 3
	ch = make(chan *provisioner.ConfigRecord, depth)
	assert.NoError(t, store.List(ctx, "", ch))
	assert.Len(t, read(ch), 3)

	cr, err := store.Get(ctx, "fp_foo_spine")
	assert.NoError(t, err)
	assert.Equal(t, provisioner.ConfigID("fp_foo_spine"), cr.ConfigID)
	assert.Equal(t, PipelineConfigKind, cr.Kind)
	assert.Len(t, cr.Artifacts, 2)

	ca, err := store.GetArtifacts(ctx, cr)
	assert.NoError(t, err)
	assert.Len(t, ca, 2)

	// List all pipeline configurations; there should be 2
	ch = make(chan *provisioner.ConfigRecord, depth)
	assert.NoError(t, store.List(ctx, PipelineConfigKind, ch))
	assert.Len(t, read(ch), 2)

	// List all chassis configurations; there should be 1
	ch = make(chan *provisioner.ConfigRecord, depth)
	assert.NoError(t, store.List(ctx, ChassisConfigKind, ch))
	assert.Len(t, read(ch), 1)

	// Delete one of the pipeline configurations
	assert.NoError(t, store.Delete(ctx, "fp_foo_spine"))

	// List all pipeline configurations; there should be 1
	ch = make(chan *provisioner.ConfigRecord, depth)
	assert.NoError(t, store.List(ctx, PipelineConfigKind, ch))
	assert.Len(t, read(ch), 1)

	// Test some bad things

	// Try to get a deleted item
	_, err = store.Get(ctx, "fp_foo_spine")
	assert.Error(t, err)

	// Try to delete item that was already deleted
	assert.Error(t, store.Delete(ctx, "fp_foo_spine"))

	// Try to delete item with an empty id
	assert.Error(t, store.Delete(ctx, ""))

	// Try to add an item that already exists
	assert.Error(t, store.Add(ctx, pr2, pa2))

	// Try to add item with bad record info
	assert.Error(t, store.Add(ctx, nil, nil))
	assert.Error(t, store.Add(ctx, &provisioner.ConfigRecord{}, nil))
	assert.Error(t, store.Add(ctx, &provisioner.ConfigRecord{}, pa1))
	assert.Error(t, store.Add(ctx, &provisioner.ConfigRecord{ConfigID: ""}, pa1))

	assert.NoError(t, store.Close())
}

func read(ch chan *provisioner.ConfigRecord) []*provisioner.ConfigRecord {
	records := make([]*provisioner.ConfigRecord, 0, 2)
	for record := range ch {
		records = append(records, record)
	}
	return records
}
