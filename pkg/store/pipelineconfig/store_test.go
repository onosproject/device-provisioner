// SPDX-FileCopyrightText: 2022-present Intel Corporation
//
// SPDX-License-Identifier: Apache-2.0

package pipelineconfig

import (
	"context"
	"github.com/atomix/go-client/pkg/test"
	p4rtapi "github.com/onosproject/onos-api/go/onos/p4rt/v1"
	"github.com/stretchr/testify/assert"
	"testing"
	"time"
)

const (
	pipelineConfigID1 = p4rtapi.PipelineConfigID("pipeline-config-switch1")
	targetID1         = p4rtapi.TargetID("switch1")
	pipelineConfigID2 = p4rtapi.PipelineConfigID("pipeline-config-switch2")
	targetID2         = p4rtapi.TargetID("switch2")
)

func TestNewAtomixStore(t *testing.T) {
	client := test.NewClient()
	defer client.Cleanup()

	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()

	store, err := NewAtomixStore(client)
	assert.NoError(t, err)
	ch := make(chan *p4rtapi.PipelineConfig)
	err = store.Watch(ctx, ch)
	assert.NoError(t, err)

	deviceConfig1 := &p4rtapi.PipelineConfig{
		ID:       pipelineConfigID1,
		TargetID: targetID1,
		Cookie: &p4rtapi.Cookie{
			Cookie: uint64(123),
		},
		Spec: &p4rtapi.PipelineConfigSpec{
			P4DeviceConfig: []byte{},
			P4Info:         []byte{},
		},
		Status: p4rtapi.PipelineConfigStatus{
			State: p4rtapi.PipelineConfigStatus_PENDING,
		},
	}

	err = store.Create(ctx, deviceConfig1)
	assert.NoError(t, err)
	t.Log(<-ch)

	pipelineConfig1, err := store.Get(ctx, pipelineConfigID1)
	assert.NoError(t, err)
	assert.Equal(t, deviceConfig1.ID, pipelineConfig1.ID)
	assert.Equal(t, deviceConfig1.TargetID, pipelineConfig1.TargetID)
	assert.Equal(t, deviceConfig1.Cookie.Cookie, pipelineConfig1.Cookie.Cookie)

	deviceConfig2 := &p4rtapi.PipelineConfig{
		ID:       pipelineConfigID2,
		TargetID: targetID2,
		Cookie: &p4rtapi.Cookie{
			Cookie: uint64(124),
		},
		Spec: &p4rtapi.PipelineConfigSpec{
			P4DeviceConfig: []byte{},
			P4Info:         []byte{},
		},
		Status: p4rtapi.PipelineConfigStatus{
			State: p4rtapi.PipelineConfigStatus_PENDING,
		},
	}

	err = store.Create(ctx, deviceConfig2)
	assert.NoError(t, err)

	pipelineConfigList, err := store.List(ctx)
	assert.NoError(t, err)

	assert.Equal(t, 2, len(pipelineConfigList))

}
