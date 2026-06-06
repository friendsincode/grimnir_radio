/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/

// Package metrics provides per-binary Prometheus registries for the HA stack.
//
// This is intentionally separate from internal/telemetry, which uses the
// global default registry for cross-binary shared metrics (scheduler,
// executor, playout). The HA metrics defined here are per-binary so that
// edge-encoder and grimnir-fanout don't have to import scheduler-only
// definitions, and so tests stay isolated.
package metrics

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/collectors"
)

// Registry wraps prometheus.Registry with a human-readable name for diagnostics.
type Registry struct {
	*prometheus.Registry
	Name string
}

// NewRegistry creates an isolated registry pre-loaded with go-runtime and
// process collectors.
func NewRegistry(name string) *Registry {
	r := prometheus.NewRegistry()
	r.MustRegister(collectors.NewGoCollector())
	r.MustRegister(collectors.NewProcessCollector(collectors.ProcessCollectorOpts{}))
	return &Registry{Registry: r, Name: name}
}

// Per-binary registries. Each Go binary picks the one it owns at init time
// and registers HA-specific metrics into it.
var (
	GrimnirRadioRegistry = NewRegistry("grimnirradio")
	MediaEngineRegistry  = NewRegistry("mediaengine")
	EdgeEncoderRegistry  = NewRegistry("edge-encoder")
	FanoutRegistry       = NewRegistry("grimnir-fanout")
	DeployRegistry       = NewRegistry("grimnir-deploy")
)
