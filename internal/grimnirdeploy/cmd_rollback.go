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
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/friendsincode/grimnir_radio/internal/grimnirdeploy/audit"
	"github.com/friendsincode/grimnir_radio/internal/grimnirdeploy/gates"
	"github.com/friendsincode/grimnir_radio/internal/grimnirdeploy/history"
	"github.com/friendsincode/grimnir_radio/internal/grimnirdeploy/pause"
	"github.com/friendsincode/grimnir_radio/internal/grimnirdeploy/probe"
	"github.com/friendsincode/grimnir_radio/internal/grimnirdeploy/runner"
)

// RollbackOpts is the dependency bag for runRollback. Mirrors DeployOpts so
// the per-node rolling sequence can be reused unchanged; the only flow
// differences are the two refusal gates (eligibility, contract crossings),
// the required --reason, and the target tag (previous successful instead of
// caller-supplied).
type RollbackOpts struct {
	Cfg           *Config
	Reason        string
	ForceAged     bool
	ForceContract bool
	Hosts         []string
	FirstHost     string
	SecondHost    string
	Runner        runner.Runner
	Compose       *runner.DockerCompose
	HealthProbe   gates.HealthProbe
	Pause         *pause.Client
	History       *history.Store
	Wrapper       *audit.Wrapper
	Out           io.Writer
	Sleep         func(time.Duration)

	HealthWaitTimeout time.Duration
}

// runRollback rolls the cluster back to the previous successful tag. Refuses
// with a clear error in any of three cases: --reason missing, the last
// successful deploy completed outside the eligibility window (unless
// --force-aged-rollback), or one or more contract migrations sit between the
// current tag and the rollback target (unless --force-through-contract-migration).
//
// On success it reuses the per-node rolling sequence from runDeploy, then
// stamps deploy_history with outcome=rollback.
func runRollback(ctx context.Context, o RollbackOpts) error {
	if o.Sleep == nil {
		o.Sleep = time.Sleep
	}
	if strings.TrimSpace(o.Reason) == "" {
		return errors.New("--reason is required for rollback")
	}
	return o.Wrapper.Wrap(ctx, "rollback", map[string]any{
		"reason":         o.Reason,
		"force_aged":     o.ForceAged,
		"force_contract": o.ForceContract,
	}, func(ctx context.Context) error {
		last, err := o.History.LastSuccessful(ctx, o.Cfg.Region)
		if err != nil {
			return err
		}
		if last == nil {
			return errors.New("no previous successful deploy found in deploy_history")
		}
		currentTag, _ := o.Compose.CurrentTag(ctx, "local", "grimnir-radio")
		if currentTag != "" && currentTag == last.Tag {
			return fmt.Errorf("currently running %s == last successful; nothing to roll back", currentTag)
		}

		// Eligibility window.
		ok, err := o.History.WithinEligibility(ctx, o.Cfg.Region, o.Cfg.RollbackWindow)
		if err != nil {
			return err
		}
		if !ok && !o.ForceAged {
			return fmt.Errorf("eligibility window (%v) exceeded; pass --force-aged-rollback to override (contract migrations may have run since)", o.Cfg.RollbackWindow)
		}

		// Contract-migration refusal.
		crossings, err := o.History.ContractCrossings(ctx, o.Cfg.Region, last.Tag, currentTag)
		if err != nil {
			return err
		}
		if len(crossings) > 0 && !o.ForceContract {
			return fmt.Errorf("rollback would cross contract migrations: %s; pass --force-through-contract-migration AND --reason explaining why this is safe", strings.Join(crossings, ", "))
		}
		if len(crossings) > 0 {
			fmt.Fprintf(o.Out, "WARNING: rolling through contract migrations: %s\n", strings.Join(crossings, ", "))
		}

		operator := "unknown"
		if op := o.Wrapper.Operator(); op != "" {
			operator = op
		}
		histID, err := o.History.Start(ctx, o.Cfg.Region, last.Tag, currentTag, operator)
		if err != nil {
			return fmt.Errorf("history start: %w", err)
		}

		// Reuse the per-node sequence from runDeploy with the previous-successful
		// tag as the "new" image. rollNode revert-pivots on the prevTag arg, so
		// pass the current (now-bad) tag there: a failed rollback rolls forward
		// to the version we were trying to escape from, which is the only safe
		// fallback target known at this layer.
		dep := DeployOpts{
			Tag:               last.Tag,
			Cfg:               o.Cfg,
			Hosts:             o.Hosts,
			FirstHost:         o.FirstHost,
			SecondHost:        o.SecondHost,
			Runner:            o.Runner,
			Compose:           o.Compose,
			HealthProbe:       o.HealthProbe,
			Pause:             o.Pause,
			History:           o.History,
			Wrapper:           o.Wrapper,
			Out:               o.Out,
			Sleep:             o.Sleep,
			HealthWaitTimeout: o.HealthWaitTimeout,
		}
		if err := dep.rollNode(ctx, o.FirstHost, false, currentTag); err != nil {
			_ = o.History.Fail(ctx, histID, history.OutcomeFailed, fmt.Sprintf("rollback first node: %v", err))
			return err
		}
		if err := dep.rollNode(ctx, o.SecondHost, false, currentTag); err != nil {
			_ = o.History.Fail(ctx, histID, history.OutcomeFailed, fmt.Sprintf("rollback second node: %v", err))
			return err
		}

		// Soak; rollback uses a shorter window than forward deploys (15s)
		// because the target tag is already known-good in production.
		soak := 15 * time.Second
		if o.Cfg.SoakWindow > 0 && o.Cfg.SoakWindow < soak {
			soak = o.Cfg.SoakWindow
		}
		if soak > 0 {
			fmt.Fprintf(o.Out, "rollback soak: waiting %v\n", soak)
			o.Sleep(soak)
		}
		if err := o.History.Complete(ctx, histID, history.OutcomeRollback, history.SoakPassed); err != nil {
			return err
		}
		fmt.Fprintf(o.Out, "rollback to %s complete (reason: %s)\n", last.Tag, o.Reason)
		return nil
	})
}

