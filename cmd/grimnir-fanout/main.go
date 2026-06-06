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
	sessionMgr := grimnirfanout.NewSessionMgr()
	statusProvider := grimnirfanout.NewSessionMgrStatusProvider(
		Version,
		sessionMgr,
		func() time.Duration { return time.Since(startTime) },
	)

	// Engine targets for every per-session multiudpsink fanout.
	engines := []string{cfg.EngineARTP}
	if cfg.EngineBRTP != "" {
		engines = append(engines, cfg.EngineBRTP)
	}

	// Harbor (Icecast SOURCE/PUT) TCP listener — Chunk 3 wire line.
	harborLis, err := net.Listen("tcp", fmt.Sprintf("%s:%d", cfg.BindAddr, cfg.HarborPort))
	if err != nil {
		fmt.Fprintf(stderr, "grimnir-fanout: harbor listen: %v\n", err)
		return 2
	}
	harbor := grimnirfanout.NewHarborListener(grimnirfanout.HarborListenerConfig{
		Listener: harborLis,
		Auth:     grimnirfanout.AcceptAllAuthenticator{}, // Chunk 7 replaces with real auth
		Sink:     grimnirfanout.NewPipelineHarborSink(engines),
		Sessions: sessionMgr,
	})
	harborCtx, harborCancel := context.WithCancel(ctx)
	defer harborCancel()
	go func() { _ = harbor.Serve(harborCtx) }()

	// SRT (Secure Reliable Transport) ingress — Chunk 5 wire line.
	srtLis, err := grimnirfanout.NewSRTListener(grimnirfanout.SRTListenerConfig{
		BindAddr: cfg.BindAddr,
		Port:     cfg.SRTPort,
		Engines:  engines,
		Sessions: sessionMgr,
	})
	if err != nil {
		fmt.Fprintf(stderr, "grimnir-fanout: srt listener: %v\n", err)
		return 2
	}
	srtCtx, srtCancel := context.WithCancel(ctx)
	defer srtCancel()
	go func() { _ = srtLis.Serve(srtCtx) }()

	// WebRTC (browser WebDJ) signaling + ingest — Chunk 6 wire line.
	webrtcIng, err := grimnirfanout.NewWebRTCIngress(grimnirfanout.WebRTCIngressConfig{
		BindAddr:   cfg.BindAddr,
		Port:       cfg.WebRTCHTTPPort,
		Engines:    engines,
		SessionMgr: sessionMgr,
	})
	if err != nil {
		fmt.Fprintf(stderr, "grimnir-fanout: webrtc ingress: %v\n", err)
		return 2
	}
	go func() { _ = webrtcIng.ListenAndServe() }()
	defer func() { _ = webrtcIng.Shutdown(context.Background()) }()

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
	// Drain: stop every live session so protocol terminators (Chunks 3-6)
	// release the pipeline + close the network conn before grpcServer's
	// GracefulStop runs via defer.
	for _, s := range sessionMgr.List() {
		if s.Pipeline != nil {
			_ = s.Pipeline.Stop()
		}
		sessionMgr.Remove(s.ID)
	}
	return 0
}
