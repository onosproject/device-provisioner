// SPDX-FileCopyrightText: 2022-present Intel Corporation
//
// SPDX-License-Identifier: Apache-2.0

package southbound

import (
	topoapi "github.com/onosproject/onos-api/go/onos/topo"
	"github.com/stretchr/testify/assert"
	"testing"
)

func TestP4ServerAspect(t *testing.T) {
	p4s := &topoapi.P4RuntimeServer{
		Endpoint: &topoapi.Endpoint{
			Address: "fabric-sim",
			Port:    20000,
		},
		DeviceID: 1,
	}
	o := &topoapi.Object{
		ID:       "leaf1",
		Revision: 0,
		Type:     topoapi.Object_ENTITY,
	}
	assert.NoError(t, o.SetAspect(p4s))
	t.Log(o)
}
