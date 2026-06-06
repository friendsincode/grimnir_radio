/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/

package grimnirfanout

import (
	"testing"
)

func TestConfig_Defaults(t *testing.T) {
	for _, k := range []string{
		"FANOUT_BIND_ADDR", "FANOUT_GRPC_PORT",
		"FANOUT_HTTP_PORT", "FANOUT_METRICS_PORT",
		"FANOUT_HARBOR_PORT", "FANOUT_RTP_PORT",
		"FANOUT_SRT_PORT", "FANOUT_WEBRTC_HTTP_PORT",
		"FANOUT_NETCLOCK_ENABLED", "FANOUT_NETCLOCK_MASTER_ADDR",
		"FANOUT_CONTROL_PLANE_GRPC", "FANOUT_REDIS_ADDR",
		"FANOUT_LOG_LEVEL",
	} {
		t.Setenv(k, "")
	}
	t.Setenv("FANOUT_ENGINE_A_RTP", "10.0.0.1:5004") // required field

	c, err := LoadConfigFromEnv()
	if err != nil {
		t.Fatal(err)
	}
	if c.BindAddr != "0.0.0.0" {
		t.Errorf("BindAddr = %q, want 0.0.0.0", c.BindAddr)
	}
	if c.GRPCPort != 9093 {
		t.Errorf("GRPCPort = %d, want 9093", c.GRPCPort)
	}
	if c.HTTPPort != 8003 {
		t.Errorf("HTTPPort = %d, want 8003", c.HTTPPort)
	}
	if c.MetricsPort != 9193 {
		t.Errorf("MetricsPort = %d, want 9193", c.MetricsPort)
	}
	if c.HarborPort != 8000 {
		t.Errorf("HarborPort = %d, want 8000", c.HarborPort)
	}
	if c.RTPPort != 5006 {
		t.Errorf("RTPPort = %d, want 5006", c.RTPPort)
	}
	if c.SRTPort != 1935 {
		t.Errorf("SRTPort = %d, want 1935", c.SRTPort)
	}
	if c.WebRTCHTTPPort != 8004 {
		t.Errorf("WebRTCHTTPPort = %d, want 8004", c.WebRTCHTTPPort)
	}
	if c.EngineARTP != "10.0.0.1:5004" {
		t.Errorf("EngineARTP = %q, want 10.0.0.1:5004", c.EngineARTP)
	}
	if c.EngineBRTP != "" {
		t.Errorf("EngineBRTP = %q, want empty", c.EngineBRTP)
	}
	if c.NetClockEnabled {
		t.Error("NetClockEnabled = true, want false")
	}
	if c.LogLevel != "info" {
		t.Errorf("LogLevel = %q, want info", c.LogLevel)
	}
}

func TestConfig_OverridesViaEnv(t *testing.T) {
	t.Setenv("FANOUT_ENGINE_A_RTP", "10.0.0.1:5004")
	t.Setenv("FANOUT_ENGINE_B_RTP", "10.0.0.2:5004")
	t.Setenv("FANOUT_GRPC_PORT", "19093")
	t.Setenv("FANOUT_NETCLOCK_ENABLED", "true")
	t.Setenv("FANOUT_NETCLOCK_MASTER_ADDR", "clock.example:9094")
	t.Setenv("FANOUT_CONTROL_PLANE_GRPC", "ctrl.example:9090")
	t.Setenv("FANOUT_REDIS_ADDR", "redis.example:6379")

	c, err := LoadConfigFromEnv()
	if err != nil {
		t.Fatal(err)
	}
	if c.GRPCPort != 19093 {
		t.Errorf("GRPCPort = %d, want 19093", c.GRPCPort)
	}
	if c.EngineBRTP != "10.0.0.2:5004" {
		t.Errorf("EngineBRTP = %q, want 10.0.0.2:5004", c.EngineBRTP)
	}
	if !c.NetClockEnabled {
		t.Error("NetClockEnabled = false, want true")
	}
	if c.NetClockMasterAddr != "clock.example:9094" {
		t.Errorf("NetClockMasterAddr = %q, want clock.example:9094", c.NetClockMasterAddr)
	}
	if c.ControlPlaneGRPC != "ctrl.example:9090" {
		t.Errorf("ControlPlaneGRPC = %q, want ctrl.example:9090", c.ControlPlaneGRPC)
	}
	if c.RedisAddr != "redis.example:6379" {
		t.Errorf("RedisAddr = %q, want redis.example:6379", c.RedisAddr)
	}
}

func TestConfig_EngineARTPRequired(t *testing.T) {
	t.Setenv("FANOUT_ENGINE_A_RTP", "")
	_, err := LoadConfigFromEnv()
	if err == nil {
		t.Error("LoadConfigFromEnv with empty FANOUT_ENGINE_A_RTP: want error, got nil")
	}
}

func TestConfig_RLMFallback(t *testing.T) {
	// Empty FANOUT_*, set RLM_FANOUT_* fallback; values come from fallback.
	t.Setenv("FANOUT_ENGINE_A_RTP", "")
	t.Setenv("FANOUT_GRPC_PORT", "")
	t.Setenv("RLM_FANOUT_ENGINE_A_RTP", "10.0.0.99:5004")
	t.Setenv("RLM_FANOUT_GRPC_PORT", "29093")

	c, err := LoadConfigFromEnv()
	if err != nil {
		t.Fatal(err)
	}
	if c.EngineARTP != "10.0.0.99:5004" {
		t.Errorf("EngineARTP = %q, want 10.0.0.99:5004 (from RLM_FANOUT_*)", c.EngineARTP)
	}
	if c.GRPCPort != 29093 {
		t.Errorf("GRPCPort = %d, want 29093 (from RLM_FANOUT_*)", c.GRPCPort)
	}
}
