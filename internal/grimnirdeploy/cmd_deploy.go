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

	"github.com/google/uuid"
	"github.com/spf13/cobra"

	"github.com/friendsincode/grimnir_radio/internal/grimnirdeploy/audit"
	"github.com/friendsincode/grimnir_radio/internal/grimnirdeploy/autorollback"
	"github.com/friendsincode/grimnir_radio/internal/grimnirdeploy/gates"
	"github.com/friendsincode/grimnir_radio/internal/grimnirdeploy/history"
	"github.com/friendsincode/grimnir_radio/internal/grimnirdeploy/pause"
	"github.com/friendsincode/grimnir_radio/internal/grimnirdeploy/probe"
	"github.com/friendsincode/grimnir_radio/internal/grimnirdeploy/runner"
	"github.com/friendsincode/grimnir_radio/internal/notify"
)

// DeployOpts is the dependency bag for runDeploy. Built by realDeployRunE for
// production wiring; built directly by tests with the runner.Fake.
type DeployOpts struct {
	Tag         string
	Cfg         *Config
	Hosts       []string
	FirstHost   string // node to upgrade first (typically the non-leader)
	SecondHost  string // node to upgrade second
	Runner      runner.Runner
	Compose     *runner.DockerCompose
	HealthProbe gates.HealthProbe
	Pause       *pause.Client
	History     *history.Store
	Wrapper     *audit.Wrapper
	Out         io.Writer
	Sleep       func(time.Duration)

	// HealthWaitTimeout caps the per-node wait-for-health loop. 0 means use
	// the 60-second default; tests set a few ms.
	HealthWaitTimeout time.Duration

	// SoakObserver actively monitors the soak window. When non-nil, replaces
	// the passive time.Sleep(SoakWindow) path: the Observer polls
	// Prometheus & may trigger an auto-rollback if a rule breaches its
	// dwell threshold. Nil means "legacy passive soak"; production wires an
	// autorollback.Monitor here when GRIMNIR_DEPLOY_AUTOROLLBACK_ENABLED.
	SoakObserver autorollback.Observer

	// AutoRollback is the closure invoked when SoakObserver returns
	// DecisionRollback. Production binds this to runRollback. Nil falls
	// back to logging the verdict without rolling back (which lets early
	// rollouts ship with the observer in shadow mode).
	AutoRollback func(ctx context.Context, reason string) error

	// Notifier sends tier-2 ntfy pages when auto-rollback fires. Nil is a
	// silent no-op (tests don't need real ntfy).
	Notifier notify.Notifier

	// Cobra flag relay.
	DryRun      bool
	ForcePolicy string
	GoFlag      bool
}

func (o *DeployOpts) waitTimeout() time.Duration {
	if o.HealthWaitTimeout <= 0 {
		return 60 * time.Second
	}
	return o.HealthWaitTimeout
}

// pauseReader adapts the region-aware pause.Client to the region-less
// gates.PauseReader. Constructed inside runDeploy with the configured region
// baked in so the gate stays generic.
type pauseReader struct {
	c      *pause.Client
	region string
}

// Read returns the pause state for the bound region.
func (p *pauseReader) Read(ctx context.Context) (*pause.State, error) {
	return p.c.Read(ctx, p.region)
}

