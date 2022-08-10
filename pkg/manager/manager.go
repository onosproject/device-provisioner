// SPDX-FileCopyrightText: 2022-present Intel Corporation
//
// SPDX-License-Identifier: Apache-2.0

package manager

import (
	"github.com/onosproject/onos-lib-go/pkg/logging"
)

var log = logging.GetLogger()

// Config is app manager
type Config struct {
	CAPath      string
	KeyPath     string
	CertPath    string
	TopoAddress string
	GRPCPort    int
}

// Manager single point of entry for the device-provisioner application
type Manager struct {
	Config Config
}

// NewManager initializes the application manager
func NewManager(cfg Config) *Manager {
	log.Info("Creating manager")
	mgr := Manager{
		Config: cfg,
	}
	return &mgr
}

// Run runs application manager
func (m *Manager) Run() {
	log.Info("Starting Manager")
	if err := m.start(); err != nil {
		log.Fatalw("Unable to run Manager", "error", err)
	}
}

func (m *Manager) start() error {
	return nil
}
