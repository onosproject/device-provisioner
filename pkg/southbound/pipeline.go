// SPDX-FileCopyrightText: 2022-present Intel Corporation
//
// SPDX-License-Identifier: Apache-2.0

// Package southbound contains code for updating P4 pipeline and chassis configuration on Stratum devices
package southbound

import (
	"context"
	"fmt"
	"github.com/onosproject/onos-api/go/onos/topo"
	"github.com/onosproject/onos-lib-go/pkg/errors"
	"github.com/onosproject/onos-lib-go/pkg/logging"
	"github.com/onosproject/onos-net-lib/pkg/p4utils"
	p4info "github.com/p4lang/p4runtime/go/p4/config/v1"
	p4api "github.com/p4lang/p4runtime/go/p4/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/protobuf/encoding/prototext"
	"time"
)

var log = logging.GetLogger("southbound")

// TODO: Extract this to onos-net-lib at some point; with appropriate generalizations of course

// StratumP4 is ab abstraction of a device with P4Runtime endpoint
type StratumP4 struct {
	ID         string
	DeviceID   uint64
	ElectionID *p4api.Uint128
	Context    context.Context
	RoleName   string
	endpoint   *topo.Endpoint
	conn       *grpc.ClientConn
	client     p4api.P4RuntimeClient
	stream     p4api.P4Runtime_StreamChannelClient
}

// NewStratumP4 creates a new stratum P4 device descriptor from the specified topo entity
func NewStratumP4(object *topo.Object, roleName string) (*StratumP4, error) {
	stratumAgents := &topo.StratumAgents{}
	if err := object.GetAspect(stratumAgents); err != nil {
		return nil, err
	}
	d := &StratumP4{
		ID:       string(object.ID),
		DeviceID: stratumAgents.DeviceID,
		endpoint: stratumAgents.P4RTEndpoint,
		RoleName: roleName,
	}
	return d, nil
}

// Connect establishes connection to the P4Runtime server
func (d *StratumP4) Connect() error {
	log.Infof("%s: connecting to P4Runtime server...", d.ID)
	opts := []grpc.DialOption{
		grpc.WithTransportCredentials(insecure.NewCredentials()), // TODO: Deal with secrets
	}

	var err error
	d.conn, err = grpc.Dial(fmt.Sprintf("%s:%d", d.endpoint.Address, d.endpoint.Port), opts...)
	if err != nil {
		return err
	}

	d.client = p4api.NewP4RuntimeClient(d.conn)
	d.Context = context.Background()

	// Establish stream and issue mastership
	if d.stream, err = d.client.StreamChannel(d.Context); err != nil {
		return err
	}

	d.ElectionID = p4utils.TimeBasedElectionID()
	role := p4utils.NewStratumRole(d.RoleName, 0, []byte{}, false, true)
	if err = d.stream.Send(p4utils.CreateMastershipArbitration(d.ElectionID, role)); err != nil {
		return err
	}

	var msg *p4api.StreamMessageResponse
	if msg, err = d.stream.Recv(); err != nil {
		return err
	}
	mar := msg.GetArbitration()
	if mar == nil {
		return errors.NewInvalid("%s: did not receive mastership arbitration", d.ID)
	}
	if mar.ElectionId == nil || mar.ElectionId.High != d.ElectionID.High || mar.ElectionId.Low != d.ElectionID.Low {
		return errors.NewInvalid("%s: did not win election", d.ID)
	}

	log.Infof("%s: connected to P4Runtime server", d.ID)
	return nil
}

// Disconnect terminates the P4 message stream
func (d *StratumP4) Disconnect() error {
	return d.conn.Close()
}

// ReconcilePipelineConfig makes sure that the device has the given P4 pipeline configuration applied
func (d *StratumP4) ReconcilePipelineConfig(info []byte, binary []byte, cookie uint64) (uint64, error) {
	log.Infof("%s: configuring pipeline...", d.ID)
	// ask for the pipeline config cookie
	gr, err := d.client.GetForwardingPipelineConfig(d.Context, &p4api.GetForwardingPipelineConfigRequest{
		DeviceId:     d.DeviceID,
		ResponseType: p4api.GetForwardingPipelineConfigRequest_COOKIE_ONLY,
	})
	if err != nil {
		return 0, err
	}

	// if that matches our cookie, we're good
	if cookie == gr.Config.Cookie.Cookie && cookie > 0 {
		return cookie, nil
	}

	// otherwise unmarshal the P4Info
	p4i := &p4info.P4Info{}
	if err = prototext.Unmarshal(info, p4i); err != nil {
		return 0, err
	}

	// and then apply it to the device
	newCookie := uint64(time.Now().UnixNano())
	_, err = d.client.SetForwardingPipelineConfig(d.Context, &p4api.SetForwardingPipelineConfigRequest{
		DeviceId:   d.DeviceID,
		Role:       d.RoleName,
		ElectionId: d.ElectionID,
		Action:     p4api.SetForwardingPipelineConfigRequest_VERIFY_AND_COMMIT,
		Config: &p4api.ForwardingPipelineConfig{
			P4Info:         p4i,
			P4DeviceConfig: binary,
			Cookie:         &p4api.ForwardingPipelineConfig_Cookie{Cookie: newCookie},
		},
	})
	if err != nil {
		return 0, nil
	}
	log.Infof("%s: pipeline configured with cookie %d", d.ID, newCookie)
	return newCookie, err
}