// runDeploy runs the full rolling deploy sequence per Section 6 of the HA
// design. Order: gates -> Start history row -> roll first node -> roll second
// node -> soak -> Complete history row. Any per-node failure triggers a revert
// for that node + a Fail history row with the failure detail.
func runDeploy(ctx context.Context, o DeployOpts) error {
	if o.Sleep == nil {
		o.Sleep = time.Sleep
	}
	return o.Wrapper.Wrap(ctx, "deploy", map[string]any{
		"tag": o.Tag, "dry_run": o.DryRun,
	}, func(ctx context.Context) error {
		// Pre-flight gates.
		policy := o.Cfg.DeployPolicy
		if o.ForcePolicy != "" {
			policy = o.ForcePolicy
		}
		tagGate := gates.NewTagSuffixGate(o.Tag, policy)
		all := []gates.Gate{
			gates.NewPauseGate(&pauseReader{c: o.Pause, region: o.Cfg.Region}),
			tagGate,
			gates.NewPolicyGate(policy, o.Cfg.DeployWindowCron, tagGate.OverridesPolicy(), o.GoFlag, time.Now),
			gates.NewImageGate(o.Runner, o.Hosts, "ghcr.io/friendsincode/grimnir-radio:"+o.Tag),
			gates.NewHealthGate(o.HealthProbe, o.Hosts),
		}
		if err := gates.RunAll(ctx, all...); err != nil {
			return err
		}

		// Capture previous tag for deploy_history.
		prevTag, _ := o.Compose.CurrentTag(ctx, o.FirstHost, "grimnir-radio")

		operator := "unknown"
		if op := o.Wrapper.Operator(); op != "" {
			operator = op
		}
		histID, err := o.History.Start(ctx, o.Cfg.Region, o.Tag, prevTag, operator)
		if err != nil {
			return fmt.Errorf("history start: %w", err)
		}

		if o.DryRun {
			fmt.Fprintf(o.Out, "[dry-run] would deploy %s across %v (prev: %s)\n",
				o.Tag, o.Hosts, prevTag)
			_ = o.History.Complete(ctx, histID, history.OutcomeSuccess, history.SoakSkipped)
			return nil
		}

		// Roll first node.
		if err := o.rollNode(ctx, o.FirstHost, true, prevTag); err != nil {
			_ = o.History.Fail(ctx, histID, history.OutcomeRolledBackMidRoll, err.Error())
			return fmt.Errorf("first-node deploy failed and was reverted: %w", err)
		}
		fmt.Fprintf(o.Out, "first node (%s) upgraded to %s\n", o.FirstHost, o.Tag)

		// Roll second node.
		if err := o.rollNode(ctx, o.SecondHost, false, prevTag); err != nil {
			// Per Section 6: try to revert second; if that fails, revert first too.
			if rerr := o.revert(ctx, o.FirstHost, prevTag); rerr != nil {
				_ = o.History.Fail(ctx, histID, history.OutcomeFailed,
					fmt.Sprintf("second-node failed (%v); first-node revert also failed (%v)", err, rerr))
				return fmt.Errorf("second-node deploy failed; first-node revert also failed: %v / %v", err, rerr)
			}
			_ = o.History.Fail(ctx, histID, history.OutcomeRolledBackMidRoll, err.Error())
			return fmt.Errorf("second-node deploy failed; cluster reverted to %s: %w", prevTag, err)
		}
		fmt.Fprintf(o.Out, "second node (%s) upgraded to %s\n", o.SecondHost, o.Tag)

		// Soak.
		if o.Cfg.SoakWindow > 0 {
			if o.SoakObserver != nil {
				fmt.Fprintf(o.Out, "soak: observing for %v (auto-rollback enabled)\n", o.Cfg.SoakWindow)
				verdict := o.SoakObserver.Observe(ctx)
				fmt.Fprintf(o.Out, "soak verdict: %s (%s)\n", verdict.Decision, verdict.Reason)
				if verdict.Decision == autorollback.DecisionRollback {
					return o.handleAutoRollback(ctx, histID, verdict)
				}
				// Pass + Inconclusive both fall through to the closing
				// health probe + soak_passed history row. Inconclusive is a
				// soft-pass at this layer; operators can tighten the policy
				// later if Prometheus outages become common.
			} else {
				fmt.Fprintf(o.Out, "soak: waiting %v\n", o.Cfg.SoakWindow)
				o.Sleep(o.Cfg.SoakWindow)
			}
		}
		// One last health probe at the end of soak. Any unhealthy host
		// flips the history row to soak_failed; the caller's auto-rollback
		// is a separate subcommand wired by --rollback.
		for _, h := range o.Hosts {
			if err := o.HealthProbe.Probe(ctx, h); err != nil {
				_ = o.History.Complete(ctx, histID, history.OutcomeSoakFailed, history.SoakFailed)
				return fmt.Errorf("soak failed: %s unhealthy: %w", h, err)
			}
		}
		if err := o.History.Complete(ctx, histID, history.OutcomeSuccess, history.SoakPassed); err != nil {
			return fmt.Errorf("history complete: %w", err)
		}
		fmt.Fprintln(o.Out, "soak passed; deploy complete")
		return nil
	})
}

