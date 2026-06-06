/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/

package grimnirdeploy

import (
	"context"
	"fmt"
	"io"
	"strconv"
	"strings"

	"github.com/spf13/cobra"

	"github.com/friendsincode/grimnir_radio/internal/grimnirdeploy/audit"
	"github.com/friendsincode/grimnir_radio/internal/grimnirdeploy/runner"
)

// PromoteOpts is the dependency bag for runPromoteReplica. Built by
// realPromoteReplicaRunE for production wiring; built directly by tests with
// the runner.Fake.
type PromoteOpts struct {
	PrimaryHost string
	ReplicaHost string
	SkipRebuild bool
	DryRun      bool
	Runner      runner.Runner
	Wrapper     *audit.Wrapper
	Out         io.Writer
}

// runPromoteReplica executes the Postgres failover runbook: verify replica is
// in streaming state with lag < 5s, pg_ctl promote, update pgbouncer on both
// nodes, then rebuild the old primary as the new replica (unless --skip-rebuild
// is set, e.g. when the old primary is dead).
func runPromoteReplica(ctx context.Context, o PromoteOpts) error {
	return o.Wrapper.Wrap(ctx, "promote-replica", map[string]any{"primary": o.PrimaryHost, "replica": o.ReplicaHost, "skip_rebuild": o.SkipRebuild, "dry_run": o.DryRun}, func(ctx context.Context) error {
		// 1. Verify replica is streaming.
		out, _, code, err := o.Runner.Run(ctx, o.ReplicaHost, "psql -tAc 'SELECT status FROM pg_stat_wal_receiver'")
		if err != nil || code != 0 {
			return fmt.Errorf("query wal receiver: code=%d err=%v out=%q", code, err, out)
		}
		if !strings.Contains(out, "streaming") {
			return fmt.Errorf("replica not in streaming state: %q", strings.TrimSpace(out))
		}
		fmt.Fprintf(o.Out, "replica %s status: streaming\n", o.ReplicaHost)

		// 2. Verify lag < 5s.
		out, _, _, err = o.Runner.Run(ctx, o.ReplicaHost, "psql -tAc 'SELECT EXTRACT(EPOCH FROM (now() - pg_last_xact_replay_timestamp()))'")
		if err != nil {
			return err
		}
		lag, err := strconv.ParseFloat(strings.TrimSpace(out), 64)
		if err != nil {
			return fmt.Errorf("parse lag %q: %w", out, err)
		}
		if lag > 5.0 {
			return fmt.Errorf("replication lag %.2fs exceeds 5s threshold; refusing to promote", lag)
		}
		fmt.Fprintf(o.Out, "replication lag: %.2fs\n", lag)

		if o.DryRun {
			fmt.Fprintf(o.Out, "[dry-run] would promote %s and reconfigure pgbouncer\n", o.ReplicaHost)
			return nil
		}

		// 3. Promote.
		_, stderr, code, err := o.Runner.Run(ctx, o.ReplicaHost, "pg_ctl promote -D /var/lib/postgresql/data")
		if err != nil || code != 0 {
			return fmt.Errorf("pg_ctl promote: code=%d err=%v stderr=%q", code, err, stderr)
		}
		fmt.Fprintf(o.Out, "%s promoted to primary\n", o.ReplicaHost)

		// 4. Update pgbouncer (both nodes).
		for _, h := range []string{o.PrimaryHost, o.ReplicaHost} {
			cmd := fmt.Sprintf("sed -i 's/^primary_conninfo.*/primary_conninfo = host=%s/' /etc/pgbouncer/pgbouncer.ini", o.ReplicaHost)
			if _, _, _, err := o.Runner.Run(ctx, h, cmd); err != nil {
				return fmt.Errorf("update pgbouncer on %s: %w", h, err)
			}
			if _, _, _, err := o.Runner.Run(ctx, h, "systemctl reload pgbouncer"); err != nil {
				return fmt.Errorf("reload pgbouncer on %s: %w", h, err)
			}
		}
		fmt.Fprintln(o.Out, "pgbouncer reloaded on both nodes")

		// 5. Rebuild old primary as replica.
		if !o.SkipRebuild {
			cmd := fmt.Sprintf("pg_basebackup -h %s -D /var/lib/postgresql/data --wal-method=stream -P", o.ReplicaHost)
			_, stderr, code, err := o.Runner.Run(ctx, o.PrimaryHost, cmd)
			if err != nil || code != 0 {
				return fmt.Errorf("rebuild old primary: code=%d err=%v stderr=%q", code, err, stderr)
			}
			fmt.Fprintf(o.Out, "old primary %s rebuilt as replica\n", o.PrimaryHost)
		}
		return nil
	})
}

// realPromoteReplicaRunE is the cobra entry point. It wires Config + Deps + the
// SSH runner, then defers to runPromoteReplica. Audit wrapping happens inside
// runPromoteReplica (via the Wrapper).
func realPromoteReplicaRunE(cmd *cobra.Command, args []string) error {
	cfg, err := LoadConfig()
	if err != nil {
		return err
	}
	deps, err := wireDeps(cmd.Context(), cfg)
	if err != nil {
		return err
	}
	defer deps.Close()
	skip, _ := cmd.Flags().GetBool("skip-rebuild")
	dryRun, _ := cmd.Flags().GetBool("dry-run")
	sshRunner := runner.NewSSHRunner(cfg.PeerSSHUser, cfg.PeerSSHPort, cfg.PeerSSHKey, nil)
	defer sshRunner.Close()
	return runPromoteReplica(cmd.Context(), PromoteOpts{
		PrimaryHost: "local", ReplicaHost: cfg.PeerHost,
		SkipRebuild: skip, DryRun: dryRun,
		Runner: sshRunner, Wrapper: deps.Wrapper, Out: cmd.OutOrStdout(),
	})
}
