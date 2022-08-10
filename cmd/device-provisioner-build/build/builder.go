// SPDX-FileCopyrightText: 2022-present Intel Corporation
//
// SPDX-License-Identifier: Apache-2.0

package build

import (
	"encoding/json"
	"fmt"
	"github.com/onosproject/onos-lib-go/pkg/errors"
	"github.com/rogpeppe/go-internal/modfile"
	"github.com/spf13/cobra"
	"io/ioutil"
	"os"
	"os/exec"
)

func Build(cmd *cobra.Command, config Config) error {
	provisionerModBytes, err := ioutil.ReadFile("go.mod")
	if err != nil {
		return err
	}

	provisionerModFile, err := modfile.Parse("go.mod", provisionerModBytes, nil)
	if err != nil {
		return err
	}

	if err := os.MkdirAll("/build/_output", os.ModePerm); err != nil {
		return err
	}
	if err := os.MkdirAll("/build/_output/plugins", os.ModePerm); err != nil {
		return err
	}

	fmt.Fprintln(cmd.OutOrStdout(), "Building github.com/onosproject/device-provisioner")
	_, err = run(".",
		"go", "build",
		"-mod=readonly",
		"-trimpath",
		"-gcflags='all=-N -l'",
		"-o", "/build/_output/device-provisioner",
		"./cmd/device-provisioner")
	if err != nil {
		return err
	}

	builder := newPluginBuilder(cmd, provisionerModFile)
	for _, plugin := range config.Plugins {
		if err := builder.Build(plugin); err != nil {
			return err
		}
	}
	return nil
}

func newPluginBuilder(cmd *cobra.Command, provisionerModFile *modfile.File) *Builder {
	return &Builder{
		cmd:                cmd,
		provisionerModFile: provisionerModFile,
	}
}

type Builder struct {
	cmd                *cobra.Command
	provisionerModFile *modfile.File
}

func (b *Builder) Build(plugin PluginConfig) error {
	pluginModFile, pluginModDir, err := b.downloadPluginMod(plugin)
	if err != nil {
		return err
	}
	if err := b.validatePluginModFile(plugin, pluginModFile); err != nil {
		return err
	}
	if err := b.buildPlugin(plugin, pluginModDir); err != nil {
		return err
	}
	return nil
}

func (b *Builder) downloadPluginMod(plugin PluginConfig) (*modfile.File, string, error) {
	if plugin.Path == "" {
		err := errors.NewInvalid("no plugin module path configured")
		fmt.Fprintln(b.cmd.OutOrStderr(), "Plugin configuration is invalid", err)
		return nil, "", err
	}

	fmt.Fprintln(b.cmd.OutOrStdout(), "Downloading module", plugin.Path)
	output, err := run(".", "go", "mod", "download", "-json", plugin.Path)
	if err != nil {
		fmt.Fprintln(b.cmd.OutOrStderr(), "Failed to download module", plugin.Path, err)
		return nil, "", err
	}
	println(output)

	var modInfo goModInfo
	if err := json.Unmarshal([]byte(output), &modInfo); err != nil {
		fmt.Fprintln(b.cmd.OutOrStderr(), "Failed to download module", plugin.Path, err)
		return nil, "", err
	}

	fmt.Fprintln(b.cmd.OutOrStdout(), "Parsing", modInfo.GoMod)
	goModBytes, err := ioutil.ReadFile(modInfo.GoMod)
	if err != nil {
		fmt.Fprintln(b.cmd.OutOrStderr(), "Failed to download module", plugin.Path, err)
		return nil, "", err
	}

	goModFile, err := modfile.Parse(modInfo.GoMod, goModBytes, nil)
	if err != nil {
		fmt.Fprintln(b.cmd.OutOrStderr(), "Failed to download module", plugin.Path, err)
		return nil, "", err
	}
	return goModFile, modInfo.Dir, nil
}

func (b *Builder) validatePluginModFile(plugin PluginConfig, pluginModFile *modfile.File) error {
	fmt.Fprintln(b.cmd.OutOrStdout(), "Validating dependencies for module", plugin.Path)
	provisionerModRequires := make(map[string]string)
	for _, require := range b.provisionerModFile.Require {
		provisionerModRequires[require.Mod.Path] = require.Mod.Version
	}

	for _, require := range pluginModFile.Require {
		if provisionerVersion, ok := provisionerModRequires[require.Mod.Path]; ok {
			if require.Mod.Version != provisionerVersion {
				fmt.Fprintln(b.cmd.OutOrStderr(), "Incompatible dependency", require.Mod.Path, require.Mod.Version)
				return errors.NewInvalid("plugin module %s has incompatible dependency %s %s != %s",
					plugin.Path, require.Mod.Path, require.Mod.Version, provisionerVersion)
			}
		}
	}
	return nil
}

func (b *Builder) buildPlugin(plugin PluginConfig, dir string) error {
	fmt.Fprintln(b.cmd.OutOrStdout(), "Building plugin", plugin.Name)
	_, err := run(dir,
		"go", "build",
		"-mod=readonly",
		"-trimpath",
		"-buildmode=plugin",
		"-gcflags='all=-N -l'",
		"-o", fmt.Sprintf("/build/_output/plugins/%s@%s.so", plugin.Name, plugin.Version),
		"./")
	if err != nil {
		return err
	}
	return nil
}

func run(dir string, name string, args ...string) (string, error) {
	cmd := exec.Command(name, args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(),
		"GO111MODULE=on",
		"CGO_ENABLED=1",
		"GOOS=linux",
		"GOARCH=amd64",
		"CC=gcc",
		"CXX=g++")
	cmd.Stderr = os.Stderr
	out, err := cmd.Output()
	if err != nil {
		return "", err
	}
	return string(out), nil
}

type goModInfo struct {
	Path     string
	Version  string
	Error    string
	Info     string
	GoMod    string
	Zip      string
	Dir      string
	Sum      string
	GoModSum string
}
