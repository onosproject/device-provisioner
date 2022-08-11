// SPDX-FileCopyrightText: 2022-present Intel Corporation
//
// SPDX-License-Identifier: Apache-2.0

package build

// Config plugin builder config
type Config struct {
	Plugins []PluginConfig `yaml:"plugins"`
}

// PluginConfig plugin configuration
type PluginConfig struct {
	Name    string `yaml:"name"`
	Version string `yaml:"version"`
	Path    string `yaml:"path"`
}
