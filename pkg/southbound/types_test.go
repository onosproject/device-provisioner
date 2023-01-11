// SPDX-FileCopyrightText: 2022-present Intel Corporation
//
// SPDX-License-Identifier: Apache-2.0

package southbound

import (
	"github.com/onosproject/onos-api/go/onos/topo"
	"github.com/stretchr/testify/assert"
	"testing"
)

func TestP4ServerAspect(t *testing.T) {
	p4s := &topo.P4RuntimeServer{
		Endpoint: &topo.Endpoint{
			Address: "fabric-sim",
			Port:    20000,
		},
		DeviceID: 1,
	}
	o := &topo.Object{
		ID:       "leaf1",
		Revision: 0,
		Type:     topo.Object_ENTITY,
	}
	assert.NoError(t, o.SetAspect(p4s))
	t.Log(o)
}