// handleAutoRollback is the soak-window-failed branch: it records the
// failed history row, pages the operator (tier-2 ntfy), writes a dedicated
// "auto-rollback" audit entry, & invokes the rollback closure. The Wrapper
// around runDeploy already opened an audit row for the parent deploy;
// handleAutoRollback opens a separate, nested audit row so the post-mortem
// can tell auto-rollback (system-initiated) apart from operator-initiated
// rollback.
//
// Safe re-triggering: the parent runDeploy invocation only ever reaches
// this branch once per call, because SoakObserver.Observe returns once.
// The rollback closure itself does NOT loop back into a soak window with
// auto-rollback wired (RollbackOpts has its own shorter, passive soak),
// so a failing rollback cannot re-enter this function within the same
// deploy. Both invariants are documented on Observer & runRollback.
func (o *DeployOpts) handleAutoRollback(ctx context.Context, histID uuid.UUID, v autorollback.Verdict) error {
	// 1. Flip the deploy_history row to soak_failed BEFORE attempting the
	//    rollback so the row reflects the truth even if the rollback
	//    itself panics.
	_ = o.History.Complete(ctx, histID, history.OutcomeSoakFailed, history.SoakFailed)

	// 2. Page the operator. ntfy errors are non-fatal — the audit + history
	//    row are the system-of-record; the page is best-effort.
	if o.Notifier != nil {
		_ = o.Notifier.Tier2(ctx,
			"[grimnir] Auto-rollback triggered",
			fmt.Sprintf("soak window observed %s: %s; rolling back now",
				v.TriggeringRule, v.Reason))
	}

	// 3. Nested audit entry tagged "auto-rollback" so the post-mortem can
	//    tell system-initiated from operator-initiated rollback. The inner
	//    body actually runs the rollback closure.
	rollbackErr := o.Wrapper.Wrap(ctx, "auto-rollback", map[string]any{
		"triggering_rule": v.TriggeringRule,
		"reason":          v.Reason,
		"ticks_observed":  v.TicksObserved,
		"query_errors":    v.QueryErrors,
	}, func(ctx context.Context) error {
		if o.AutoRollback == nil {
			fmt.Fprintln(o.Out, "auto-rollback: no rollback closure wired; logged verdict only")
			return nil
		}
		return o.AutoRollback(ctx, v.Reason)
	})

	if rollbackErr != nil {
		// Roll-back failure ntfy. Distinct title so operators with a
		// rotating-light ringtone immediately know this is a "rollback
		// failed, intervene now" event vs a normal "rollback fired".
		if o.Notifier != nil {
			// Re-use Tier2 — the Notifier interface intentionally doesn't
			// expose PageAndRollback so callers can't fire the rollback
			// ringtone by accident. The "FAILED" prefix in the title gives
			// the operator the same signal.
			_ = o.Notifier.Tier2(ctx,
				"[grimnir] Auto-rollback FAILED",
				fmt.Sprintf("triggering_rule=%s reason=%s error=%s — operator MUST intervene",
					v.TriggeringRule, v.Reason, rollbackErr))
		}
		return fmt.Errorf("auto-rollback failed: %w", rollbackErr)
	}

	fmt.Fprintf(o.Out, "auto-rollback completed (rule=%s)\n", v.TriggeringRule)
	// Bubble the soak failure up to the caller so the deploy command
	// exits non-zero — the system is no longer running the requested
	// version, the caller's CI / operator workflow needs to know.
	return fmt.Errorf("soak failed (auto-rolled back): %s: %s", v.TriggeringRule, v.Reason)
}

