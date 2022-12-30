// SPDX-FileCopyrightText: 2022-present Intel Corporation
//
// SPDX-License-Identifier: Apache-2.0

// Package southbound contains code for updating P4 pipeline and chassis configuration on Stratum devices
package southbound

import (
	"context"
	"fmt"
	topoapi "github.com/onosproject/onos-api/go/onos/topo"
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

// StratumP4 is ab abstraction of a device with P4Runtime endpoint
type StratumP4 struct {
	ID         topoapi.ID
	p4Server   *topoapi.P4RuntimeServer
	conn       *grpc.ClientConn
	p4Client   p4api.P4RuntimeClient
	stream     p4api.P4Runtime_StreamChannelClient
	electionID *p4api.Uint128
	ctx        context.Context
	roleName   string
}

// NewStratumP4 creates a new stratum P4 device descriptor from the specified topo entity
func NewStratumP4(object *topoapi.Object, roleName string) (*StratumP4, error) {
	d := &StratumP4{
		ID:       object.ID,
		p4Server: &topoapi.P4RuntimeServer{},
		roleName: roleName,
	}
	if err := object.GetAspect(d.p4Server); err != nil {
		return nil, err
	}
	return d, nil
}

// Connect establishes
func (d *StratumP4) Connect() error {
	log.Infof("%s: connecting...", d.ID)
	opts := []grpc.DialOption{
		grpc.WithTransportCredentials(insecure.NewCredentials()), // TODO: Deal with secrets
	}

	var err error
	endpoint := d.p4Server.Endpoint
	d.conn, err = grpc.Dial(fmt.Sprintf("%s:%d", endpoint.Address, endpoint.Port), opts...)
	if err != nil {
		return err
	}

	d.p4Client = p4api.NewP4RuntimeClient(d.conn)
	d.ctx = context.Background()

	// Establish stream and issue mastership
	if d.stream, err = d.p4Client.StreamChannel(d.ctx); err != nil {
		return err
	}

	d.electionID = p4utils.TimeBasedElectionID()
	role := p4utils.NewStratumRole(d.roleName, 0, []byte{}, false, true)
	if err = d.stream.Send(p4utils.CreateMastershipArbitration(d.electionID, role)); err != nil {
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
	if mar.ElectionId == nil || mar.ElectionId.High != d.electionID.High || mar.ElectionId.Low != d.electionID.Low {
		return errors.NewInvalid("%s: did not win election", d.ID)
	}

	log.Infof("%s: connected", d.ID)
	return nil
}

// Disconnect terminates the P4 message stream
func (d *StratumP4) Disconnect() error {
	return nil
}

// ReconcilePipelineConfig makes sure that the device has the given P4 pipeline configuration applied
func (d *StratumP4) ReconcilePipelineConfig(info []byte, binary []byte, cookie uint64) (uint64, error) {
	log.Infof("%s: configuring pipeline...", d.ID)
	// ask for the pipeline config cookie
	gr, err := d.p4Client.GetForwardingPipelineConfig(d.ctx, &p4api.GetForwardingPipelineConfigRequest{
		DeviceId:     d.p4Server.DeviceID,
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
	_, err = d.p4Client.SetForwardingPipelineConfig(d.ctx, &p4api.SetForwardingPipelineConfigRequest{
		DeviceId:   d.p4Server.DeviceID,
		Role:       d.roleName,
		ElectionId: d.electionID,
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
