// SPDX-FileCopyrightText: 2020-present Open Networking Foundation <info@opennetworking.org>
//
// SPDX-License-Identifier: Apache-2.0

// Package topo store client
package topo

import (
	"context"
	"io"

	"github.com/onosproject/onos-lib-go/pkg/grpc/retry"
	"google.golang.org/grpc/codes"

	"github.com/onosproject/onos-lib-go/pkg/errors"

	topoapi "github.com/onosproject/onos-api/go/onos/topo"
	"github.com/onosproject/onos-lib-go/pkg/logging"
	"google.golang.org/grpc"
)

var log = logging.GetLogger()

// Store is a topology store client interface
type Store interface {
	// Create creates a topology object
	Create(ctx context.Context, object *topoapi.Object) error

	// Update updates an existing topology object
	Update(ctx context.Context, object *topoapi.Object) error

	// Get gets a topology object
	Get(ctx context.Context, id topoapi.ID) (*topoapi.Object, error)

	// Query streams objects to the given channel
	Query(ctx context.Context, ch chan<- *topoapi.Object, filters *topoapi.Filters) error

	// Delete deletes a topology object using the given ID
	Delete(ctx context.Context, object *topoapi.Object) error

	// Watch watches topology events
	Watch(ctx context.Context, ch chan<- topoapi.Event, filters *topoapi.Filters) error
}

// NewStore creates a new topology store
func NewStore(topoEndpoint string, opts ...grpc.DialOption) (Store, error) {
	if len(opts) == 0 {
		return nil, errors.New(errors.Invalid, "no opts given when creating topology store")
	}
	opts = append(opts,
		grpc.WithStreamInterceptor(retry.RetryingStreamClientInterceptor(retry.WithRetryOn(codes.Unavailable, codes.Unknown))),
		grpc.WithUnaryInterceptor(retry.RetryingUnaryClientInterceptor(retry.WithRetryOn(codes.Unavailable, codes.Unknown))))
	conn, err := grpc.DialContext(context.Background(), topoEndpoint, opts...)
	if err != nil {
		log.Warn(err)
		return nil, err
	}
	client := topoapi.CreateTopoClient(conn)
	return &topoClient{
		client: client,
	}, nil
}

type topoClient struct {
	client topoapi.TopoClient
}

// Create creates topology object in topo store
func (s *topoClient) Create(ctx context.Context, object *topoapi.Object) error {
	log.Debugf("Creating topology object: %v", object)
	_, err := s.client.Create(ctx, &topoapi.CreateRequest{
		Object: object,
	})
	if err != nil {
		log.Warn(err)
		return errors.FromGRPC(err)
	}
	return nil
}

// Update updates the given topology object in topo store
func (s *topoClient) Update(ctx context.Context, object *topoapi.Object) error {
	log.Debugf("Updating topology object: %v", object)
	response, err := s.client.Update(ctx, &topoapi.UpdateRequest{
		Object: object,
	})
	if err != nil {
		return errors.FromGRPC(err)
	}
	object = response.Object
	log.Debug("Updated topology object is:", object)
	return nil
}

// Get gets topology object based on a given ID
func (s *topoClient) Get(ctx context.Context, id topoapi.ID) (*topoapi.Object, error) {
	log.Debugf("Getting topology object with ID: %v", id)
	getResponse, err := s.client.Get(ctx, &topoapi.GetRequest{
		ID: id,
	})
	if err != nil {
		return nil, errors.FromGRPC(err)
	}
	return getResponse.Object, nil
}

// Query query topology objects based on a give set of filters
func (s *topoClient) Query(ctx context.Context, ch chan<- *topoapi.Object, filters *topoapi.Filters) error {
	log.Debugf("Query topology objects")
	stream, err := s.client.Query(ctx, &topoapi.QueryRequest{
		Filters: filters,
	})
	if err != nil {
		return errors.FromGRPC(err)
	}
	go func() {
		defer close(ch)
		for {
			resp, err := stream.Recv()
			if err == io.EOF {
				break
			}
			if err != nil {
				log.Warn(err)
				break
			}
			ch <- resp.Object
		}
	}()
	return nil
}

// Delete deletes topology object using the given ID
func (s *topoClient) Delete(ctx context.Context, object *topoapi.Object) error {
	log.Debugf("Deleting topology object with ID: %v", object.ID)
	_, err := s.client.Delete(ctx, &topoapi.DeleteRequest{
		ID:       object.GetID(),
		Revision: object.GetRevision(),
	})
	if err != nil {
		return errors.FromGRPC(err)
	}
	return nil
}

// Watch watches topology events
func (s *topoClient) Watch(ctx context.Context, ch chan<- topoapi.Event, filters *topoapi.Filters) error {
	stream, err := s.client.Watch(ctx, &topoapi.WatchRequest{
		Noreplay: false,
		Filters:  filters,
	})
	if err != nil {
		return errors.FromGRPC(err)
	}
	go func() {
		defer close(ch)
		for {
			resp, err := stream.Recv()
			if err == io.EOF {
				break
			}
			if err != nil {
				log.Warn(err)
				break
			}
			ch <- resp.Event
		}
	}()
	return nil
}

var _ Store = &topoClient{}
