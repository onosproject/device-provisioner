// SPDX-FileCopyrightText: 2022-present Intel Corporation
//
// SPDX-License-Identifier: Apache-2.0

package basic

import (
	"context"
	fsimtest "github.com/onosproject/fabric-sim/test/client"
	fsimapi "github.com/onosproject/onos-api/go/onos/fabricsim"
	"github.com/onosproject/onos-api/go/onos/provisioner"
	topoapi "github.com/onosproject/onos-api/go/onos/topo"
	libtest "github.com/onosproject/onos-lib-go/pkg/test"
	utils "github.com/onosproject/onos-net-lib/pkg/gnmiutils"
	"github.com/openconfig/gnmi/proto/gnmi"
	p4api "github.com/p4lang/p4runtime/go/p4/v1"
	"github.com/stretchr/testify/assert"
	"io"
	"os"
	"testing"
	"time"
)

const (
	pipelineConfigName = "foobar-v0.1.0"
	chassisConfigName  = "chassis-v0.2.0"
)

// TestPipelineBasics validate P4 pipeline config reconciliation
func (s *TestSuite) TestPipelineBasics(t *testing.T) {
	topoClient, provClient := getConnections(t)
	p4infoBytes, err := os.ReadFile("./test/basic/p4info.txt")
	assert.NoError(t, err)

	// Add pipeline config
	ctx := context.Background()
	_, err = provClient.Add(ctx, &provisioner.AddConfigRequest{
		Config: &provisioner.Config{
			Record: &provisioner.ConfigRecord{
				ConfigID: pipelineConfigName,
				Kind:     provisioner.PipelineConfigKind,
			},
			Artifacts: map[string][]byte{
				provisioner.P4InfoType:   p4infoBytes,
				provisioner.P4BinaryType: p4infoBytes,
			},
		},
	})
	assert.NoError(t, err)

	// Get pipeline configs; there should be one
	stream, err := provClient.List(ctx, &provisioner.ListConfigsRequest{})
	assert.NoError(t, err)
	assert.True(t, len(slurp(stream)) >= 1)

	// Create topo object for our topology with device config aspect
	object := &topoapi.Object{
		ID:   "spine1",
		Type: topoapi.Object_ENTITY,
		Obj: &topoapi.Object_Entity{Entity: &topoapi.Entity{
			KindID: "switch",
		}},
		Labels: map[string]string{"pod": "all"},
	}
	err = object.SetAspect(&topoapi.P4RuntimeServer{
		Endpoint: &topoapi.Endpoint{
			Address: "fabric-sim",
			Port:    20000,
		},
	})
	assert.NoError(t, err)

	err = object.SetAspect(&provisioner.DeviceConfig{
		PipelineConfigID: pipelineConfigName,
	})
	assert.NoError(t, err)

	_, err = topoClient.Create(ctx, &topoapi.CreateRequest{Object: object})
	assert.NoError(t, err)

	time.Sleep(5 * time.Second)

	// Validate that the pipeline config got set on the topology object
	gresp, err := topoClient.Get(ctx, &topoapi.GetRequest{ID: "spine1"})
	assert.NoError(t, err)

	pcState := &provisioner.PipelineConfigState{}
	err = gresp.Object.GetAspect(pcState)
	assert.NoError(t, err)
	assert.True(t, pcState.Cookie > 0)

	// Validate that the pipeline config got set on the fabric-sim devices
	dconn, err := fsimtest.CreateDeviceConnection(&fsimapi.Device{
		ID:          "spine1",
		Type:        fsimapi.DeviceType_SWITCH,
		ControlPort: 20000,
	})
	assert.NoError(t, err)

	p4client := p4api.NewP4RuntimeClient(dconn)
	presp, err := p4client.GetForwardingPipelineConfig(ctx, &p4api.GetForwardingPipelineConfigRequest{
		DeviceId:     0,
		ResponseType: p4api.GetForwardingPipelineConfigRequest_COOKIE_ONLY,
	})
	assert.NoError(t, err)
	assert.Equal(t, presp.Config.Cookie.Cookie, pcState.Cookie)
}

