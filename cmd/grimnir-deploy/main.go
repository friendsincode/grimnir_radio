/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/

// grimnir-deploy is the single operator binary for the HA cluster.
// It orchestrates rolling updates across two HA nodes, runs operational
// runbooks as first-class subcommands, and gates every action through a
// shared pre-flight + audit-log layer.
//
// See docs/superpowers/plans/2026-06-01-ha-zero-loss-failover-design.md
// Sections 6, 8.2, and 8.3 for the design contract this binary implements.
package main

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/friendsincode/grimnir_radio/internal/grimnirdeploy"
	"github.com/friendsincode/grimnir_radio/internal/version"
)

var rootCmd = &cobra.Command{
	Use:     "grimnir-deploy",
	Short:   "Operator binary for HA rolling updates and runbooks",
	Long:    "grimnir-deploy orchestrates rolling updates across the two HA nodes, runs operational runbooks (promote-replica, drain, cold-start-region, restore, recover-partition, backup-drill), and gates every action via per-region deploy policy.",
	Version: version.Version,
}

func init() {
	rootCmd.SetVersionTemplate("grimnir-deploy {{.Version}}\n")
	grimnirdeploy.RegisterCommands(rootCmd)
}

func main() {
	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
}
