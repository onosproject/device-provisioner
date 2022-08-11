// SPDX-FileCopyrightText: 2022-present Intel Corporation
//
// SPDX-License-Identifier: Apache-2.0

package pluginregistry

import (
	p4rtapi "github.com/onosproject/onos-api/go/onos/p4rt/v1"
	"github.com/onosproject/onos-lib-go/pkg/errors"
	"github.com/onosproject/onos-lib-go/pkg/logging"
	p4configapi "github.com/p4lang/p4runtime/go/p4/config/v1"
	"plugin"
	"sync"
)

var log = logging.GetLogger()

// P4PluginRegistry is the object for the saving information about P4 program artifacts such as P4 info and P4 device config
type P4PluginRegistry interface {
	GetPlugins() map[p4rtapi.P4PluginID]P4Plugin
	GetPlugin(id p4rtapi.P4PluginID) (P4Plugin, error)
	RegisterPlugin(pluginName string) error
}

type pluginRegistry struct {
	plugins map[p4rtapi.P4PluginID]P4Plugin
	mu      sync.RWMutex
}

// GetPlugins get list of p4 plugins
func (p *pluginRegistry) GetPlugins() map[p4rtapi.P4PluginID]P4Plugin {
	p.mu.RLock()
	defer p.mu.RUnlock()
	plugins := make(map[p4rtapi.P4PluginID]P4Plugin, len(p.plugins))
	for id, p4Plugin := range p.plugins {
		plugins[id] = p4Plugin
	}
	return plugins
}

// GetPlugin gets a plugin based on a given ID
func (p *pluginRegistry) GetPlugin(id p4rtapi.P4PluginID) (P4Plugin, error) {
	p.mu.RLock()
	defer p.mu.RUnlock()
	p4Plugin, ok := p.plugins[id]
	if !ok {
		err := errors.NewNotFound("P4 plugin with ID '%s' not found", id)
		return nil, err
	}
	return p4Plugin, nil
}

// RegisterPlugin registers a  plugin based on a given name
func (p *pluginRegistry) RegisterPlugin(pluginPath string) error {
	log.Infow("Loading plugin", "plugin path", pluginPath)
	pluginModule, err := plugin.Open(pluginPath)
	if err != nil {
		log.Warnw("Unable to load module %s %s", "plugin path", pluginPath, "error", err)
		return err
	}
	symbolMP, err := pluginModule.Lookup("P4Plugin")
	if err != nil {
		log.Warnw("Unable to find P4 plugin ", "plugin path", pluginPath, "error", err)
		return err
	}
	p4Plugin, ok := symbolMP.(P4Plugin)
	if !ok {
		log.Warnw("Unable to use P4 Plugin", "plugin path", pluginPath)
		return errors.NewInvalid("symbol loaded from module %s is not a P4 plugin", pluginPath)
	}
	pkgInfo, err := p4Plugin.GetPkgInfo()
	if err != nil {
		log.Warnw("Cannot retrieve P4 Program PkgInfo", "plugin path", pluginPath)
		return err
	}
	pluginID := p4rtapi.NewP4PluginID(pkgInfo.Name, pkgInfo.Version, pkgInfo.Arch)
	log.Infow("Registering a P4 plugin", "plugin ID", pluginID)
	p.mu.Lock()
	defer p.mu.Unlock()
	p.plugins[pluginID] = p4Plugin
	return nil
}

// NewP4PluginRegistry create an instance of p4 plugin registry
func NewP4PluginRegistry() P4PluginRegistry {
	return &pluginRegistry{
		plugins: make(map[p4rtapi.P4PluginID]P4Plugin),
	}
}

// P4Plugin p4 plugin interface
type P4Plugin interface {
	GetPkgInfo() (*p4configapi.PkgInfo, error)
	GetP4DeviceConfig() ([]byte, error)
	GetP4Info() (info *p4configapi.P4Info, err error)
}
