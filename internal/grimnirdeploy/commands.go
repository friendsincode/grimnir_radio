/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/

// Package grimnirdeploy implements the subcommand tree of the grimnir-deploy
// binary. Each subcommand lives in its own file (cmd_<name>.go) and is
// registered through RegisterCommands.
package grimnirdeploy

import (
	"github.com/spf13/cobra"

	"github.com/friendsincode/grimnir_radio/internal/grimnirdeploy/audit"
)

// RegisterCommands attaches every subcommand to the given root command. Each
// subcommand's RunE is wrapped via audit.WrapCobra so the operator gets a
// START / COMPLETE / FAILED row in audit_log + paired ntfy notification for
// free; subcommand authors never touch audit code directly.
//
// A nil wrapper is supported (no audit bookkeeping) so unit tests and the
// pre-config-wiring binary still work.
func RegisterCommands(root *cobra.Command, w *audit.Wrapper) {
	for _, c := range []*cobra.Command{
		newDeployCmd(),
		newVerifyCmd(),
		newDrainCmd(),
		newEmergencyPauseCmd(),
		newEmergencyResumeCmd(),
		newPromoteReplicaCmd(),
		newColdStartRegionCmd(),
		newRestoreCmd(),
		newRecoverPartitionCmd(),
		newBackupDrillCmd(),
	} {
		audit.WrapCobra(w, c)
		root.AddCommand(c)
	}
}
