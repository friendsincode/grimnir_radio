/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/

package playout

import (
	"os/exec"
	"syscall"
)

// newPipelineProcessGroup launches the child in its own process group so we can
// kill the entire group (the `sh -c` wrapper AND the gst-launch grandchild it
// spawned). Without this, signalling cmd.Process kills only the shell;
// gst-launch orphans to PID 1 and keeps writing to the now-orphan output
// pipes — accumulating leaked decoder processes per day on a busy station and
// producing audible artifacts because every leaked pipeline is still feeding
// bytes into its target.
//
// Used by crossfade.go's PCM decoder spawning (still subprocess-based for the
// per-track decoders that run alongside the encoder appsrc). The main
// broadcast Pipeline moved off subprocess management in NetClock Chunk 1
// Task 1.2 and no longer needs this helper.
func newPipelineProcessGroup() *syscall.SysProcAttr {
	return &syscall.SysProcAttr{Setpgid: true}
}

// killProcessGroup sends a signal to the entire process group of cmd so the
// shell wrapper AND its gst-launch child both die. Falls back to signalling
// the leader process if pgid lookup fails (still better than nothing).
func killProcessGroup(cmd *exec.Cmd, sig syscall.Signal) {
	if cmd == nil || cmd.Process == nil {
		return
	}
	pgid, err := syscall.Getpgid(cmd.Process.Pid)
	if err == nil && pgid != 0 {
		_ = syscall.Kill(-pgid, sig)
		return
	}
	_ = cmd.Process.Signal(sig)
}
