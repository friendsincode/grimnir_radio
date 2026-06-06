/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/

// alertmanager-ntfy is a thin HTTP bridge that converts Alertmanager v4
// webhook POSTs into ntfy notifications routed through internal/notify.
//
// It runs as a sidecar to Alertmanager. Configure each Alertmanager receiver
// to POST to http://localhost:9098/webhook; the bridge picks the ntfy topic
// (audit vs page) from the alert's `severity` label.
//
// Environment:
//
//	GRIMNIR_ALERTBRIDGE_ADDR  listen address (default ":9098")
//	GRIMNIR_NTFY_URL          ntfy server (e.g. https://ntfy.example.com)
//	GRIMNIR_NTFY_AUDIT_TOPIC  topic for tier-1 notifies (default "grimnir-audit")
//	GRIMNIR_NTFY_PAGE_TOPIC   topic for tier-2 pages (default "grimnir-page")
//	GRIMNIR_NTFY_AUDIT_TOKEN  bearer token for audit topic (optional)
//	GRIMNIR_NTFY_PAGE_TOKEN   bearer token for page topic (optional)
//
// When GRIMNIR_NTFY_URL is unset, the bridge logs a warning and accepts
// payloads as no-ops; useful for dev / CI where ntfy isn't reachable.
package main

import (
	"context"
	"errors"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/rs/zerolog/log"

	"github.com/friendsincode/grimnir_radio/internal/alertbridge"
	"github.com/friendsincode/grimnir_radio/internal/notify"
	"github.com/friendsincode/grimnir_radio/internal/version"
)

func main() {
	addr := os.Getenv("GRIMNIR_ALERTBRIDGE_ADDR")
	if addr == "" {
		addr = ":9098"
	}
	log.Info().Str("version", version.Version).Str("addr", addr).Msg("alertmanager-ntfy starting")

	n := notify.FromEnv()
	h := alertbridge.NewHandler(n)
	srv := &http.Server{
		Addr:              addr,
		Handler:           h,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      15 * time.Second,
	}

	idleConnsClosed := make(chan struct{})
	go func() {
		sigCh := make(chan os.Signal, 1)
		signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
		<-sigCh
		log.Info().Msg("alertmanager-ntfy shutting down")
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := srv.Shutdown(ctx); err != nil {
			log.Warn().Err(err).Msg("shutdown error")
		}
		close(idleConnsClosed)
	}()

	if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		log.Fatal().Err(err).Msg("listen failed")
	}
	<-idleConnsClosed
}
