/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/

// Command edge-encoder ingests PCM-over-RTP from media engines, performs
// sample-aligned input switching when the active source goes unhealthy, and
// serves the encoded result to HTTP/ICY and HLS listeners.
//
// See internal/edgeencoder for the per-component documentation and
// docs/superpowers/plans/2026-06-03-edge-encoder-pcm-transport.md for the
// implementation plan.
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

	"github.com/friendsincode/grimnir_radio/internal/broadcast"
	"github.com/friendsincode/grimnir_radio/internal/edgeencoder"
	pb "github.com/friendsincode/grimnir_radio/proto/edgeencoder/v1"
	"github.com/rs/zerolog"
	"google.golang.org/grpc"
)

// Version is set at build time via ldflags; mirrors mediaengine convention.
var Version = "dev"

func main() {
	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()
	os.Exit(run(ctx, os.Stdout, os.Stderr))
}

func run(ctx context.Context, stdout, stderr io.Writer) int {
	cfg, err := edgeencoder.LoadConfigFromEnv()
	if err != nil {
		fmt.Fprintf(stderr, "edge-encoder: config error: %v\n", err)
		return 2
	}
	fmt.Fprintf(stdout, "edge-encoder starting; version=%s grpc_port=%d http_port=%d rtp_ports=%d,%d\n",
		Version, cfg.GRPCPort, cfg.HTTPPort, cfg.RTPPortA, cfg.RTPPortB)

	// Initialize GStreamer before any element construction.
	edgeencoder.Init()

	// Build the pipeline (udpsrc x2 -> input-selector -> tee -> mp3 appsink).
	pipeline, err := edgeencoder.NewPipeline(cfg)
	if err != nil {
		fmt.Fprintf(stderr, "edge-encoder: pipeline construction failed: %v\n", err)
		return 2
	}
	defer func() { _ = pipeline.Close() }()

	// Per-input health trackers. 100ms window matches the design's
	// "switch within 100ms of source loss" target.
	healthA := edgeencoder.NewInputHealth(100 * time.Millisecond)
	healthB := edgeencoder.NewInputHealth(100 * time.Millisecond)
	pipeline.AttachHealthProbes(healthA, healthB)

	// Start the pipeline (async transition to PLAYING).
	if err := pipeline.Start(); err != nil {
		fmt.Fprintf(stderr, "edge-encoder: pipeline start failed: %v\n", err)
		return 2
	}

	// Switcher: 50ms tick, hysteresis 2 (= 100ms confirmation window).
	switcher := edgeencoder.NewSwitcher(healthA, healthB, pipeline, 50*time.Millisecond, 2)
	go switcher.Run(ctx)

	// Listener-facing broadcast mount: appsink bytes -> Mount -> HTTP /live.
	// Bus is nil; this binary has no event bus, & Mount tolerates that.
	contentType := "audio/mpeg"
	if cfg.OutputFormat == "aac" {
		contentType = "audio/aac"
	}
	mount := broadcast.NewMount("live", contentType, cfg.OutputBitrateKbps, zerolog.Nop(), nil)
	reader := edgeencoder.NewAppsinkReader(pipeline.MP3Appsink())
	go func() {
		if err := mount.FeedFrom(reader); err != nil && err != io.EOF {
			fmt.Fprintf(stderr, "edge-encoder: mount feed exited: %v\n", err)
		}
	}()
	defer func() { _ = reader.Close(); mount.Close() }()

	// Live status provider for the gRPC service.
	startTime := time.Now()
	statusProvider := &liveStatus{
		pipeline:  pipeline,
		a:         healthA,
		b:         healthB,
		switcher:  switcher,
		mount:     mount,
		startTime: startTime,
	}

	// gRPC server
	grpcLis, err := net.Listen("tcp", fmt.Sprintf("%s:%d", cfg.BindAddr, cfg.GRPCPort))
	if err != nil {
		fmt.Fprintf(stderr, "edge-encoder: grpc listen: %v\n", err)
		return 2
	}
	grpcServer := grpc.NewServer()
	pb.RegisterEdgeEncoderServer(grpcServer, edgeencoder.NewGRPCServer(statusProvider))
	go func() { _ = grpcServer.Serve(grpcLis) }()
	defer grpcServer.GracefulStop()

	// /healthz HTTP endpoint
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		fmt.Fprintln(w, "ok")
	})
	mux.Handle("/live", mount)
	httpSrv := &http.Server{
		Addr:              fmt.Sprintf("%s:%d", cfg.BindAddr, cfg.HTTPPort),
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
	}
	go func() { _ = httpSrv.ListenAndServe() }()
	defer func() { _ = httpSrv.Shutdown(context.Background()) }()

	<-ctx.Done()
	fmt.Fprintln(stdout, "edge-encoder shutting down")
	return 0
}

type liveStatus struct {
	pipeline  *edgeencoder.Pipeline
	a, b      *edgeencoder.InputHealth
	switcher  *edgeencoder.Switcher
	mount     *broadcast.Mount
	startTime time.Time
}

func (s *liveStatus) Status() edgeencoder.Status {
	var listeners int64
	if s.mount != nil {
		listeners = int64(s.mount.ClientCount())
	}
	return edgeencoder.Status{
		Version:       Version,
		UptimeSeconds: int64(time.Since(s.startTime).Seconds()),
		ActiveInput:   s.pipeline.ActiveInput(),
		InputAHealthy: s.a.IsHealthy(),
		InputBHealthy: s.b.IsHealthy(),
		SwitchCount:   s.switcher.SwitchCount(),
		ListenerCount: listeners,
	}
}
