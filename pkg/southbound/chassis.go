// SPDX-FileCopyrightText: 2023-present Intel Corporation
//
// SPDX-License-Identifier: Apache-2.0

package southbound

import (
	"github.com/onosproject/onos-api/go/onos/topo"
	utils "github.com/onosproject/onos-net-lib/pkg/gnmiutils"
	"github.com/onosproject/onos-net-lib/pkg/stratum"
	"github.com/openconfig/gnmi/proto/gnmi"
)

// SetChassisConfig sets the chassis configuration on the device via gNMI
func SetChassisConfig(object *topo.Object, config []byte) error {
	// Connect to the device using gNMI
	device, err := stratum.NewGNMI(object, true)
	if err != nil {
		log.Warnf("Unable to connect to Stratum device gNMI %s: %+v", object.ID, err)
		return err
	}
	defer device.Disconnect()

	_, err = device.Client.Set(device.Context, &gnmi.SetRequest{
		Replace: []*gnmi.Update{{
			Path: utils.ToPath(""),
			Val:  &gnmi.TypedValue{Value: &gnmi.TypedValue_BytesVal{BytesVal: config}},
		}},
	})
	return err
}
