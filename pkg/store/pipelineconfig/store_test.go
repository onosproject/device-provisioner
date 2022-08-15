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
	client1 := test.NewClient()
	defer client1.Cleanup()

	/*client2 := test.NewClient()
	defer client2.Cleanup()*/

	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()

	store1, err := NewAtomixStore(client1)
	assert.NoError(t, err)
	assert.NotNil(t, store1)

	/*store2, err := NewAtomixStore(client2)
	assert.NoError(t, err)
	assert.NotNil(t, store2)*/

	ch1 := make(chan *p4rtapi.PipelineConfig)
	err = store1.Watch(ctx, ch1)
	assert.NoError(t, err)

	/*ch2 := make(chan *p4rtapi.PipelineConfig)
	err = store2.Watch(ctx, ch2)
	assert.NoError(t, err)*/

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

	err = store1.Create(ctx, deviceConfig1)
	assert.NoError(t, err)
	event := <-ch1
	assert.Equal(t, targetID1, event.TargetID)

	pipelineConfig1, err := store1.Get(ctx, pipelineConfigID1)
	assert.NoError(t, err)
	assert.Equal(t, deviceConfig1.ID, pipelineConfig1.ID)
	assert.Equal(t, deviceConfig1.TargetID, pipelineConfig1.TargetID)
	assert.Equal(t, deviceConfig1.Cookie.Cookie, pipelineConfig1.Cookie.Cookie)

	assert.Equal(t, p4rtapi.PipelineConfigStatus_PENDING, pipelineConfig1.Status.State)
	pipelineConfig1.Status.State = p4rtapi.PipelineConfigStatus_COMPLETE
	err = store1.UpdateStatus(ctx, pipelineConfig1)
	assert.NoError(t, err)
	event = <-ch1
	assert.Equal(t, targetID1, event.TargetID)
	assert.Equal(t, p4rtapi.PipelineConfigStatus_COMPLETE, event.Status.State)

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

	err = store1.Create(ctx, deviceConfig2)
	assert.NoError(t, err)

	event = <-ch1
	assert.Equal(t, targetID2, event.TargetID)

	pipelineConfigList, err := store1.List(ctx)
	assert.NoError(t, err)
	assert.Equal(t, 2, len(pipelineConfigList))

}
