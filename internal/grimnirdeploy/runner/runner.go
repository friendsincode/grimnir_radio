/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/

// Package runner abstracts command execution on the local node or on the
// peer node via SSH. Both implementations satisfy Runner.
package runner

import "context"

// Runner executes a shell command on a target host and returns its captured
// stdout, stderr, exit code, and any transport error.
//
// Host == "local" means execute on the current node via os/exec.
// Any other host is treated as an SSH target.
type Runner interface {
	Run(ctx context.Context, host, cmd string) (stdout, stderr string, exitCode int, err error)
}