// rollNode runs the per-node sequence: drain via VRRP failure file, stop
// services in dependency order, migrate on the first node only, pull + start
// the new image, wait for health, restore VRRP. Returns an error after a
// best-effort revert if the new image does not come up healthy.
func (o *DeployOpts) rollNode(ctx context.Context, host string, firstNode bool, prevTag string) error {
	if err := o.touchVRRPFail(ctx, host); err != nil {
		return fmt.Errorf("vrrp drain on %s: %w", host, err)
	}
	// Stop services in dependency order; outermost listeners first so the
	// inner pipeline can drain cleanly. Grace handled by docker stop's
	// default timeout (10s) — overridden globally via docker-compose.yml.
	for _, svc := range []string{"grimnir-radio", "edge-encoder", "grimnir-fanout", "grimnir-mediaengine"} {
		if err := o.Compose.Stop(ctx, host, svc); err != nil {
			return err
		}
	}
	if firstNode {
		if err := o.runMigrations(ctx, host); err != nil {
			return fmt.Errorf("migrations: %w", err)
		}
	}
	if err := o.Compose.Pull(ctx, host); err != nil {
		return err
	}
	if err := o.Compose.Up(ctx, host); err != nil {
		return err
	}
	if err := o.waitHealthy(ctx, host); err != nil {
		_ = o.revert(ctx, host, prevTag)
		return err
	}
	if err := o.removeVRRPFail(ctx, host); err != nil {
		return fmt.Errorf("vrrp restore on %s: %w", host, err)
	}
	return nil
}

// waitHealthy polls the HealthProbe at 2-second intervals up to
// HealthWaitTimeout. Returns nil on the first healthy reply.
func (o *DeployOpts) waitHealthy(ctx context.Context, host string) error {
	deadline := time.Now().Add(o.waitTimeout())
	for time.Now().Before(deadline) {
		if err := o.HealthProbe.Probe(ctx, host); err == nil {
			return nil
		}
		o.Sleep(2 * time.Second)
	}
	return fmt.Errorf("%s never came healthy within %v", host, o.waitTimeout())
}

// revert pulls the prior tag + restarts the stack. Idempotent.
func (o *DeployOpts) revert(ctx context.Context, host, prevTag string) error {
	_, _, _, err := o.Runner.Run(ctx, host,
		fmt.Sprintf("docker pull ghcr.io/friendsincode/grimnir-radio:%s", prevTag))
	if err != nil {
		return err
	}
	return o.Compose.Up(ctx, host)
}

// touchVRRPFail creates the keepalived vrrp_script failure marker so the
// node drops VIP priority + the peer takes over.
func (o *DeployOpts) touchVRRPFail(ctx context.Context, host string) error {
	_, _, _, err := o.Runner.Run(ctx, host, "touch /var/run/keepalived/vrrp_fail")
	return err
}

// removeVRRPFail removes the failure marker so VRRP priority returns to normal.
func (o *DeployOpts) removeVRRPFail(ctx context.Context, host string) error {
	_, _, _, err := o.Runner.Run(ctx, host, "rm -f /var/run/keepalived/vrrp_fail")
	return err
}

// runMigrations runs `grimnir migrate` from the new image against the primary.
// Only the first node runs migrations; the second node sees an already-migrated
// schema.
func (o *DeployOpts) runMigrations(ctx context.Context, host string) error {
	cmd := fmt.Sprintf("docker run --rm --network host -e GRIMNIR_DB_DSN ghcr.io/friendsincode/grimnir-radio:%s grimnirradio migrate", o.Tag)
	_, stderr, code, err := o.Runner.Run(ctx, host, cmd)
	if err != nil {
		return err
	}
	if code != 0 {
		return fmt.Errorf("migrate exit %d: %s", code, stderr)
	}
	return nil
}

