// SPDX-FileCopyrightText: 2022-present Intel Corporation
//
// SPDX-License-Identifier: Apache-2.0

package pipelineconfig

import (
	"context"
	"github.com/atomix/go-client/pkg/atomix/test"
	api "github.com/atomix/runtime/api/atomix/runtime/map/v1"
	"github.com/stretchr/testify/assert"
	"testing"
	"time"
)

func TestNewAtomixStore(t *testing.T) {
	cluster := test.NewCluster()
	defer cluster.Cleanup()

	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()

	conn1, err := cluster.Connect(ctx)
	assert.NoError(t, err)
	client1 := api.NewMapClient(conn1)

	_, err = NewAtomixStore(client1)
	assert.NoError(t, err)

	//store.Create(ctx, &p4rtapi.PipelineConfig{})

}
