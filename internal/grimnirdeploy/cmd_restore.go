/*
Copyright (C) 2026 Friends Incode
SPDX-License-Identifier: AGPL-3.0-or-later
*/

package grimnirdeploy

import (
	"context"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/friendsincode/grimnir_radio/internal/grimnirdeploy/audit"
	"github.com/friendsincode/grimnir_radio/internal/grimnirdeploy/probe"
	"github.com/friendsincode/grimnir_radio/internal/grimnirdeploy/runner"
)

// RestoreOpts is the dependency bag for runRestore. The cobra entry point
// builds the production wiring; tests build it directly with a runner.Fake.
//
// PostgresIsRunning is an injectable pre-flight that returns true if a live
// Postgres is detectable on the named host. Tests substitute it; the
// production wiring uses pg_isready via the Runner.
type RestoreOpts struct {
	From       string // backup id, or "latest"
	TargetTime string // RFC3339 PITR target; empty replays to end of WAL
	Hosts      []string
	Runner     runner.Runner
	Compose    *runner.DockerCompose
	Prober     FullProber
	Wrapper    *audit.Wrapper
	Out        io.Writer
	DryRun     bool

	// PostgresIsRunning is an optional injectable pre-flight check. If nil,
	// runRestore uses `pg_isready` via the Runner against Hosts[0].
	PostgresIsRunning func(ctx context.Context, host string) bool

	// Sleep is the time-sink for the pg_isready poll. Tests pass a no-op so
	// the post-restore wait does not take real time.
	Sleep func(interface{})
}

// runRestore implements the pgbackrest restore + service-restart + verify
// sequence. Three pre-flight gates fire before anything mutates:
//
//  1. Backup id (when not "latest") must appear in `pgbackrest info`.
//  2. --target-time (when set) must fall inside the archive WAL window.
//  3. Postgres must NOT be running on the primary host before restore.
//
// Any pre-flight failure aborts before the first compose stop. The real
// sequence then runs: stop services on every host, pgbackrest restore on
// Hosts[0], `systemctl start postgresql`, wait for pg_isready, start services
// on every host, run the full prober against every host.
func runRestore(ctx context.Context, o RestoreOpts) error {
	if o.Sleep == nil {
		o.Sleep = func(_ interface{}) { time.Sleep(time.Second) }
	}
	return o.Wrapper.Wrap(ctx, "restore", map[string]any{"from": o.From, "target_time": o.TargetTime, "dry_run": o.DryRun}, func(ctx context.Context) error {
		say := func(format string, args ...any) { fmt.Fprintf(o.Out, format+"\n", args...) }

		if len(o.Hosts) == 0 {
			return fmt.Errorf("restore: at least one host is required")
		}
		primary := o.Hosts[0]

		// --- Pre-flight 1: backup id must exist (unless "latest"). ---
		// `pgbackrest info` lists every backup on the stanza along with the
		// archive window; we parse both pieces of metadata here so we call
		// pgbackrest once, not twice.
		infoOut, infoErr, infoCode, err := o.Runner.Run(ctx, primary, "pgbackrest info --stanza=grimnir")
		if err != nil || infoCode != 0 {
			return fmt.Errorf("pgbackrest info: code=%d err=%v stderr=%q", infoCode, err, infoErr)
		}
		if o.From != "" && o.From != "latest" && !strings.Contains(infoOut, o.From) {
			return fmt.Errorf("backup id %q not found in pgbackrest info; check available backups", o.From)
		}

		// --- Pre-flight 2: --target-time inside the WAL window. ---
		if o.TargetTime != "" {
			tgt, parseErr := time.Parse(time.RFC3339, o.TargetTime)
			if parseErr != nil {
				return fmt.Errorf("--target-time %q: not RFC3339: %w", o.TargetTime, parseErr)
			}
			oldest, ok := parseOldestWAL(infoOut)
			if ok && tgt.Before(oldest) {
				return fmt.Errorf("--target-time %s predates oldest WAL %s; no PITR target inside archive window",
					tgt.Format(time.RFC3339), oldest.Format(time.RFC3339))
			}
		}

		// --- Pre-flight 3: Postgres must not be running on the primary. ---
		// Restoring on top of a live cluster corrupts the data directory.
		pgRunning := o.PostgresIsRunning
		if pgRunning == nil {
			pgRunning = func(ctx context.Context, h string) bool {
				_, _, code, _ := o.Runner.Run(ctx, h, "pg_isready -q")
				return code == 0
			}
		}
		if !o.DryRun && pgRunning(ctx, primary) {
			return fmt.Errorf("Postgres is still running on %s; stop it before restoring (the restore would corrupt a live data directory)", primary)
		}

		// Stop services on every host.
		for _, h := range o.Hosts {
			say("stopping services on %s", h)
			if o.DryRun {
				say("[dry-run] would stop services on %s", h)
				continue
			}
			for _, svc := range []string{"grimnir-radio", "edge-encoder", "grimnir-fanout", "grimnir-mediaengine"} {
				if err := o.Compose.Stop(ctx, h, svc); err != nil {
					return err
				}
			}
		}

		// Build the pgbackrest restore command.
		cmd := fmt.Sprintf("pgbackrest restore --stanza=grimnir --set=%s", o.From)
		if o.From == "latest" {
			cmd = "pgbackrest restore --stanza=grimnir"
		}
		if o.TargetTime != "" {
			cmd += fmt.Sprintf(" --type=time --target='%s'", o.TargetTime)
		}
		say("pgbackrest restore on %s", primary)
		if o.DryRun {
			say("[dry-run] would run: %s", cmd)
			return nil
		}
		if _, stderr, code, err := o.Runner.Run(ctx, primary, cmd); err != nil || code != 0 {
			return fmt.Errorf("pgbackrest: code=%d err=%v stderr=%q", code, err, stderr)
		}

		// Start Postgres and wait for ready.
		if _, _, _, err := o.Runner.Run(ctx, primary, "systemctl start postgresql"); err != nil {
			return err
		}
		ready := false
		for i := 0; i < 60; i++ {
			if _, _, code, _ := o.Runner.Run(ctx, primary, "pg_isready"); code == 0 {
				ready = true
				break
			}
			o.Sleep(time.Second)
		}
		if !ready {
			return fmt.Errorf("postgres did not become ready within 60s after restore")
		}

		// Restart application services everywhere.
		for _, h := range o.Hosts {
			say("starting services on %s", h)
			if err := o.Compose.Up(ctx, h); err != nil {
				return err
			}
		}

		// Verify.
		for _, h := range o.Hosts {
			r := o.Prober.ProbeAll(ctx, h)
			if !r.ControlPlaneOK || !r.MediaEngineOK {
				return fmt.Errorf("post-restore verify failed on %s: cp=%s me=%s", h, r.ControlPlaneErr, r.MediaEngineErr)
			}
		}
		say("restore complete and verified")
		return nil
	})
}