// realDeployRunE is the cobra entry point. It wires Config + Deps + the
// runner + DockerCompose helper + the prober, then defers to runDeploy. The
// audit-wrapping happens inside runDeploy (via the Wrapper).
//
// The --rollback flag pivots to realRollbackRunE (Chunk 7); the rollback
// path has its own eligibility window + contract-migration refusal gates
// before it reuses the per-node rolling sequence.
func realDeployRunE(cmd *cobra.Command, args []string) error {
	rb, _ := cmd.Flags().GetBool("rollback")
	if rb {
		return realRollbackRunE(cmd, args)
	}
	if len(args) != 1 {
		return fmt.Errorf("usage: grimnir-deploy deploy <tag>")
	}
	tag := args[0]

	cfg, err := LoadConfig()
	if err != nil {
		return err
	}
	deps, err := wireDeps(cmd.Context(), cfg)
	if err != nil {
		return err
	}
	defer deps.Close()

	dryRun, _ := cmd.Flags().GetBool("dry-run")
	forcePolicy, _ := cmd.Flags().GetString("force-policy")
	goFlag, _ := cmd.Flags().GetBool("go")

	histStore := history.NewStore(deps.DB)
	prober := probe.NewProber()
	sshRunner := runner.NewSSHRunner(cfg.PeerSSHUser, cfg.PeerSSHPort, cfg.PeerSSHKey, nil)
	defer sshRunner.Close()
	compose := runner.NewDockerCompose(sshRunner, "/srv/docker/grimnir_radio")

	hosts := []string{"local"}
	firstHost, secondHost := "local", "local"
	if cfg.PeerHost != "" {
		hosts = append(hosts, cfg.PeerHost)
		firstHost = cfg.PeerHost // peer first (non-leader assumption); a follow-up probes the leader lease.
		secondHost = "local"
	}

	opts := DeployOpts{
		Tag:         tag,
		Cfg:         cfg,
		Hosts:       hosts,
		FirstHost:   firstHost,
		SecondHost:  secondHost,
		Runner:      sshRunner,
		Compose:     compose,
		HealthProbe: prober,
		Pause:       deps.Pause,
		History:     histStore,
		Wrapper:     deps.Wrapper,
		Out:         cmd.OutOrStdout(),
		DryRun:      dryRun,
		ForcePolicy: forcePolicy,
		GoFlag:      goFlag,
	}

	// Wire auto-rollback only when both the env flag is set & a Prometheus
	// URL is configured. A missing URL with the flag on is a soft fail: log
	// + fall back to passive sleep rather than refusing the deploy.
	if cfg.AutoRollbackEnabled && cfg.AutoRollbackPromURL != "" {
		obs, err := autorollback.NewMonitorObserver(
			cfg.AutoRollbackPromURL,
			cfg.SoakWindow,
			cfg.AutoRollbackTickInterval,
			autorollback.DefaultRules(),
		)
		if err != nil {
			fmt.Fprintf(cmd.OutOrStdout(),
				"WARN: auto-rollback disabled (could not build Prometheus client: %v); falling back to passive soak\n",
				err)
		} else {
			opts.SoakObserver = obs
			// The rollback closure builds RollbackOpts from the same deps
			// & invokes runRollback. Reason is propagated so the audit row
			// inside runRollback carries the original verdict text.
			opts.AutoRollback = func(ctx context.Context, reason string) error {
				return runRollback(ctx, RollbackOpts{
					Cfg:         cfg,
					Reason:      "auto-rollback: " + reason,
					ForceAged:   false, // never bypass safety gates from auto-flow
					Hosts:       hosts,
					FirstHost:   firstHost,
					SecondHost:  secondHost,
					Runner:      sshRunner,
					Compose:     compose,
					HealthProbe: prober,
					Pause:       deps.Pause,
					History:     histStore,
					Wrapper:     deps.Wrapper,
					Out:         cmd.OutOrStdout(),
				})
			}
			opts.Notifier = deps.NotifyClient
		}
	}

	return runDeploy(cmd.Context(), opts)
}
