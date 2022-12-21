// SPDX-FileCopyrightText: 2022-present Intel Corporation
//
// SPDX-License-Identifier: Apache-2.0

// Package manager contains the device provisioner manager coordinating lifecycle of the NB API and SB controller
package manager

import (
	"github.com/atomix/go-sdk/pkg/client"
	"github.com/onosproject/device-provisioner/pkg/controller"
	nb "github.com/onosproject/device-provisioner/pkg/northbound"
	"github.com/onosproject/device-provisioner/pkg/store"
	"github.com/onosproject/onos-lib-go/pkg/certs"
	"github.com/onosproject/onos-lib-go/pkg/cli"
	"github.com/onosproject/onos-lib-go/pkg/logging"
	"github.com/onosproject/onos-lib-go/pkg/northbound"
)

var log = logging.GetLogger("manager")

// Config is a manager configuration
type Config struct {
	Realm        string
	TopoAddress  string
	ArtifactDir  string
	ServiceFlags *cli.ServiceEndpointFlags
}

// Manager single point of entry for the provisioner
type Manager struct {
	cli.Daemon
	Config      Config
	configStore store.ConfigStore
	controller  *controller.Controller
}

// NewManager initializes the application manager
func NewManager(cfg Config) *Manager {
	log.Infof("Creating manager")
	return &Manager{Config: cfg}
}

// Start initializes and starts the manager.
func (m *Manager) Start() error {
	log.Info("Starting Manager")

	var err error
	if m.configStore, err = store.NewAtomixStore(client.NewClient(), m.Config.ArtifactDir); err != nil {
		return err
	}

	// Initialize and start the configuration provisioning controller
	opts, err := certs.HandleCertPaths(m.Config.ServiceFlags.CAPath, m.Config.ServiceFlags.KeyPath, m.Config.ServiceFlags.CertPath, true)
	if err != nil {
		return err
	}

	m.controller = controller.NewController(m.Config.Realm, m.configStore, m.Config.TopoAddress, opts...)
	m.controller.Start()

	// Start NB server
	s := northbound.NewServer(cli.ServerConfigFromFlags(m.Config.ServiceFlags, northbound.SecurityConfig{}))
	s.AddService(logging.Service{})
	s.AddService(nb.NewService(m.controller, m.configStore))
	return s.StartInBackground()
}

// Stop stops the manager
func (m *Manager) Stop() {
	log.Info("Stopping Manager")
	m.controller.Stop()
}
