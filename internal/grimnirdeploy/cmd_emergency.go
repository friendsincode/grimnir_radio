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
	"github.com/friendsincode/grimnir_radio/internal/grimnirdeploy/pause"
)

// runEmergencyOpts is the option bag for runEmergencyPause / runEmergencyResume.
// Tests build it directly so they exercise the same code path the cobra
// entry points take, minus the env-var-driven dependency wiring.
type runEmergencyOpts struct {
	Region  string
	Reason  string
	DryRun  bool
	TTL     time.Duration
	Pause   *pause.Client
	Wrapper *audit.Wrapper
	Out     io.Writer
}

// runEmergencyPause is the body of the emergency-pause subcommand. Audit
// bookkeeping (START / COMPLETE / FAILED rows + ntfy) is handled by the
// audit.WrapCobra middleware in RegisterCommands; this function only does
// the Redis mutation and operator output.
//
// The Wrapper in opts is consulted for the bound operator string (stamped
// into the Redis payload) but never for Wrap; double-wrapping would write
// two audit rows per invocation.
func runEmergencyPause(ctx context.Context, o runEmergencyOpts) error {
	prior, err := o.Pause.Read(ctx, o.Region)
	if err != nil {
		return fmt.Errorf("read prior pause state: %w", err)
	}
	if prior != nil {
		fmt.Fprintf(o.Out, "pause already set by %s at %s (reason: %s); overwriting\n",
			prior.Operator, prior.TS.Format(time.RFC3339), prior.Reason)
	}
	if o.DryRun {
		fmt.Fprintf(o.Out, "[dry-run] would set %s reason=%q region=%s\n",
			pause.KeyFor(o.Region), o.Reason, o.Region)
		return nil
	}
	operator := "unknown"
	if o.Wrapper != nil && o.Wrapper.Operator() != "" {
		operator = o.Wrapper.Operator()
	}
	if err := o.Pause.Set(ctx, o.Region, o.Reason, operator, o.TTL); err != nil {
		return fmt.Errorf("set pause: %w", err)
	}
	fmt.Fprintf(o.Out, "emergency-pause SET region=%s reason=%q operator=%s\n",
		o.Region, o.Reason, operator)
	return nil
}

// runEmergencyResume is the body of the emergency-resume subcommand. See
// runEmergencyPause for the audit-wrapping note.
func runEmergencyResume(ctx context.Context, o runEmergencyOpts) error {
	prior, err := o.Pause.Read(ctx, o.Region)
	if err != nil {
		return fmt.Errorf("read prior pause state: %w", err)
	}
	if prior == nil {
		fmt.Fprintln(o.Out, "no pause was set; nothing to clear")
		return nil
	}
	if o.DryRun {
		fmt.Fprintf(o.Out, "[dry-run] would clear %s (was: %s by %s)\n",
			pause.KeyFor(o.Region), prior.Reason, prior.Operator)
		return nil
	}
	if err := o.Pause.Clear(ctx, o.Region); err != nil {
		return fmt.Errorf("clear pause: %w", err)
	}
	fmt.Fprintf(o.Out, "emergency-pause CLEARED region=%s (was: %s by %s; resume reason: %s)\n",
		o.Region, prior.Reason, prior.Operator, o.Reason)
	return nil
}

// realEmergencyPauseRunE is wired by newEmergencyPauseCmd in cmd_stubs.go.
// It builds the runtime Wrapper inside the call (rather than relying on the
// nil Wrapper passed to RegisterCommands at init), so the START / COMPLETE
// / FAILED audit rows fire against the real audit_log table when a DSN is
// configured and against a no-op recorder otherwise.
func realEmergencyPauseRunE(cmd *cobra.Command, _ []string) error {
	cfg, err := LoadConfig()
	if err != nil {
		return err
	}
	region, _ := cmd.Flags().GetString("region")
	if region == "" {
		region = cfg.Region
	}
	reason, _ := cmd.Flags().GetString("reason")
	dryRun, _ := cmd.Flags().GetBool("dry-run")
	ttl, _ := cmd.Flags().GetDuration("ttl")

	deps, err := wireDeps(cmd.Context(), cfg)
	if err != nil {
		return err
	}
	defer deps.Close()

	return deps.Wrapper.Wrap(cmd.Context(), "emergency-pause", map[string]any{
		"region": region, "reason": reason, "dry_run": dryRun, "ttl": ttl.String(),
	}, func(ctx context.Context) error {
		return runEmergencyPause(ctx, runEmergencyOpts{
			Region: region, Reason: reason, DryRun: dryRun, TTL: ttl,
			Pause: deps.Pause, Wrapper: deps.Wrapper, Out: cmd.OutOrStdout(),
		})
	})
}

// realEmergencyResumeRunE is wired by newEmergencyResumeCmd in cmd_stubs.go.
func realEmergencyResumeRunE(cmd *cobra.Command, _ []string) error {
	cfg, err := LoadConfig()
	if err != nil {
		return err
	}
	region, _ := cmd.Flags().GetString("region")
	if region == "" {
		region = cfg.Region
	}
	reason, _ := cmd.Flags().GetString("reason")
	dryRun, _ := cmd.Flags().GetBool("dry-run")

	deps, err := wireDeps(cmd.Context(), cfg)
	if err != nil {
		return err
	}
	defer deps.Close()

	return deps.Wrapper.Wrap(cmd.Context(), "emergency-resume", map[string]any{
		"region": region, "reason": reason, "dry_run": dryRun,
	}, func(ctx context.Context) error {
		return runEmergencyResume(ctx, runEmergencyOpts{
			Region: region, Reason: reason, DryRun: dryRun,
			Pause: deps.Pause, Wrapper: deps.Wrapper, Out: cmd.OutOrStdout(),
		})
	})
}
