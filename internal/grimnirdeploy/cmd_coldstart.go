/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/

package grimnirdeploy

import (
	"context"
	"fmt"
	"io"

	"github.com/spf13/cobra"

	"github.com/friendsincode/grimnir_radio/internal/grimnirdeploy/audit"
	"github.com/friendsincode/grimnir_radio/internal/grimnirdeploy/runner"
)

// ColdStartOpts is the dependency bag for runColdStartRegion. Production wiring
// goes through realColdStartRunE; tests build it directly with a runner.Fake.
type ColdStartOpts struct {
	Region  string
	Hosts   []string
	Runner  runner.Runner
	Compose *runner.DockerCompose
	Wrapper *audit.Wrapper
	Out     io.Writer
	DryRun  bool
}

// runColdStartRegion brings up a freshly-built region from scratch in the
// dependency order from Section 9.1 of the HA design: firewall, WireGuard mesh,
// Postgres primary + replica, Redis, object-storage reachability, then the
// application stack via the ./grimnir wrapper. Each step is idempotent so the
// command is safe to re-run on a half-up region.
func runColdStartRegion(ctx context.Context, o ColdStartOpts) error {
	return o.Wrapper.Wrap(ctx, "cold-start-region", map[string]any{"region": o.Region, "dry_run": o.DryRun}, func(ctx context.Context) error {
		say := func(format string, args ...any) { fmt.Fprintf(o.Out, format+"\n", args...) }
		say("cold-start region %s on %v", o.Region, o.Hosts)
		if o.DryRun {
			say("[dry-run] would bring up firewall, wireguard, postgres, redis, applications")
			return nil
		}

		// 1. Firewall (deny-all + explicit allow rules).
		for _, h := range o.Hosts {
			say("firewall on %s", h)
			if _, _, _, err := o.Runner.Run(ctx, h, "iptables -P INPUT DROP && iptables -A INPUT -i lo -j ACCEPT && iptables -A INPUT -p tcp --dport 22 -j ACCEPT"); err != nil {
				return fmt.Errorf("firewall on %s: %w", h, err)
			}
		}
		// 2. WireGuard mesh.
		for _, h := range o.Hosts {
			say("wireguard on %s", h)
			if _, _, _, err := o.Runner.Run(ctx, h, "wg-quick up wg0"); err != nil {
				return fmt.Errorf("wg on %s: %w", h, err)
			}
		}
		// 3. Postgres (primary on first host; replica on second).
		say("postgres primary on %s", o.Hosts[0])
		if _, _, _, err := o.Runner.Run(ctx, o.Hosts[0], "systemctl start postgresql && pg_isready"); err != nil {
			return err
		}
		say("postgres replica on %s", o.Hosts[1])
		if _, _, _, err := o.Runner.Run(ctx, o.Hosts[1], "systemctl start postgresql && pg_isready"); err != nil {
			return err
		}
		// 4. Redis.
		for _, h := range o.Hosts {
			say("redis on %s", h)
			if _, _, _, err := o.Runner.Run(ctx, h, "systemctl start redis && redis-cli ping"); err != nil {
				return err
			}
		}
		// 5. Object storage reachability probe.
		for _, h := range o.Hosts {
			if _, _, _, err := o.Runner.Run(ctx, h, "curl -sS -m 5 -o /dev/null -w '%{http_code}' $GRIMNIR_S3_ENDPOINT/minio/health/ready"); err != nil {
				return fmt.Errorf("s3 probe on %s: %w", h, err)
			}
		}
		// 6. Application stack (compose up).
		for _, h := range o.Hosts {
			say("grimnir-radio + dependencies on %s", h)
			if err := o.Compose.Up(ctx, h); err != nil {
				return err
			}
		}
		say("cold-start complete; run `grimnir-deploy verify` to validate")
		return nil
	})
}

// realColdStartRunE is the cobra entry point: it wires Config + Deps + the SSH
// runner + DockerCompose helper, then defers to runColdStartRegion (which owns
// the audit wrapping).
func realColdStartRunE(cmd *cobra.Command, args []string) error {
	cfg, err := LoadConfig()
	if err != nil {
		return err
	}
	region, _ := cmd.Flags().GetString("region")
	dryRun, _ := cmd.Flags().GetBool("dry-run")
	deps, err := wireDeps(cmd.Context(), cfg)
	if err != nil {
		return err
	}
	defer deps.Close()
	sshRunner := runner.NewSSHRunner(cfg.PeerSSHUser, cfg.PeerSSHPort, cfg.PeerSSHKey, nil)
	defer sshRunner.Close()
	compose := runner.NewDockerCompose(sshRunner, "/srv/docker/grimnir_radio")
	return runColdStartRegion(cmd.Context(), ColdStartOpts{
		Region: region, Hosts: []string{"local", cfg.PeerHost},
		Runner: sshRunner, Compose: compose,
		Wrapper: deps.Wrapper, Out: cmd.OutOrStdout(), DryRun: dryRun,
	})
}
