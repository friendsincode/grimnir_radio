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
	"text/tabwriter"

	"github.com/spf13/cobra"

	"github.com/friendsincode/grimnir_radio/internal/grimnirdeploy/audit"
	"github.com/friendsincode/grimnir_radio/internal/grimnirdeploy/probe"
)

// FullProber is the surface verify needs (per-component report). The real
// implementation is *probe.Prober; tests supply a fake that returns a fixed
// Result so the formatting + pass/fail logic can be exercised without
// network calls.
type FullProber interface {
	ProbeAll(ctx context.Context, host string) probe.Result
}

// VerifyOpts is the option bag for runVerify. Tests build it directly so
// they exercise the same code path the cobra entry point takes, minus the
// env-var-driven dependency wiring.
type VerifyOpts struct {
	Hosts   []string
	Prober  FullProber
	Wrapper *audit.Wrapper
	Out     io.Writer
}

// runVerify probes every host concurrently, prints a per-component report
// table, and returns a non-nil error if any component on any host is
// unhealthy. The audit Wrapper records the invocation; verify itself never
// mutates cluster state, so the audit row is metadata only.
func runVerify(ctx context.Context, o VerifyOpts) error {
	return o.Wrapper.Wrap(ctx, "verify", nil, func(ctx context.Context) error {
		// Probe every host concurrently. The probe library bounds its own
		// HTTP + gRPC timeouts, so a slow host can't stall the report past
		// the configured per-probe deadline.
		results := make([]probe.Result, len(o.Hosts))
		done := make(chan struct{}, len(o.Hosts))
		for i, h := range o.Hosts {
			i, h := i, h
			go func() {
				results[i] = o.Prober.ProbeAll(ctx, h)
				done <- struct{}{}
			}()
		}
		for range o.Hosts {
			<-done
		}

		tw := tabwriter.NewWriter(o.Out, 2, 2, 2, ' ', 0)
		fmt.Fprintln(tw, "host\tcontrol plane\tmedia engine\tedge encoder\tfan-out")
		anyBad := false
		for _, r := range results {
			cp := okOr(r.ControlPlaneOK, r.ControlPlaneErr)
			me := okOr(r.MediaEngineOK, r.MediaEngineErr)
			ee := okOr(r.EdgeEncoderOK, r.EdgeEncoderErr)
			fo := okOr(r.FanOutOK, r.FanOutErr)
			fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\n", r.Host, cp, me, ee, fo)
			if !r.ControlPlaneOK || !r.MediaEngineOK || !r.EdgeEncoderOK || !r.FanOutOK {
				anyBad = true
			}
		}
		_ = tw.Flush()
		if anyBad {
			return errors.New("one or more components unhealthy")
		}
		return nil
	})
}

// okOr renders an OK / FAIL cell for the report.
func okOr(ok bool, errMsg string) string {
	if ok {
		return "OK"
	}
	if errMsg == "" {
		return "FAIL"
	}
	return "FAIL: " + errMsg
}

// realVerifyRunE is wired by newVerifyCmd in cmd_stubs.go.
func realVerifyRunE(cmd *cobra.Command, _ []string) error {
	cfg, err := LoadConfig()
	if err != nil {
		return err
	}
	deps, err := wireDeps(cmd.Context(), cfg)
	if err != nil {
		return err
	}
	defer deps.Close()
	hosts := []string{"local"}
	// PeerHost wiring lands with Chunk 6 (Config gains the field then). Until
	// then, accept a raw env var so operators can already point verify at the
	// other node without rebuilding the binary.
	if peer := os.Getenv("GRIMNIR_DEPLOY_PEER_HOST"); peer != "" {
		hosts = append(hosts, peer)
	}
	return runVerify(cmd.Context(), VerifyOpts{
		Hosts:   hosts,
		Prober:  probe.NewProber(),
		Wrapper: deps.Wrapper,
		Out:     cmd.OutOrStdout(),
	})
}
