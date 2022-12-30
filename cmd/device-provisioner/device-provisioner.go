// SPDX-FileCopyrightText: 2022-present Intel Corporation
//
// SPDX-License-Identifier: Apache-2.0

// Package main is an entry point for launching the device provisioner
package main

import (
	"github.com/onosproject/device-provisioner/pkg/manager"
	"github.com/onosproject/onos-lib-go/pkg/cli"
	"github.com/onosproject/onos-lib-go/pkg/logging"
	"github.com/spf13/cobra"
)

var log = logging.GetLogger()

const (
	realmFlag          = "realm"
	topoAddressFlag    = "topo-address"
	defaultTopoAddress = "onos-topo:5150"
	artifactDirFlag    = "artifact-dir"
	defaultArtifactDir = "/etc/onos/device-configs"
)

// The main entry point
func main() {
	cmd := &cobra.Command{
		Use:  "device-provisioner",
		RunE: runRootCommand,
	}
	cmd.Flags().String(realmFlag, "all", "realm of devices over which the provisioner should operate")
	cmd.Flags().String(topoAddressFlag, defaultTopoAddress, "address:port or just :port of the onos-topo service")
	cmd.Flags().String(artifactDirFlag, defaultArtifactDir, "directory where artifact files are maintained")
	cli.AddServiceEndpointFlags(cmd, "provisioner gRPC")
	cli.Run(cmd)
}

func runRootCommand(cmd *cobra.Command, args []string) error {
	realm, _ := cmd.Flags().GetString(realmFlag)
	topoAddress, _ := cmd.Flags().GetString(topoAddressFlag)
	artifactDir, _ := cmd.Flags().GetString(artifactDirFlag)

	flags, err := cli.ExtractServiceEndpointFlags(cmd)
	if err != nil {
		return err
	}

	log.Infof("Starting device-provisioner")
	cfg := manager.Config{
		Realm:        realm,
		TopoAddress:  topoAddress,
		ArtifactDir:  artifactDir,
		ServiceFlags: flags,
	}

	return cli.RunDaemon(manager.NewManager(cfg))
}
