// SPDX-FileCopyrightText: 2022-present Intel Corporation
//
// SPDX-License-Identifier: Apache-2.0

// Package manager contains the device provisioner manager coordinating lifecycle of the NB API and SB controller
package manager

import (
	"github.com/atomix/go-sdk/pkg/client"
	"github.com/onosproject/device-provisioner/pkg/controller/chassis"
	"github.com/onosproject/device-provisioner/pkg/controller/pipeline"
	"github.com/onosproject/device-provisioner/pkg/controller/target"
	nb "github.com/onosproject/device-provisioner/pkg/northbound"
	"github.com/onosproject/device-provisioner/pkg/store/pipelineconfig"
	"github.com/onosproject/device-provisioner/pkg/store/topo"
	"github.com/onosproject/onos-lib-go/pkg/certs"
	"github.com/onosproject/onos-lib-go/pkg/cli"
	"github.com/onosproject/onos-lib-go/pkg/logging"
	"github.com/onosproject/onos-lib-go/pkg/northbound"
	"github.com/onosproject/onos-net-lib/pkg/p4rtclient"
	"github.com/onosproject/onos-net-lib/pkg/realm"
)

var log = logging.GetLogger()

// Config is a manager configuration
type Config struct {
	RealmOptions *realm.Options
	TopoAddress  string
	ArtifactDir  string
	ServiceFlags *cli.ServiceEndpointFlags
}

// Manager single point of entry for the provisioner
type Manager struct {
	cli.Daemon
	Config Config
}

// NewManager initializes the application manager
func NewManager(cfg Config) *Manager {
	log.Infof("Creating manager")
	return &Manager{Config: cfg}
}

// Start initializes and starts the manager.
func (m *Manager) Start() error {
	log.Info("Starting Manager")

	// Initialize and start the configuration provisioning controller
	opts, err := certs.HandleCertPaths(m.Config.ServiceFlags.CAPath, m.Config.ServiceFlags.KeyPath, m.Config.ServiceFlags.CertPath, true)
	if err != nil {
		return err
	}
	topoStore, err := topo.NewStore(m.Config.TopoAddress, opts...)
	if err != nil {
		return err
	}

	configStore, err := pipelineconfig.NewAtomixStore(client.NewClient(), m.Config.ArtifactDir)
	if err != nil {
		return err
	}
	conns := p4rtclient.NewConnManager()

	targetController := target.NewController(topoStore, conns, m.Config.RealmOptions)
	err = targetController.Start()
	if err != nil {
		return err
	}

	pipelineController := pipeline.NewController(topoStore, conns, configStore, m.Config.RealmOptions)
	err = pipelineController.Start()
	if err != nil {
		return err
	}

	chassisController := chassis.NewController(topoStore, configStore, m.Config.RealmOptions)
	err = chassisController.Start()
	if err != nil {
		return err
	}

	// Start NB server
	s := northbound.NewServer(cli.ServerConfigFromFlags(m.Config.ServiceFlags, northbound.SecurityConfig{}))
	s.AddService(logging.Service{})
	s.AddService(nb.NewService(configStore))
	return s.StartInBackground()
}

// Stop stops the manager
func (m *Manager) Stop() {
	log.Info("Stopping Manager")
}
