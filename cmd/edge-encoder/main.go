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

	"github.com/friendsincode/grimnir_radio/internal/edgeencoder"
	pb "github.com/friendsincode/grimnir_radio/proto/edgeencoder/v1"
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

	// Stub status provider; replaced by real pipeline integration in Chunk 5.
	startTime := time.Now()
	statusProvider := &stubStatus{startTime: startTime}

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

type stubStatus struct {
	startTime time.Time
}

func (s *stubStatus) Status() edgeencoder.Status {
	return edgeencoder.Status{
		Version:       Version,
		UptimeSeconds: int64(time.Since(s.startTime).Seconds()),
		ActiveInput:   "none",
	}
}
