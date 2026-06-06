/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/

// Package alertbridge converts Alertmanager v4 webhook POSTs into ntfy POSTs
// via the internal/notify.Notifier interface.
//
// Alertmanager doesn't speak ntfy natively, and the ntfy server's built-in
// webhook compatibility shim drops label context (alertname, vip, instance)
// that operators need to triage a page. The bridge runs as a small sidecar
// that Alertmanager posts to over loopback; it routes each alert by its
// `severity` label and re-emits with a formatted title and body.
//
// Routing table:
//
//	severity=notify              → Notifier.Tier1   (audit topic, priority 3)
//	severity=page                → Notifier.Tier2   (page topic, priority 5)
//	severity=page-and-rollback   → Notifier.Tier2   (page topic; Chunk 8's
//	                               grimnir-deploy webhook handles the
//	                               actual rollback separately so a bridge
//	                               outage doesn't silently disable it)
//	any other / missing          → Notifier.Tier2   (defensive default;
//	                               loud failure mode preferred over silent)
package alertbridge

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/rs/zerolog/log"

	"github.com/friendsincode/grimnir_radio/internal/notify"
)

// alertmanagerPayload mirrors the subset of Alertmanager's webhook payload the
// bridge actually reads. Defining it locally avoids a hard dependency on the
// alertmanager module and pins us to the v4 schema documented at
// https://prometheus.io/docs/alerting/latest/configuration/#webhook_config.
type alertmanagerPayload struct {
	Version  string             `json:"version"`
	Status   string             `json:"status"`
	Receiver string             `json:"receiver"`
	Alerts   []alertmanagerData `json:"alerts"`
}

type alertmanagerData struct {
	Status      string            `json:"status"`
	Labels      map[string]string `json:"labels"`
	Annotations map[string]string `json:"annotations"`
	StartsAt    string            `json:"startsAt"`
	EndsAt      string            `json:"endsAt"`
}

// Handler is an http.Handler that routes Alertmanager POSTs through a
// Notifier. Construct via NewHandler; the zero value is unusable.
type Handler struct {
	n   notify.Notifier
	mux *http.ServeMux
}

// NewHandler builds a Handler with /webhook (POST) and /healthz (GET) routes
// already wired up.
func NewHandler(n notify.Notifier) *Handler {
	h := &Handler{n: n, mux: http.NewServeMux()}
	h.mux.HandleFunc("/webhook", h.handleWebhook)
	h.mux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})
	return h
}

// ServeHTTP implements http.Handler.
func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) { h.mux.ServeHTTP(w, r) }

func (h *Handler) handleWebhook(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var p alertmanagerPayload
	if err := json.NewDecoder(r.Body).Decode(&p); err != nil {
		http.Error(w, "bad payload", http.StatusBadRequest)
		return
	}
	ctx := r.Context()
	for _, a := range p.Alerts {
		if err := h.dispatch(ctx, a); err != nil {
			// Log but don't return a 5xx: Alertmanager would retry the
			// whole batch, re-paging the operator for the alerts that
			// already went through. ntfy delivery is best-effort.
			log.Warn().Err(err).
				Str("alertname", a.Labels["alertname"]).
				Str("severity", a.Labels["severity"]).
				Msg("alertbridge: notify failed")
		}
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) dispatch(ctx context.Context, a alertmanagerData) error {
	title, body := formatAlert(a)
	switch a.Labels["severity"] {
	case "notify":
		return h.n.Tier1(ctx, title, body)
	case "page", "page-and-rollback":
		return h.n.Tier2(ctx, title, body)
	default:
		// Unknown severity: page the operator. Silent drops are worse
		// than a spurious alert.
		return h.n.Tier2(ctx, title, body)
	}
}

// formatAlert turns a single Alertmanager alert into a ntfy-friendly
// (title, body) pair. The title carries the alertname and any [RESOLVED]
// prefix; the body is the human-readable summary and description annotations
// plus the distinctive labels (vip, instance, etc.) that triage usually
// needs.
func formatAlert(a alertmanagerData) (string, string) {
	name := a.Labels["alertname"]
	if name == "" {
		name = "UnnamedAlert"
	}
	title := name
	if a.Status == "resolved" {
		title = "[RESOLVED] " + name
	}
	var b strings.Builder
	if s := a.Annotations["summary"]; s != "" {
		b.WriteString(s)
		b.WriteString("\n")
	}
	if d := a.Annotations["description"]; d != "" {
		b.WriteString(d)
		b.WriteString("\n")
	}
	// Append distinguishing labels (skip severity + alertname; already
	// surfaced via routing & title). Sorted keys would be nicer for
	// readability but iteration order doesn't matter for paging.
	first := true
	for k, v := range a.Labels {
		if k == "severity" || k == "alertname" {
			continue
		}
		if first {
			b.WriteString("\n")
			first = false
		}
		fmt.Fprintf(&b, "%s=%s ", k, v)
	}
	if r := a.Annotations["runbook"]; r != "" {
		b.WriteString("\n")
		b.WriteString("runbook: " + r)
	}
	return title, strings.TrimRight(b.String(), " \n")
}
