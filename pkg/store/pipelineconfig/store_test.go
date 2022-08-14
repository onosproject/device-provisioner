// SPDX-FileCopyrightText: 2022-present Intel Corporation
//
// SPDX-License-Identifier: Apache-2.0

package pipelineconfig

import (
	"context"
	"github.com/atomix/go-client/pkg/atomix/test"
	p4rtapi "github.com/onosproject/onos-api/go/onos/p4rt/v1"
	"github.com/stretchr/testify/assert"
	"testing"
	"time"
)

func TestNewAtomixStore(t *testing.T) {
	cluster := test.NewCluster()
	defer cluster.Cleanup()

	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	store, err := NewAtomixStore()
	assert.NoError(t, err)

	err = store.Create(ctx, &p4rtapi.PipelineConfig{})
	assert.NoError(t, err)

}
