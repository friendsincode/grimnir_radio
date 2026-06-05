/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/

package edgeencoder

import (
	"testing"
)

func TestConfig_Defaults(t *testing.T) {
	for _, k := range []string{
		"EDGE_ENCODER_BIND_ADDR", "EDGE_ENCODER_GRPC_PORT",
		"EDGE_ENCODER_METRICS_PORT", "EDGE_ENCODER_HTTP_PORT",
		"EDGE_ENCODER_RTP_PORT_A", "EDGE_ENCODER_RTP_PORT_B",
		"EDGE_ENCODER_OUTPUT_FORMAT", "EDGE_ENCODER_OUTPUT_BITRATE_KBPS",
		"EDGE_ENCODER_HLS_ENABLED",
	} {
		t.Setenv(k, "")
	}
	c, err := LoadConfigFromEnv()
	if err != nil {
		t.Fatal(err)
	}
	if c.BindAddr != "0.0.0.0" {
		t.Errorf("BindAddr = %q, want 0.0.0.0", c.BindAddr)
	}
	if c.GRPCPort != 9092 {
		t.Errorf("GRPCPort = %d, want 9092", c.GRPCPort)
	}
	if c.MetricsPort != 9192 {
		t.Errorf("MetricsPort = %d, want 9192", c.MetricsPort)
	}
	if c.HTTPPort != 8001 {
		t.Errorf("HTTPPort = %d, want 8001", c.HTTPPort)
	}
	if c.RTPPortA != 5004 {
		t.Errorf("RTPPortA = %d, want 5004", c.RTPPortA)
	}
	if c.RTPPortB != 5005 {
		t.Errorf("RTPPortB = %d, want 5005", c.RTPPortB)
	}
	if c.OutputFormat != "mp3" {
		t.Errorf("OutputFormat = %q, want mp3", c.OutputFormat)
	}
	if c.OutputBitrateKbps != 128 {
		t.Errorf("OutputBitrateKbps = %d, want 128", c.OutputBitrateKbps)
	}
	if c.HLSEnabled {
		t.Error("HLSEnabled = true, want false")
	}
}

func TestConfig_OverridesViaEnv(t *testing.T) {
	t.Setenv("EDGE_ENCODER_GRPC_PORT", "19092")
	t.Setenv("EDGE_ENCODER_OUTPUT_BITRATE_KBPS", "192")
	t.Setenv("EDGE_ENCODER_HLS_ENABLED", "true")
	t.Setenv("EDGE_ENCODER_HLS_S3_BUCKET", "my-hls-bucket")

	c, err := LoadConfigFromEnv()
	if err != nil {
		t.Fatal(err)
	}
	if c.GRPCPort != 19092 {
		t.Errorf("GRPCPort = %d, want 19092", c.GRPCPort)
	}
	if c.OutputBitrateKbps != 192 {
		t.Errorf("OutputBitrateKbps = %d, want 192", c.OutputBitrateKbps)
	}
	if !c.HLSEnabled {
		t.Error("HLSEnabled = false, want true")
	}
	if c.HLSS3Bucket != "my-hls-bucket" {
		t.Errorf("HLSS3Bucket = %q, want my-hls-bucket", c.HLSS3Bucket)
	}
}

func TestConfig_HLSRequiresBucket(t *testing.T) {
	t.Setenv("EDGE_ENCODER_HLS_ENABLED", "true")
	t.Setenv("EDGE_ENCODER_HLS_S3_BUCKET", "")
	_, err := LoadConfigFromEnv()
	if err == nil {
		t.Error("LoadConfigFromEnv with HLS enabled and empty bucket: want error, got nil")
	}
}

func TestConfig_InvalidOutputFormat(t *testing.T) {
	t.Setenv("EDGE_ENCODER_OUTPUT_FORMAT", "flac")
	_, err := LoadConfigFromEnv()
	if err == nil {
		t.Error("LoadConfigFromEnv with format=flac: want error, got nil")
	}
}
