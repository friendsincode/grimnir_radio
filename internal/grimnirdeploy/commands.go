/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/

// Package grimnirdeploy implements the subcommand tree of the grimnir-deploy
// binary. Each subcommand lives in its own file (cmd_<name>.go) and is
// registered through RegisterCommands.
package grimnirdeploy

import "github.com/spf13/cobra"

// RegisterCommands attaches every subcommand to the given root command.
// Called once from cmd/grimnir-deploy/main.go.
func RegisterCommands(root *cobra.Command) {
	root.AddCommand(newDeployCmd())
	root.AddCommand(newVerifyCmd())
	root.AddCommand(newDrainCmd())
	root.AddCommand(newEmergencyPauseCmd())
	root.AddCommand(newEmergencyResumeCmd())
	root.AddCommand(newPromoteReplicaCmd())
	root.AddCommand(newColdStartRegionCmd())
	root.AddCommand(newRestoreCmd())
	root.AddCommand(newRecoverPartitionCmd())
	root.AddCommand(newBackupDrillCmd())
}
