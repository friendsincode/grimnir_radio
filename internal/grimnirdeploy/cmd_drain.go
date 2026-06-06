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

// DrainOpts is the dependency bag for runDrain. Built by realDrainRunE for
// production wiring; built directly by tests with the runner.Fake.
type DrainOpts struct {
	Node    string
	Grace   time.Duration
	Runner  runner.Runner
	Compose *runner.DockerCompose
	Wrapper *audit.Wrapper
	Out     io.Writer
	Sleep   func(time.Duration)
	DryRun  bool
}

// runDrain executes the per-node drain runbook per Section 6 step 2 of the HA
// design: touch the VRRP failure file so the peer takes the VIP, wait 3s for
// VIP float, then SIGTERM grimnir / edge-encoder / fan-out / mediaengine in
// dependency order (outermost listeners first). Used both standalone (operator
// runbook before a reboot or hardware swap) and as the first step inside the
// deploy rolling sequence.
func runDrain(ctx context.Context, o DrainOpts) error {
	if o.Sleep == nil {
		o.Sleep = time.Sleep
	}
	if o.Grace == 0 {
		o.Grace = 30 * time.Second
	}
	return o.Wrapper.Wrap(ctx, "drain", map[string]any{"node": o.Node, "dry_run": o.DryRun}, func(ctx context.Context) error {
		host := o.Node
		if host == "self" {
			host = "local"
		}
		if o.DryRun {
			fmt.Fprintf(o.Out, "[dry-run] would drain %s\n", host)
			return nil
		}
		fmt.Fprintf(o.Out, "drain %s: dropping VRRP priority\n", host)
		if _, _, _, err := o.Runner.Run(ctx, host, "touch /var/run/keepalived/vrrp_fail"); err != nil {
			return err
		}
		o.Sleep(3 * time.Second) // VIP float delay
		for _, svc := range []string{"grimnir-radio", "edge-encoder", "grimnir-fanout", "grimnir-mediaengine"} {
			fmt.Fprintf(o.Out, "drain %s: stopping %s (grace %v)\n", host, svc, o.Grace)
			if err := o.Compose.Stop(ctx, host, svc); err != nil {
				return fmt.Errorf("stop %s: %w", svc, err)
			}
		}
		fmt.Fprintf(o.Out, "drain %s: complete\n", host)
		return nil
	})
}

// realDrainRunE is the cobra entry point. It wires Config + Deps + the
// runner + DockerCompose helper, then defers to runDrain. The audit-wrapping
// happens inside runDrain (via the Wrapper).
func realDrainRunE(cmd *cobra.Command, args []string) error {
	cfg, err := LoadConfig()
	if err != nil {
		return err
	}
	node, _ := cmd.Flags().GetString("node")
	dryRun, _ := cmd.Flags().GetBool("dry-run")
	grace, _ := cmd.Flags().GetDuration("grace")
	deps, err := wireDeps(cmd.Context(), cfg)
	if err != nil {
		return err
	}
	defer deps.Close()
	sshRunner := runner.NewSSHRunner(cfg.PeerSSHUser, cfg.PeerSSHPort, cfg.PeerSSHKey, nil)
	defer sshRunner.Close()
	compose := runner.NewDockerCompose(sshRunner, "/srv/docker/grimnir_radio")
	return runDrain(cmd.Context(), DrainOpts{
		Node: node, Grace: grace, Runner: sshRunner, Compose: compose,
		Wrapper: deps.Wrapper, Out: cmd.OutOrStdout(), DryRun: dryRun,
	})
}