// TestChassisBasics validate gNMI chassis config reconciliation
func (s *TestSuite) TestChassisBasics(t *testing.T) {
	topoClient, provClient := getConnections(t)
	chassisBytes, err := os.ReadFile("./test/basic/stratum.gnmi")
	assert.NoError(t, err)

	// Add chassis config
	ctx := context.Background()
	_, err = provClient.Add(ctx, &provisioner.AddConfigRequest{
		Config: &provisioner.Config{
			Record: &provisioner.ConfigRecord{
				ConfigID: chassisConfigName,
				Kind:     provisioner.ChassisConfigKind,
			},
			Artifacts: map[string][]byte{
				provisioner.ChassisType: chassisBytes,
			},
		},
	})
	assert.NoError(t, err)

	// Get pipeline configs; there should be one
	stream, err := provClient.List(ctx, &provisioner.ListConfigsRequest{})
	assert.NoError(t, err)
	assert.True(t, len(slurp(stream)) >= 1)

	// Create topo object for our topology with device config aspect
	object := &topoapi.Object{
		ID:   "spine2",
		Type: topoapi.Object_ENTITY,
		Obj: &topoapi.Object_Entity{Entity: &topoapi.Entity{
			KindID: "switch",
		}},
		Labels: map[string]string{"pod": "all"},
	}
	err = object.SetAspect(&topoapi.GNMIServer{
		Endpoint: &topoapi.Endpoint{
			Address: "fabric-sim",
			Port:    20001,
		},
	})
	assert.NoError(t, err)

	err = object.SetAspect(&provisioner.DeviceConfig{
		ChassisConfigID: chassisConfigName,
	})
	assert.NoError(t, err)

	_, err = topoClient.Create(ctx, &topoapi.CreateRequest{Object: object})
	assert.NoError(t, err)

	time.Sleep(5 * time.Second)

	// Validate that the chassis config got set on the topology object
	gresp, err := topoClient.Get(ctx, &topoapi.GetRequest{ID: "spine2"})
	assert.NoError(t, err)

	ccState := &provisioner.ChassisConfigState{}
	err = gresp.Object.GetAspect(ccState)
	assert.NoError(t, err)

	// Validate that the chassis config got set on the fabric-sim devices
	dconn, err := fsimtest.CreateDeviceConnection(&fsimapi.Device{
		ID:          "spine2",
		Type:        fsimapi.DeviceType_SWITCH,
		ControlPort: 20001,
	})
	assert.NoError(t, err)

	gnmiClient := gnmi.NewGNMIClient(dconn)
	resp, err := gnmiClient.Get(ctx, &gnmi.GetRequest{
		Path: []*gnmi.Path{utils.ToPath("")},
	})
	assert.NoError(t, err)
	assert.Equal(t, chassisBytes, resp.Notification[0].Update[0].Val.GetBytesVal())
}

func slurp(stream provisioner.ProvisionerService_ListClient) []*provisioner.ConfigRecord {
	records := make([]*provisioner.ConfigRecord, 0)
	for {
		resp, err := stream.Recv()
		switch err {
		case nil:
			records = append(records, resp.Config.Record)
		case io.EOF:
			return records
		}
	}
}

func getConnections(t *testing.T) (topoapi.TopoClient, provisioner.ProvisionerServiceClient) {
	topoConn, err := libtest.CreateConnection("onos-topo:5150", false)
	assert.NoError(t, err)

	provConn, err := libtest.CreateConnection("device-provisioner:5150", false)
	assert.NoError(t, err)

	topoClient := topoapi.NewTopoClient(topoConn)
	assert.NotNil(t, topoClient)
	provClient := provisioner.NewProvisionerServiceClient(provConn)
	return topoClient, provClient
}