// realRollbackRunE is the cobra path for `grimnir-deploy deploy --rollback`.
// Wires Config + Deps + the SSH runner + DockerCompose + the prober, then
// defers to runRollback. The audit wrapping happens inside runRollback.
func realRollbackRunE(cmd *cobra.Command, _ []string) error {
	cfg, err := LoadConfig()
	if err != nil {
		return err
	}
	deps, err := wireDeps(cmd.Context(), cfg)
	if err != nil {
		return err
	}
	defer deps.Close()

	reason, _ := cmd.Flags().GetString("reason")
	forceAged, _ := cmd.Flags().GetBool("force-aged-rollback")
	forceContract, _ := cmd.Flags().GetBool("force-through-contract-migration")

	sshRunner := runner.NewSSHRunner(cfg.PeerSSHUser, cfg.PeerSSHPort, cfg.PeerSSHKey, nil)
	defer sshRunner.Close()
	compose := runner.NewDockerCompose(sshRunner, "/srv/docker/grimnir_radio")

	hosts := []string{"local"}
	firstHost, secondHost := "local", "local"
	if cfg.PeerHost != "" {
		hosts = append(hosts, cfg.PeerHost)
		firstHost = cfg.PeerHost
		secondHost = "local"
	}

	return runRollback(cmd.Context(), RollbackOpts{
		Cfg:           cfg,
		Reason:        reason,
		ForceAged:     forceAged,
		ForceContract: forceContract,
		Hosts:         hosts,
		FirstHost:     firstHost,
		SecondHost:    secondHost,
		Runner:        sshRunner,
		Compose:       compose,
		HealthProbe:   probe.NewProber(),
		Pause:         deps.Pause,
		History:       history.NewStore(deps.DB),
		Wrapper:       deps.Wrapper,
		Out:           cmd.OutOrStdout(),
	})
}
