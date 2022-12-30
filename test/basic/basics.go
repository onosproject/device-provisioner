// SPDX-FileCopyrightText: 2022-present Intel Corporation
//
// SPDX-License-Identifier: Apache-2.0

package basic

import (
	"context"
	fsimtopo "github.com/onosproject/fabric-sim/pkg/topo"
	fsimtest "github.com/onosproject/fabric-sim/test/client"
	fsimapi "github.com/onosproject/onos-api/go/onos/fabricsim"
	"github.com/onosproject/onos-api/go/onos/provisioner"
	topoapi "github.com/onosproject/onos-api/go/onos/topo"
	libtest "github.com/onosproject/onos-lib-go/pkg/test"
	p4api "github.com/p4lang/p4runtime/go/p4/v1"
	"github.com/stretchr/testify/assert"
	"io"
	"os"
	"testing"
	"time"
)

const (
	pipelineConfigName = "foobar-v0.1.0"
)

// TestBasics loads simulator with custom.yaml topology and validates proper startup
func (s *TestSuite) TestBasics(t *testing.T) {
	fsimConn, err := libtest.CreateConnection("fabric-sim:5150", true)
	assert.NoError(t, err)

	topoConn, err := libtest.CreateConnection("onos-topo:5150", false)
	assert.NoError(t, err)

	provConn, err := libtest.CreateConnection("device-provisioner:5150", false)
	assert.NoError(t, err)

	err = fsimtopo.LoadTopology(fsimConn, "./test/basic/topo.yaml")
	assert.NoError(t, err)

	topoClient := topoapi.NewTopoClient(topoConn)
	assert.NotNil(t, topoClient)
	provClient := provisioner.NewProvisionerServiceClient(provConn)

	ctx := context.Background()

	// Get pipeline configs; there should be none
	stream, err := provClient.List(ctx, &provisioner.ListConfigsRequest{})
	assert.NoError(t, err)
	assert.Len(t, slurp(stream), 0)

	p4infoBytes, err := os.ReadFile("./test/basic/p4info.txt")
	assert.NoError(t, err)

	// Add pipeline config
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
	stream, err = provClient.List(ctx, &provisioner.ListConfigsRequest{})
	assert.NoError(t, err)
	assert.Len(t, slurp(stream), 1)

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
