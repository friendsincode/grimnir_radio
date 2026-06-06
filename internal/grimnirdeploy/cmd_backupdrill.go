/*
Copyright (C) 2026 Friends Incode
SPDX-License-Identifier: AGPL-3.0-or-later
*/

package grimnirdeploy

import (
	"context"
	"fmt"
	"io"
	"time"

	"github.com/spf13/cobra"

	"github.com/friendsincode/grimnir_radio/internal/grimnirdeploy/audit"
	"github.com/friendsincode/grimnir_radio/internal/grimnirdeploy/runner"
)

// BackupDrillOpts is the dependency bag for runBackupDrill. The cobra entry
// point builds production wiring; tests build it directly with a runner.Fake.
type BackupDrillOpts struct {
	Region    string
	DrillHost string
	Runner    runner.Runner
	Wrapper   *audit.Wrapper
	Out       io.Writer
	DryRun    bool
}

// runBackupDrill stands up a temporary Postgres on the named drill host,
// restores the latest pgbackrest backup into it, then reports measured RTO +
// RPO. The drill is non-destructive to production: the staging Postgres lives
// in /tmp/drill-data on the drill host & is torn down on exit.
//
// Pre-flight: a `docker run` smoke for postgres:16 image presence; if absent,
// pgbackrest still runs but RTO will include image-pull time. We accept that
// to keep the drill close to a real cold-start scenario.
//
// Measured values:
//
//   - staging-startup: time from `docker run` start to container running
//   - pgbackrest restore: time for base-restore + WAL replay to end-of-archive
//   - total RTO: the sum
//   - RPO bound: archive_timeout (30s) + push retry interval
//
// The numbers go to stdout & to the audit_log row via the wrapper.
func runBackupDrill(ctx context.Context, o BackupDrillOpts) error {
	return o.Wrapper.Wrap(ctx, "backup-drill", map[string]any{
		"region":     o.Region,
		"drill_host": o.DrillHost,
		"dry_run":    o.DryRun,
	}, func(ctx context.Context) error {
		say := func(format string, args ...any) { fmt.Fprintf(o.Out, format+"\n", args...) }
		say("backup drill on %s for region %s", o.DrillHost, o.Region)
		if o.DryRun {
			say("[dry-run] would stand up staging Postgres + restore latest backup")
			say("[dry-run] would tear down staging container on exit")
			return nil
		}

		containerName := fmt.Sprintf("grimnir-drill-%d", time.Now().Unix())
		// Best-effort cleanup; never fail the drill on teardown error.
		defer func() {
			_, _, _, _ = o.Runner.Run(context.Background(), o.DrillHost,
				fmt.Sprintf("docker rm -f %s", containerName))
		}()

		startUp := time.Now()
		cmd := fmt.Sprintf("docker run --rm -d --name %s -v /tmp/drill-data:/var/lib/postgresql/data postgres:16", containerName)
		if _, stderr, code, err := o.Runner.Run(ctx, o.DrillHost, cmd); err != nil || code != 0 {
			return fmt.Errorf("docker run: code=%d err=%v stderr=%q", code, err, stderr)
		}
		startupDuration := time.Since(startUp)

		startRestore := time.Now()
		restore := "pgbackrest restore --stanza=grimnir --pg1-path=/tmp/drill-data"
		if _, stderr, code, err := o.Runner.Run(ctx, o.DrillHost, restore); err != nil || code != 0 {
			return fmt.Errorf("pgbackrest restore: code=%d err=%v stderr=%q", code, err, stderr)
		}
		restoreDuration := time.Since(startRestore)

		say("RTO measured:")
		say("  staging-startup: %v", startupDuration)
		say("  pgbackrest restore: %v", restoreDuration)
		say("  total: %v", startupDuration+restoreDuration)
		say("RPO bound: archive_timeout (30s) + push retry interval")
		return nil
	})
}

// realBackupDrillRunE is the cobra entry point. Wires Config + Deps + SSH.
func realBackupDrillRunE(cmd *cobra.Command, args []string) error {
	cfg, err := LoadConfig()
	if err != nil {
		return err
	}
	region, _ := cmd.Flags().GetString("region")
	host, _ := cmd.Flags().GetString("drill-host")
	dryRun, _ := cmd.Flags().GetBool("dry-run")
	deps, err := wireDeps(cmd.Context(), cfg)
	if err != nil {
		return err
	}
	defer deps.Close()
	sshRunner := runner.NewSSHRunner(cfg.PeerSSHUser, cfg.PeerSSHPort, cfg.PeerSSHKey, nil)
	return runBackupDrill(cmd.Context(), BackupDrillOpts{
		Region: region, DrillHost: host,
		Runner: sshRunner, Wrapper: deps.Wrapper, Out: cmd.OutOrStdout(), DryRun: dryRun,
	})
}
