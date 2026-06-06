/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/

package autorollback

import (
	"context"
	"os"
	"strings"
	"time"
)

// Observer is the surface cmd_deploy depends on for the soak phase. The
// default implementation (Monitor) polls Prometheus; tests can pass a
// canned Observer that returns a fixed Verdict so the deploy-orchestration
// tests don't need a real Prometheus.
type Observer interface {
	// Observe blocks until the soak window completes, the context cancels,
	// or a rule triggers Rollback. MUST return a non-zero-Decision Verdict.
	Observe(ctx context.Context) Verdict
}

// EnabledFromEnv reports whether auto-rollback is enabled for this binary
// invocation. Defaults true; the operator opts out by setting
// GRIMNIR_DEPLOY_AUTOROLLBACK_ENABLED=false (or 0 / no / off). Tests pass
// "false" to keep the deploy-orchestration tests free of Prometheus
// dependencies.
func EnabledFromEnv() bool {
	v := strings.ToLower(strings.TrimSpace(os.Getenv("GRIMNIR_DEPLOY_AUTOROLLBACK_ENABLED")))
	switch v {
	case "false", "0", "no", "off":
		return false
	}
	return true
}

// NewMonitorObserver builds a production Observer that polls Prometheus
// over the configured soak window. Returns nil + error if the Prometheus
// URL is unreachable or malformed; the caller can fall back to passive
// sleep in that case rather than failing the whole deploy.
func NewMonitorObserver(promURL string, window, tick time.Duration, rules []Rule) (Observer, error) {
	q, err := NewPromQuerier(promURL)
	if err != nil {
		return nil, err
	}
	if len(rules) == 0 {
		rules = DefaultRules()
	}
	if tick <= 0 {
		tick = 15 * time.Second
	}
	return &Monitor{
		Querier:      q,
		Rules:        rules,
		Window:       window,
		TickInterval: tick,
	}, nil
}
