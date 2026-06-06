/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/

// Package probe runs read-only health checks against a node's components.
// Used by gates.HealthGate (pass/fail) and by the verify subcommand
// (per-component report).
package probe

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	healthpb "google.golang.org/grpc/health/grpc_health_v1"
)

// Result is the per-component verdict for one host.
type Result struct {
	Host             string
	ControlPlaneOK   bool
	ControlPlaneErr  string
	MediaEngineOK    bool
	MediaEngineErr   string
	EdgeEncoderOK    bool
	EdgeEncoderErr   string
	FanOutOK         bool
	FanOutErr        string
	ReplicationLagS  float64
	ReplicationLagOK bool
}

// Prober runs probes for one host.
type Prober struct {
	HTTPClient       *http.Client
	GRPCDialTimeout  time.Duration
	ControlPlanePort int // default 8080
	MediaEnginePort  int // default 9091
	EdgeEncoderPort  int // default 8081
	FanOutPort       int // default 9000
}

// NewProber constructs a Prober with sensible defaults.
func NewProber() *Prober {
	return &Prober{
		HTTPClient:       &http.Client{Timeout: 5 * time.Second},
		GRPCDialTimeout:  3 * time.Second,
		ControlPlanePort: 8080,
		MediaEnginePort:  9091,
		EdgeEncoderPort:  8081,
		FanOutPort:       9000,
	}
}

// ProbeAll returns a populated Result for the host. Never returns an error;
// per-component failures surface as Result fields.
func (p *Prober) ProbeAll(ctx context.Context, host string) Result {
	r := Result{Host: host}
	if err := p.probeControlPlane(ctx, host); err != nil {
		r.ControlPlaneErr = err.Error()
	} else {
		r.ControlPlaneOK = true
	}
	if err := p.probeGRPCHealth(ctx, host, p.MediaEnginePort); err != nil {
		r.MediaEngineErr = err.Error()
	} else {
		r.MediaEngineOK = true
	}
	if err := p.probeGRPCHealth(ctx, host, p.EdgeEncoderPort); err != nil {
		r.EdgeEncoderErr = err.Error()
	} else {
		r.EdgeEncoderOK = true
	}
	if err := p.probeFanOut(ctx, host); err != nil {
		r.FanOutErr = err.Error()
	} else {
		r.FanOutOK = true
	}
	return r
}

// Probe satisfies gates.HealthProbe: returns the first per-component error.
func (p *Prober) Probe(ctx context.Context, host string) error {
	r := p.ProbeAll(ctx, host)
	switch {
	case !r.ControlPlaneOK:
		return fmt.Errorf("control plane: %s", r.ControlPlaneErr)
	case !r.MediaEngineOK:
		return fmt.Errorf("media engine: %s", r.MediaEngineErr)
	case !r.EdgeEncoderOK:
		return fmt.Errorf("edge encoder: %s", r.EdgeEncoderErr)
	case !r.FanOutOK:
		return fmt.Errorf("fan-out: %s", r.FanOutErr)
	}
	return nil
}

func (p *Prober) probeControlPlane(ctx context.Context, host string) error {
	url := fmt.Sprintf("http://%s:%d/healthz", host, p.ControlPlanePort)
	req, _ := http.NewRequestWithContext(ctx, "GET", url, nil)
	resp, err := p.HTTPClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return fmt.Errorf("status %d", resp.StatusCode)
	}
	return nil
}

func (p *Prober) probeGRPCHealth(ctx context.Context, host string, port int) error {
	dialCtx, cancel := context.WithTimeout(ctx, p.GRPCDialTimeout)
	defer cancel()
	conn, err := grpc.DialContext(dialCtx,
		fmt.Sprintf("%s:%d", host, port),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithBlock(),
	)
	if err != nil {
		return err
	}
	defer conn.Close()
	c := healthpb.NewHealthClient(conn)
	resp, err := c.Check(ctx, &healthpb.HealthCheckRequest{})
	if err != nil {
		return err
	}
	if resp.Status != healthpb.HealthCheckResponse_SERVING {
		return fmt.Errorf("status=%s", resp.Status)
	}
	return nil
}

func (p *Prober) probeFanOut(ctx context.Context, host string) error {
	url := fmt.Sprintf("http://%s:%d/healthz", host, p.FanOutPort)
	req, _ := http.NewRequestWithContext(ctx, "GET", url, nil)
	resp, err := p.HTTPClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return fmt.Errorf("status %d", resp.StatusCode)
	}
	return nil
}
