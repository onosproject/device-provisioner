// SPDX-FileCopyrightText: 2022-present Intel Corporation
//
// SPDX-License-Identifier: Apache-2.0

package manager

import (
	"github.com/atomix/go-client/pkg/client"
	"github.com/onosproject/device-provisioner/pkg/controller/pipeline"
	"github.com/onosproject/device-provisioner/pkg/pluginregistry"
	"github.com/onosproject/device-provisioner/pkg/store/pipelineconfig"
	"github.com/onosproject/device-provisioner/pkg/store/topo"
	"github.com/onosproject/onos-lib-go/pkg/certs"
	"github.com/onosproject/onos-lib-go/pkg/logging"
	"github.com/onosproject/onos-lib-go/pkg/northbound"
	"github.com/onosproject/onos-p4-sdk/pkg/p4rt/admin"
)

var log = logging.GetLogger()

// Config is app manager
type Config struct {
	CAPath      string
	KeyPath     string
	CertPath    string
	TopoAddress string
	GRPCPort    int
	P4Plugins   []string
}

// Manager single point of entry for the device-provisioner application
type Manager struct {
	Config           Config
	p4PluginRegistry pluginregistry.P4PluginRegistry
}

// NewManager initializes the application manager
func NewManager(cfg Config) *Manager {
	log.Info("Creating application manager")
	p4PluginRegistry := pluginregistry.NewP4PluginRegistry()
	for _, smp := range cfg.P4Plugins {
		if err := p4PluginRegistry.RegisterPlugin(smp); err != nil {
			log.Fatal(err)
		}
	}
	mgr := Manager{
		Config:           cfg,
		p4PluginRegistry: p4PluginRegistry,
	}
	return &mgr
}

// Run runs application manager
func (m *Manager) Run() {
	log.Info("Starting application Manager")

	if err := m.start(); err != nil {
		log.Fatal("Unable to run Manager", "error", err)
	}
}

func (m *Manager) start() error {
	opts, err := certs.HandleCertPaths(m.Config.CAPath, m.Config.KeyPath, m.Config.CertPath, true)
	if err != nil {
		return err
	}

	adminController := admin.StartController(admin.Config{
		CAPath:      m.Config.CAPath,
		CertPath:    m.Config.CertPath,
		KeyPath:     m.Config.KeyPath,
		TopoAddress: m.Config.TopoAddress,
	})

	// Create new topo store
	topoStore, err := topo.NewStore(m.Config.TopoAddress, opts...)
	if err != nil {
		return err
	}

	pipelineConfigStore, err := pipelineconfig.NewAtomixStore(client.NewClient())
	if err != nil {
		return err
	}

	err = m.startPipelineController(topoStore, pipelineConfigStore, adminController, m.p4PluginRegistry)
	if err != nil {
		return err
	}
	// Starts NB server
	err = m.startNorthboundServer()
	if err != nil {
		return err
	}

	return nil
}

// startSouthboundServer starts the northbound gRPC server
func (m *Manager) startNorthboundServer() error {
	log.Info("Starting NB server")
	s := northbound.NewServer(northbound.NewServerCfg(
		m.Config.CAPath,
		m.Config.KeyPath,
		m.Config.CertPath,
		int16(m.Config.GRPCPort),
		true,
		northbound.SecurityConfig{}))
	s.AddService(logging.Service{})

	doneCh := make(chan error)
	go func() {
		err := s.Serve(func(started string) {
			log.Info("Started NBI on ", started)
			close(doneCh)
		})
		if err != nil {
			doneCh <- err
		}
	}()
	return <-doneCh
}

func (m *Manager) startPipelineController(topoStore topo.Store, pipelineConfigStore pipelineconfig.Store, adminController *admin.Controller, p4pluginRegistry pluginregistry.P4PluginRegistry) error {
	pipelineController := pipeline.NewController(topoStore, pipelineConfigStore, adminController, p4pluginRegistry)
	return pipelineController.Start()
}
