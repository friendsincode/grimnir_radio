/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/

// Command grimnir-fanout accepts a single DJ connection over Harbor TCP,
// raw RTP, SRT, or WebRTC & duplicates the audio as PCM-over-RTP toward N
// media engines, keeping the lockstep executor alive across engine failover
// during live broadcasts.
//
// See internal/grimnirfanout for the per-component documentation and
// docs/superpowers/plans/2026-06-05-live-input-fan-out.md for the
// implementation plan. This file wires the scaffold (Chunk 1): config,
// gRPC server, /healthz, /metrics, signal-driven shutdown. Protocols arrive
// in Chunks 3-6.
package main

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/signal"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/friendsincode/grimnir_radio/internal/grimnirfanout"
	"github.com/friendsincode/grimnir_radio/internal/metrics"
	pb "github.com/friendsincode/grimnir_radio/proto/grimnirfanout/v1"
	"google.golang.org/grpc"
)

// Version is set at build time via ldflags; mirrors the other binaries.
var Version = "dev"

func main() {
	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()
	os.Exit(run(ctx, os.Stdout, os.Stderr))
}

func run(ctx context.Context, stdout, stderr io.Writer) int {
	cfg, err := grimnirfanout.LoadConfigFromEnv()
	if err != nil {
		fmt.Fprintf(stderr, "grimnir-fanout: config error: %v\n", err)
		return 2
	}
	fmt.Fprintf(stdout,
		"grimnir-fanout starting; version=%s grpc_port=%d http_port=%d metrics_port=%d harbor=%d rtp=%d srt=%d webrtc=%d engine_a=%s engine_b=%s\n",
		Version, cfg.GRPCPort, cfg.HTTPPort, cfg.MetricsPort,
		cfg.HarborPort, cfg.RTPPort, cfg.SRTPort, cfg.WebRTCHTTPPort,
		cfg.EngineARTP, cfg.EngineBRTP,
	)

	startTime := time.Now()
	statusProvider := &scaffoldStatus{startTime: startTime}

	// gRPC server (control-plane queries; engine health, session list).
	grpcLis, err := net.Listen("tcp", fmt.Sprintf("%s:%d", cfg.BindAddr, cfg.GRPCPort))
	if err != nil {
		fmt.Fprintf(stderr, "grimnir-fanout: grpc listen: %v\n", err)
		return 2
	}
	grpcServer := grpc.NewServer()
	pb.RegisterGrimnirFanoutServer(grpcServer, grimnirfanout.NewGRPCServer(statusProvider))
	go func() { _ = grpcServer.Serve(grpcLis) }()
	defer grpcServer.GracefulStop()

	// HTTP server: /healthz + /metrics on the same mux.
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		fmt.Fprintln(w, "ok")
	})
	httpSrv := &http.Server{
		Addr:              fmt.Sprintf("%s:%d", cfg.BindAddr, cfg.HTTPPort),
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
	}
	go func() { _ = httpSrv.ListenAndServe() }()
	defer func() { _ = httpSrv.Shutdown(context.Background()) }()

	// Separate Prometheus listener so /metrics scrape isn't behind the same
	// rate limits as /healthz; matches the edge-encoder convention.
	metricsMux := http.NewServeMux()
	metricsMux.Handle("/metrics", metrics.Handler(metrics.FanoutRegistry))
	metricsSrv := &http.Server{
		Addr:              fmt.Sprintf("%s:%d", cfg.BindAddr, cfg.MetricsPort),
		Handler:           metricsMux,
		ReadHeaderTimeout: 5 * time.Second,
	}
	go func() { _ = metricsSrv.ListenAndServe() }()
	defer func() { _ = metricsSrv.Shutdown(context.Background()) }()

	<-ctx.Done()
	fmt.Fprintln(stdout, "grimnir-fanout shutting down")
	// Drain placeholder: Chunk 2 wires the session manager & this is where
	// in-flight sessions get drained before grpcServer.GracefulStop runs via
	// defer. For the scaffold there is nothing to drain.
	return 0
}

// scaffoldStatus is the Chunk 1 placeholder StatusProvider. Chunk 2 replaces
// it with the real session-manager-backed provider. It only reports uptime so
// the gRPC GetStatus call returns something non-zero & observable.
type scaffoldStatus struct {
	startTime           time.Time
	totalSessionsServed atomic.Int64
}

func (s *scaffoldStatus) Status() grimnirfanout.Status {
	return grimnirfanout.Status{
		Version:             Version,
		UptimeSeconds:       int64(time.Since(s.startTime).Seconds()),
		TotalSessionsServed: s.totalSessionsServed.Load(),
	}
}
