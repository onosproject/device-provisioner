// SPDX-FileCopyrightText: 2023-present Intel Corporation
//
// SPDX-License-Identifier: Apache-2.0

package southbound

import (
	"context"
	"fmt"
	"github.com/onosproject/onos-api/go/onos/topo"
	utils "github.com/onosproject/onos-net-lib/pkg/gnmiutils"
	"github.com/openconfig/gnmi/proto/gnmi"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

// StratumGNMI is ab abstraction of a device with gNMI endpoint
type StratumGNMI struct {
	ID         topo.ID
	gnmiServer *topo.GNMIServer
	conn       *grpc.ClientConn
	client     gnmi.GNMIClient
	ctx        context.Context
}

// NewStratumGNMI creates a new stratum gNMI device descriptor from the specified topo entity
func NewStratumGNMI(object *topo.Object) (*StratumGNMI, error) {
	d := &StratumGNMI{
		ID:         object.ID,
		gnmiServer: &topo.GNMIServer{},
	}
	if err := object.GetAspect(d.gnmiServer); err != nil {
		return nil, err
	}
	return d, nil
}

// Connect establishes connection to the gNMI server
func (d *StratumGNMI) Connect() error {
	log.Infof("%s: connecting to gNMI server...", d.ID)
	opts := []grpc.DialOption{
		grpc.WithTransportCredentials(insecure.NewCredentials()), // TODO: Deal with secrets
	}

	var err error
	endpoint := d.gnmiServer.Endpoint
	d.conn, err = grpc.Dial(fmt.Sprintf("%s:%d", endpoint.Address, endpoint.Port), opts...)
	if err != nil {
		return err
	}

	d.client = gnmi.NewGNMIClient(d.conn)
	d.ctx = context.Background()

	log.Infof("%s: connected to gNMI server", d.ID)
	return nil
}

// Disconnect terminates the gNMI connection
func (d *StratumGNMI) Disconnect() error {
	return d.conn.Close()
}

// SetChassisConfig sets the chassis configuration on the device using previously established connection
func (d *StratumGNMI) SetChassisConfig(config []byte) error {
	_, err := d.client.Set(d.ctx, &gnmi.SetRequest{
		Replace: []*gnmi.Update{{
			Path: utils.ToPath(""),
			Val:  &gnmi.TypedValue{Value: &gnmi.TypedValue_BytesVal{BytesVal: config}},
		}},
	})
	return err
}
