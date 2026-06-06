/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/

package grimnirdeploy

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/redis/go-redis/v9"
	"github.com/spf13/cobra"

	"github.com/friendsincode/grimnir_radio/internal/grimnirdeploy/audit"
	"github.com/friendsincode/grimnir_radio/internal/grimnirdeploy/runner"
)

// RecoverOpts is the dependency bag for runRecoverPartition. Built by
// realRecoverRunE for production wiring; built directly by tests with the
// runner.Fake + miniredis-backed redis.Client.
type RecoverOpts struct {
	Hosts   []string
	VIPs    []string
	Runner  runner.Runner
	Redis   *redis.Client
	Wrapper *audit.Wrapper
	Out     io.Writer
	DryRun  bool
}

// runRecoverPartition reports the cluster's split-brain state and surfaces
// conflicts for operator decision. By design this subcommand does NOT
// auto-merge: divergence after a partition needs human judgement on which
// side is canonical (which writes were taken, which listener flow is "real").
//
// Checks performed:
//  1. VIP holder count per VIP -- exactly 1 host must answer "held".
//  2. Postgres WAL position per host (informational; the operator compares).
//  3. Redis leader-election lease holder (must be exactly one).
//
// Returns nil + "no conflicts detected" if every check passes. Returns a
// non-nil error otherwise so the audit row records the conflict and the
// shell exit code is non-zero.
func runRecoverPartition(ctx context.Context, o RecoverOpts) error {
	return o.Wrapper.Wrap(ctx, "recover-partition", map[string]any{"dry_run": o.DryRun}, func(ctx context.Context) error {
		say := func(format string, args ...any) { fmt.Fprintf(o.Out, format+"\n", args...) }
		if o.DryRun {
			say("[dry-run] would inspect VIPs %v across hosts %v and read leader lease from Redis", o.VIPs, o.Hosts)
			return nil
		}
		var problems []string

		// 1. VIP holder count. A held VIP returns "held"; a non-holder returns
		// "not-held". Anything else (empty, error) is treated as not-held; the
		// operator gets the host list either way.
		for _, vip := range o.VIPs {
			if vip == "" {
				continue
			}
			var holders []string
			for _, h := range o.Hosts {
				cmd := fmt.Sprintf("ip addr show | grep -q %s && echo held || echo not-held", vip)
				stdout, _, _, _ := o.Runner.Run(ctx, h, cmd)
				if strings.Contains(stdout, "held") && !strings.Contains(stdout, "not-held") {
					holders = append(holders, h)
				}
			}
			say("VIP %s holders: %v", vip, holders)
			if len(holders) != 1 {
				problems = append(problems, fmt.Sprintf("VIP %s has %d holders (want 1)", vip, len(holders)))
			}
		}

		// 2. WAL position per node. Informational: the operator compares the
		// two LSNs to decide which side has the more recent writes.
		walPositions := map[string]string{}
		for _, h := range o.Hosts {
			stdout, _, _, _ := o.Runner.Run(ctx, h, "psql -tAc 'SELECT pg_current_wal_lsn()'")
			walPositions[h] = strings.TrimSpace(stdout)
			say("WAL LSN on %s: %s", h, walPositions[h])
		}

		// 3. Leader-election lease holder. Redis returning Nil means the lease
		// expired during the partition; that's a conflict the operator must
		// resolve (re-elect from a chosen side).
		leader, err := o.Redis.Get(ctx, "grimnir:leader").Result()
		switch {
		case errors.Is(err, redis.Nil):
			say("no leader lease held")
			problems = append(problems, "no leader lease held in Redis")
		case err != nil:
			return fmt.Errorf("read leader: %w", err)
		default:
			say("leader lease: %s", leader)
		}

		if len(problems) > 0 {
			say("CONFLICTS:")
			for _, p := range problems {
				say("  - %s", p)
			}
			say("This subcommand does NOT auto-merge. Operator must decide which side is canonical.")
			return errors.New("partition recovery requires operator decision")
		}
		say("no conflicts detected; cluster appears healthy")
		return nil
	})
}

// realRecoverRunE is the cobra entry point wired by newRecoverPartitionCmd in
// cmd_stubs.go. It loads Config + Deps, builds the SSH runner, and defers to
// runRecoverPartition. The VIP list comes from GRIMNIR_LISTENER_VIP +
// GRIMNIR_DJ_VIP (the same vars the verify subcommand will adopt once VIP
// probing lands).
func realRecoverRunE(cmd *cobra.Command, _ []string) error {
	cfg, err := LoadConfig()
	if err != nil {
		return err
	}
	deps, err := wireDeps(cmd.Context(), cfg)
	if err != nil {
		return err
	}
	defer deps.Close()
	sshRunner := runner.NewSSHRunner(cfg.PeerSSHUser, cfg.PeerSSHPort, cfg.PeerSSHKey, nil)
	defer sshRunner.Close()
	dryRun, _ := cmd.Flags().GetBool("dry-run")
	hosts := []string{"local"}
	if cfg.PeerHost != "" {
		hosts = append(hosts, cfg.PeerHost)
	}
	var vips []string
	if v := os.Getenv("GRIMNIR_LISTENER_VIP"); v != "" {
		vips = append(vips, v)
	}
	if v := os.Getenv("GRIMNIR_DJ_VIP"); v != "" {
		vips = append(vips, v)
	}
	return runRecoverPartition(cmd.Context(), RecoverOpts{
		Hosts:   hosts,
		VIPs:    vips,
		Runner:  sshRunner,
		Redis:   deps.Redis,
		Wrapper: deps.Wrapper,
		Out:     cmd.OutOrStdout(),
		DryRun:  dryRun,
	})
}
