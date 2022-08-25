// SPDX-FileCopyrightText: 2022-present Intel Corporation
//
// SPDX-License-Identifier: Apache-2.0

package northbound

import (
	"context"
	pipelineConfigStore "github.com/onosproject/device-provisioner/pkg/store/pipelineconfig"
	"github.com/onosproject/onos-api/go/onos/device-provisioner/admin"
	p4rtapi "github.com/onosproject/onos-api/go/onos/p4rt/v1"
	"github.com/onosproject/onos-lib-go/pkg/errors"
	"github.com/onosproject/onos-lib-go/pkg/logging"

	"github.com/onosproject/onos-lib-go/pkg/northbound"
	"google.golang.org/grpc"
)

var log = logging.GetLogger()

// Service is a Service implementation for administration.
type Service struct {
	northbound.Service
	pipelineConfigStore pipelineConfigStore.Store
}

// NewService allocates a Service struct with the given parameters
func NewService(pipelineConfigStore pipelineConfigStore.Store) Service {
	return Service{
		pipelineConfigStore: pipelineConfigStore,
	}
}

// Register registers the Service with the gRPC server.
func (s Service) Register(r *grpc.Server) {
	server := Server{
		pipelineConfigStore: s.pipelineConfigStore,
	}
	admin.RegisterPipelineConfigServiceServer(r, server)

}

// Server implements the gRPC service for administrative facilities.
type Server struct {
	pipelineConfigStore pipelineConfigStore.Store
}

func (s Server) GetPipeline(ctx context.Context, request *admin.GetPipelineRequest) (*admin.GetPipelineResponse, error) {
	log.Infow("Received GetPipeline request", "request", request)
	pipelineConfig, err := s.pipelineConfigStore.Get(ctx, request.PipelineConfigID)
	if err != nil {
		log.Warnw("Get pipeline config failed", "request", request, "error", err)
		return nil, errors.Status(err).Err()
	}
	return &admin.GetPipelineResponse{Pipelineconfig: pipelineConfig}, nil
}

func (s Server) ListPipelines(request *admin.ListPipelinesRequest, server admin.PipelineConfigService_ListPipelinesServer) error {
	log.Infow("Received ListConfigurations request", "request", request)
	pipelines, err := s.pipelineConfigStore.List(server.Context())
	if err != nil {
		log.Warnf("Listing pipeline configs failed", "request", request, "error", err)
		return errors.Status(err).Err()
	}
	for _, pipelineConfig := range pipelines {
		err := server.Send(&admin.ListPipelinesResponse{Pipelineconfig: pipelineConfig})
		if err != nil {
			log.Warnw("Listing pipeline configs failed", "request", request, "error", err)
			return errors.Status(err).Err()
		}
	}
	return nil
}

func (s Server) WatchPipelines(request *admin.WatchPipelinesRequest, server admin.PipelineConfigService_WatchPipelinesServer) error {
	log.Infow("Received Watch Pipelines  request", "request", request)
	var watchOpts []pipelineConfigStore.WatchOption
	if !request.Noreplay {
		watchOpts = append(watchOpts, pipelineConfigStore.WithReplay())
	}

	if len(request.PipelineConfigID) > 0 {
		watchOpts = append(watchOpts, pipelineConfigStore.WithPipelineConfigID(request.PipelineConfigID))
	}

	ch := make(chan *p4rtapi.PipelineConfig)
	if err := s.pipelineConfigStore.Watch(server.Context(), ch, watchOpts...); err != nil {
		log.Warnw("Watch Pipelines request failed", "request", request, "error", err)
		return errors.Status(err).Err()
	}

	if err := s.streamPipelines(server, ch); err != nil {
		return errors.Status(err).Err()
	}
	return nil
}
func (s Server) streamPipelines(server admin.PipelineConfigService_WatchPipelinesServer, ch chan *p4rtapi.PipelineConfig) error {
	for event := range ch {
		res := &admin.WatchPipelinesResponse{
			PipelineConfig: *event,
		}

		log.Debugw("Sending Watch Pipelines Response", "response", res)
		if err := server.Send(res); err != nil {
			log.Warnw("WatchConfigurationsResponse send failed", "response", res, "error", err)
			return err
		}
	}
	return nil
}
