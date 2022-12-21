// SPDX-FileCopyrightText: 2022-present Intel Corporation
//
// SPDX-License-Identifier: Apache-2.0

// Package northbound implements the northbound API of the device provisioner
package northbound

import (
	"context"
	"github.com/onosproject/device-provisioner/pkg/controller"
	"github.com/onosproject/device-provisioner/pkg/store"
	api "github.com/onosproject/onos-api/go/onos/provisioner"
	"github.com/onosproject/onos-lib-go/pkg/errors"
	"github.com/onosproject/onos-lib-go/pkg/logging"
	"github.com/onosproject/onos-lib-go/pkg/northbound"
	"google.golang.org/grpc"
)

var log = logging.GetLogger("northbound")

// Service implements the device provisioner NB gRPC
type Service struct {
	northbound.Service
	controller  *controller.Controller
	configStore store.ConfigStore
}

// NewService allocates a Service struct with the given parameters
func NewService(controller *controller.Controller, configStore store.ConfigStore) Service {
	return Service{
		controller:  controller,
		configStore: configStore,
	}
}

// Register registers the server with grpc
func (s Service) Register(r *grpc.Server) {
	server := &Server{
		controller:  s.controller,
		configStore: s.configStore,
	}
	api.RegisterProvisionerServiceServer(r, server)
	log.Debug("Device Provisioner API services registered")
}

// Server implements the grpc device provisioner service
type Server struct {
	controller  *controller.Controller
	configStore store.ConfigStore
}

// Add registers new pipeline configuration
func (s *Server) Add(ctx context.Context, request *api.AddConfigRequest) (*api.AddConfigResponse, error) {
	log.Infof("Received add request: %+v", request)
	if err := s.configStore.Add(ctx, request.Config.Record, request.Config.Artifacts); err != nil {
		log.Warnf("Request %+v failed adding configuration: %v", request, err)
		return nil, errors.Status(err).Err()
	}
	return &api.AddConfigResponse{}, nil
}

// Delete removes a pipeline configuration
func (s *Server) Delete(ctx context.Context, request *api.DeleteConfigRequest) (*api.DeleteConfigResponse, error) {
	log.Infof("Received delete request: %+v", request)
	if err := s.configStore.Delete(ctx, request.Record); err != nil {
		log.Warnf("Request %+v failed deleting configuration: %v", request, err)
		return nil, errors.Status(err).Err()
	}
	return &api.DeleteConfigResponse{}, nil
}

// Get returns pipeline configuration based on a given ID
func (s *Server) Get(ctx context.Context, request *api.GetConfigRequest) (*api.GetConfigResponse, error) {
	log.Infof("Received get request: %+v", request)
	record, err := s.configStore.Get(ctx, request.ConfigID)
	if err != nil {
		log.Warnf("Request %+v failed retrieving configuration: %v", request, err)
		return nil, errors.Status(err).Err()
	}
	artifacts, err := s.configStore.GetArtifacts(ctx, record)
	if err != nil {
		log.Warnf("Request %+v failed retrieving artifacts: %v", request, err)
		return nil, errors.Status(err).Err()
	}
	return &api.GetConfigResponse{Config: &api.Config{Record: record, Artifacts: artifacts}}, nil
}

// List returns all registered pipelines
func (s *Server) List(request *api.ListConfigsRequest, server api.ProvisionerService_ListServer) error {
	log.Infof("Received list request: %+v", request)
	ch := make(chan *api.ConfigRecord, 512)
	go func() {
		if err := s.configStore.List(server.Context(), request.Kind, ch); err != nil {
			log.Warnf("Request %+v failed: %v", request, err)
		}
	}()

	for record := range ch {
		artifacts, err := s.configStore.GetArtifacts(server.Context(), record)
		if err != nil {
			log.Warnf("Request %+v failed retrieving artifacts: %v", request, err)
			return err
		}
		res := &api.ListConfigsResponse{Config: &api.Config{Record: record, Artifacts: artifacts}}
		log.Debugf("Sending list response %+v", res)
		if err = server.Send(res); err != nil {
			log.Warnf("Unable to send response %+v: %v", res, err)
			return err
		}
	}
	return nil
}
