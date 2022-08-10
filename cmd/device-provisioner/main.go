// SPDX-FileCopyrightText: 2022-present Intel Corporation
//
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"github.com/onosproject/onos-lib-go/pkg/logging"
	"github.com/spf13/cobra"
	"os"
	"os/signal"
	"syscall"
)

var log = logging.GetLogger()

// The main entry point
func main() {
	if err := getRootCommand().Execute(); err != nil {
		println(err)
		os.Exit(1)
	}
}

func getRootCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "device-provisioner",
		Short: "device-provisioner",
		RunE:  runRootCommand,
	}
	cmd.Flags().String("caPath", "", "path to CA certificate")
	cmd.Flags().String("keyPath", "", "path to client private key")
	cmd.Flags().String("certPath", "", "path to client certificate")
	cmd.Flags().String("topoEndpoint", "onos-topo:5150", "topology service endpoint")
	cmd.Flags().StringSlice("p4Plugin", []string{}, "p4 plugin")
	return cmd
}

func runRootCommand(cmd *cobra.Command, args []string) error {
	caPath, _ := cmd.Flags().GetString("caPath")
	keyPath, _ := cmd.Flags().GetString("keyPath")
	certPath, _ := cmd.Flags().GetString("certPath")
	topoEndpoint, _ := cmd.Flags().GetString("topoEndpoint")

	log.Infow("Starting Device Provisioner application",
		"CaPath", caPath,
		"keyPath", keyPath,
		"certPath", certPath,
		"topoEndpoint", topoEndpoint)
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)
	<-sigCh

	return nil
}
