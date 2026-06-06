/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/

// Package grimnirfanout contains the live-input fan-out service. It accepts a
// single DJ connection over one of several protocols (Harbor TCP, raw RTP,
// SRT, WebRTC) & duplicates the audio as PCM-over-RTP toward N media engines
// so the lockstep executor survives an engine failover mid-broadcast.
//
// This file only covers configuration loading. See the chunks 2-6 of
// docs/superpowers/plans/2026-06-05-live-input-fan-out.md for the rest.
package grimnirfanout

import (
	"fmt"
	"os"
	"strconv"
	"strings"
)

// Config holds runtime configuration for the fan-out binary. Loaded from
// environment variables; see LoadConfigFromEnv for the names. Variables are
// looked up under the FANOUT_* namespace first, then the RLM_FANOUT_* legacy
// namespace as a fallback, matching the convention used elsewhere in the
// repo.
type Config struct {
	BindAddr       string
	GRPCPort       int
	HTTPPort       int
	MetricsPort    int
	HarborPort     int
	RTPPort        int
	SRTPort        int
	WebRTCHTTPPort int

	// EngineARTP is the host:port of media engine A's PCM RTP ingress.
	// Required; an empty value fails LoadConfigFromEnv.
	EngineARTP string
	// EngineBRTP is the host:port of media engine B's ingress. Empty means
	// single-engine deployment (no fan-out duplication target on side B).
	EngineBRTP string

	NetClockEnabled    bool
	NetClockMasterAddr string

	ControlPlaneGRPC string
	RedisAddr        string

	LogLevel string
}

// LoadConfigFromEnv reads Config from the environment, applying defaults &
// the RLM_FANOUT_* fallback. Returns an error when a required field is unset.
func LoadConfigFromEnv() (*Config, error) {
	c := &Config{
		BindAddr:           getEnvOr("BIND_ADDR", "0.0.0.0"),
		GRPCPort:           getEnvIntOr("GRPC_PORT", 9093),
		HTTPPort:           getEnvIntOr("HTTP_PORT", 8003),
		MetricsPort:        getEnvIntOr("METRICS_PORT", 9193),
		HarborPort:         getEnvIntOr("HARBOR_PORT", 8000),
		RTPPort:            getEnvIntOr("RTP_PORT", 5006),
		SRTPort:            getEnvIntOr("SRT_PORT", 1935),
		WebRTCHTTPPort:     getEnvIntOr("WEBRTC_HTTP_PORT", 8004),
		EngineARTP:         getEnv("ENGINE_A_RTP"),
		EngineBRTP:         getEnv("ENGINE_B_RTP"),
		NetClockEnabled:    getEnvBoolOr("NETCLOCK_ENABLED", false),
		NetClockMasterAddr: getEnv("NETCLOCK_MASTER_ADDR"),
		ControlPlaneGRPC:   getEnv("CONTROL_PLANE_GRPC"),
		RedisAddr:          getEnv("REDIS_ADDR"),
		LogLevel:           getEnvOr("LOG_LEVEL", "info"),
	}

	if c.EngineARTP == "" {
		return nil, fmt.Errorf("FANOUT_ENGINE_A_RTP (or RLM_FANOUT_ENGINE_A_RTP) is required")
	}
	return c, nil
}

// envLookup tries FANOUT_<key> first, then RLM_FANOUT_<key>.
func envLookup(key string) string {
	if v := os.Getenv("FANOUT_" + key); v != "" {
		return v
	}
	if v := os.Getenv("RLM_FANOUT_" + key); v != "" {
		return v
	}
	return ""
}

func getEnv(key string) string {
	return envLookup(key)
}

func getEnvOr(key, def string) string {
	if v := envLookup(key); v != "" {
		return v
	}
	return def
}

func getEnvIntOr(key string, def int) int {
	if v := envLookup(key); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
	}
	return def
}

func getEnvBoolOr(key string, def bool) bool {
	v := strings.ToLower(envLookup(key))
	switch v {
	case "1", "true", "yes", "on":
		return true
	case "0", "false", "no", "off":
		return false
	}
	return def
}
