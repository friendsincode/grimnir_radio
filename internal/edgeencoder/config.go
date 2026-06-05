/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/

package edgeencoder

import (
	"fmt"
	"os"
	"strconv"
	"strings"
)

// Config holds all runtime configuration for the edge encoder. Loaded from
// environment variables; see LoadConfigFromEnv for the variable names.
type Config struct {
	BindAddr          string
	GRPCPort          int
	MetricsPort       int
	HTTPPort          int
	RTPPortA          int
	RTPPortB          int
	EngineAGRPC       string
	EngineBGRPC       string
	OutputFormat      string // "mp3" or "aac"
	OutputBitrateKbps int
	HLSEnabled        bool
	HLSS3Bucket       string
	HLSSegmentDir     string
	LogLevel          string
}

func LoadConfigFromEnv() (*Config, error) {
	c := &Config{
		BindAddr:          getEnvOr("EDGE_ENCODER_BIND_ADDR", "0.0.0.0"),
		GRPCPort:          getEnvIntOr("EDGE_ENCODER_GRPC_PORT", 9092),
		MetricsPort:       getEnvIntOr("EDGE_ENCODER_METRICS_PORT", 9192),
		HTTPPort:          getEnvIntOr("EDGE_ENCODER_HTTP_PORT", 8001),
		RTPPortA:          getEnvIntOr("EDGE_ENCODER_RTP_PORT_A", 5004),
		RTPPortB:          getEnvIntOr("EDGE_ENCODER_RTP_PORT_B", 5005),
		EngineAGRPC:       os.Getenv("EDGE_ENCODER_ENGINE_A_GRPC"),
		EngineBGRPC:       os.Getenv("EDGE_ENCODER_ENGINE_B_GRPC"),
		OutputFormat:      strings.ToLower(getEnvOr("EDGE_ENCODER_OUTPUT_FORMAT", "mp3")),
		OutputBitrateKbps: getEnvIntOr("EDGE_ENCODER_OUTPUT_BITRATE_KBPS", 128),
		HLSEnabled:        getEnvBoolOr("EDGE_ENCODER_HLS_ENABLED", false),
		HLSS3Bucket:       os.Getenv("EDGE_ENCODER_HLS_S3_BUCKET"),
		HLSSegmentDir:     getEnvOr("EDGE_ENCODER_HLS_SEGMENT_DIR", "/tmp/grimnir-hls"),
		LogLevel:          getEnvOr("EDGE_ENCODER_LOG_LEVEL", "info"),
	}

	switch c.OutputFormat {
	case "mp3", "aac":
	default:
		return nil, fmt.Errorf("EDGE_ENCODER_OUTPUT_FORMAT=%q invalid; want mp3 or aac", c.OutputFormat)
	}
	if c.HLSEnabled && c.HLSS3Bucket == "" {
		return nil, fmt.Errorf("EDGE_ENCODER_HLS_ENABLED=true requires non-empty EDGE_ENCODER_HLS_S3_BUCKET")
	}
	return c, nil
}

func getEnvOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func getEnvIntOr(key string, def int) int {
	if v := os.Getenv(key); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
	}
	return def
}

func getEnvBoolOr(key string, def bool) bool {
	v := strings.ToLower(os.Getenv(key))
	switch v {
	case "1", "true", "yes", "on":
		return true
	case "0", "false", "no", "off":
		return false
	}
	return def
}