// parseOldestWAL extracts the "oldest wal timestamp" field from `pgbackrest
// info` output. Returns zero + false if missing or malformed; the caller
// treats that as "skip the gate" rather than failing closed so a parser
// regression doesn't block a real restore during an incident.
func parseOldestWAL(info string) (time.Time, bool) {
	for _, line := range strings.Split(info, "\n") {
		l := strings.TrimSpace(line)
		const prefix = "oldest wal timestamp:"
		if !strings.HasPrefix(l, prefix) {
			continue
		}
		v := strings.TrimSpace(l[len(prefix):])
		v = strings.Replace(v, " ", "T", 1)
		v = strings.Replace(v, "+00", "+00:00", 1)
		v = strings.Replace(v, "+00:00:00", "+00:00", 1)
		if t, err := time.Parse(time.RFC3339, v); err == nil {
			return t, true
		}
	}
	return time.Time{}, false
}

// realRestoreRunE is the cobra entry point. Wires Config + Deps + SSH runner.
func realRestoreRunE(cmd *cobra.Command, args []string) error {
	cfg, err := LoadConfig()
	if err != nil {
		return err
	}
	from, _ := cmd.Flags().GetString("from")
	target, _ := cmd.Flags().GetString("target-time")
	dryRun, _ := cmd.Flags().GetBool("dry-run")
	deps, err := wireDeps(cmd.Context(), cfg)
	if err != nil {
		return err
	}
	defer deps.Close()
	sshRunner := runner.NewSSHRunner(cfg.PeerSSHUser, cfg.PeerSSHPort, cfg.PeerSSHKey, nil)
	compose := runner.NewDockerCompose(sshRunner, "/srv/docker/grimnir_radio")
	hosts := []string{"local"}
	if cfg.PeerHost != "" {
		hosts = append(hosts, cfg.PeerHost)
	}
	return runRestore(cmd.Context(), RestoreOpts{
		From: from, TargetTime: target,
		Hosts:  hosts,
		Runner: sshRunner, Compose: compose, Prober: probe.NewProber(),
		Wrapper: deps.Wrapper, Out: cmd.OutOrStdout(), DryRun: dryRun,
	})
}
