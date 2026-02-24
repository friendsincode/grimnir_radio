/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/

package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

// Database backend selection.
type DatabaseBackend string

const (
	DatabasePostgres DatabaseBackend = "postgres"
	DatabaseMySQL    DatabaseBackend = "mysql"
	DatabaseSQLite   DatabaseBackend = "sqlite"
)

// Config covers process level configuration read from environment variables.
type Config struct {
	Environment        string
	HTTPBind           string
	HTTPPort           int
	BaseURL            string // Public base URL (e.g., http://192.168.195.6:8080)
	DBBackend          DatabaseBackend
	DBDSN              string
	MediaRoot          string
	ObjectStorageURL   string
	GStreamerBin       string
	SchedulerLookahead time.Duration
	JWTSigningKey      string
	MetricsBind        string
	MaxUploadSizeMB    int // Optional global multipart upload limit override for web handlers (MB)

	// S3 Object Storage configuration
	S3AccessKeyID     string
	S3SecretAccessKey string
	S3Region          string
	S3Bucket          string
	S3Endpoint        string // For S3-compatible services (MinIO, Spaces, etc.)
	S3PublicBaseURL   string // Optional CDN/CloudFront URL
	S3UsePathStyle    bool   // Required for MinIO

	// Tracing configuration
	TracingEnabled    bool
	OTLPEndpoint      string
	TracingSampleRate float64

	// Multi-instance configuration
	LeaderElectionEnabled bool
	RedisAddr             string
	RedisPassword         string
	RedisDB               int
	InstanceID            string

	// Icecast configuration
	IcecastURL            string // Internal URL for stream proxy (e.g., http://icecast:8000)
	IcecastPublicURL      string // Public URL for direct access (e.g., http://radio.example.com:8000)
	IcecastSourcePassword string // Source password for connecting to Icecast

	// WebRTC configuration
	WebRTCEnabled      bool   // Enable WebRTC audio streaming
	WebRTCRTPPort      int    // UDP port for RTP audio input (default: 5004)
	WebRTCSTUNURL      string // STUN server for NAT traversal
	WebRTCTURNURL      string // TURN server for relaying (optional)
	WebRTCTURNUsername string // TURN username
	WebRTCTURNPassword string // TURN password

	// Harbor (built-in Icecast source receiver)
	HarborEnabled     bool   // GRIMNIR_HARBOR_ENABLED (default: false)
	HarborBind        string // GRIMNIR_HARBOR_BIND (default: "0.0.0.0")
	HarborPort        int    // GRIMNIR_HARBOR_PORT (default: 8088)
	HarborHost        string // GRIMNIR_HARBOR_HOST — public hostname DJs connect to (shown in UI)
	HarborPublicPort  int    // GRIMNIR_HARBOR_PUBLIC_PORT — public port shown to DJs (default: same as HarborPort)
	HarborMountPrefix string // GRIMNIR_HARBOR_MOUNT_PREFIX — path prefix when behind reverse proxy (e.g. "/harbor")
	HarborMaxSources  int    // GRIMNIR_HARBOR_MAX_SOURCES (default: 10)

	// Media Engine configuration
	MediaEngineGRPCAddr string // gRPC address of the media engine (e.g., "mediaengine:9091")
	LegacyEnvWarnings   []string
}

// Load reads environment variables, applies defaults, and validates the result.
func Load() (*Config, error) {
	cfg := &Config{
		Environment:        getEnvAny([]string{"GRIMNIR_ENV", "RLM_ENV"}, "development"),
		HTTPBind:           getEnvAny([]string{"GRIMNIR_HTTP_BIND", "RLM_HTTP_BIND"}, "0.0.0.0"),
		HTTPPort:           getEnvIntAny([]string{"GRIMNIR_HTTP_PORT", "RLM_HTTP_PORT"}, 8080),
		BaseURL:            getEnvAny([]string{"GRIMNIR_BASE_URL", "RLM_BASE_URL"}, ""),
		DBBackend:          DatabaseBackend(getEnvAny([]string{"GRIMNIR_DB_BACKEND", "RLM_DB_BACKEND"}, string(DatabasePostgres))),
		DBDSN:              getEnvAny([]string{"GRIMNIR_DB_DSN", "RLM_DB_DSN"}, ""),
		MediaRoot:          getEnvAny([]string{"GRIMNIR_MEDIA_ROOT", "RLM_MEDIA_ROOT"}, "./media"),
		ObjectStorageURL:   getEnvAny([]string{"GRIMNIR_OBJECT_STORAGE_URL", "RLM_OBJECT_STORAGE_URL"}, ""),
		GStreamerBin:       getEnvAny([]string{"GRIMNIR_GSTREAMER_BIN", "RLM_GSTREAMER_BIN"}, "gst-launch-1.0"),
		SchedulerLookahead: time.Duration(getEnvIntAny([]string{"GRIMNIR_SCHEDULER_LOOKAHEAD_MINUTES", "RLM_SCHEDULER_LOOKAHEAD_MINUTES"}, 168)) * time.Hour,
		JWTSigningKey:      getEnvAny([]string{"GRIMNIR_JWT_SIGNING_KEY", "RLM_JWT_SIGNING_KEY"}, ""),
		MetricsBind:        getEnvAny([]string{"GRIMNIR_METRICS_BIND", "RLM_METRICS_BIND"}, "127.0.0.1:9000"),
		MaxUploadSizeMB:    getEnvIntAny([]string{"GRIMNIR_MAX_UPLOAD_SIZE_MB", "RLM_MAX_UPLOAD_SIZE_MB"}, 0),

		// S3 Object Storage configuration
		S3AccessKeyID:     getEnvAny([]string{"GRIMNIR_S3_ACCESS_KEY_ID", "AWS_ACCESS_KEY_ID"}, ""),
		S3SecretAccessKey: getEnvAny([]string{"GRIMNIR_S3_SECRET_ACCESS_KEY", "AWS_SECRET_ACCESS_KEY"}, ""),
		S3Region:          getEnvAny([]string{"GRIMNIR_S3_REGION", "AWS_REGION"}, "us-east-1"),
		S3Bucket:          getEnvAny([]string{"GRIMNIR_S3_BUCKET", "S3_BUCKET"}, ""),
		S3Endpoint:        getEnvAny([]string{"GRIMNIR_S3_ENDPOINT", "S3_ENDPOINT"}, ""),
		S3PublicBaseURL:   getEnvAny([]string{"GRIMNIR_S3_PUBLIC_BASE_URL", "S3_PUBLIC_BASE_URL"}, ""),
		S3UsePathStyle:    getEnvBoolAny([]string{"GRIMNIR_S3_USE_PATH_STYLE", "S3_USE_PATH_STYLE"}, false),

		// Tracing configuration
		TracingEnabled:    getEnvBoolAny([]string{"GRIMNIR_TRACING_ENABLED", "RLM_TRACING_ENABLED"}, false),
		OTLPEndpoint:      getEnvAny([]string{"GRIMNIR_OTLP_ENDPOINT", "RLM_OTLP_ENDPOINT"}, "localhost:4317"),
		TracingSampleRate: getEnvFloatAny([]string{"GRIMNIR_TRACING_SAMPLE_RATE", "RLM_TRACING_SAMPLE_RATE"}, 1.0),

		// Multi-instance configuration
		LeaderElectionEnabled: getEnvBoolAny([]string{"GRIMNIR_LEADER_ELECTION_ENABLED", "RLM_LEADER_ELECTION_ENABLED"}, false),
		RedisAddr:             getEnvAny([]string{"GRIMNIR_REDIS_ADDR", "RLM_REDIS_ADDR"}, "localhost:6379"),
		RedisPassword:         getEnvAny([]string{"GRIMNIR_REDIS_PASSWORD", "RLM_REDIS_PASSWORD"}, ""),
		RedisDB:               getEnvIntAny([]string{"GRIMNIR_REDIS_DB", "RLM_REDIS_DB"}, 0),
		InstanceID:            getEnvAny([]string{"GRIMNIR_INSTANCE_ID", "RLM_INSTANCE_ID"}, ""),

		// Icecast configuration
		IcecastURL:            getEnvAny([]string{"GRIMNIR_ICECAST_URL", "ICECAST_URL"}, "http://icecast:8000"),
		IcecastPublicURL:      getEnvAny([]string{"GRIMNIR_ICECAST_PUBLIC_URL", "ICECAST_PUBLIC_URL"}, ""),
		IcecastSourcePassword: getEnvAny([]string{"GRIMNIR_ICECAST_SOURCE_PASSWORD", "ICECAST_SOURCE_PASSWORD"}, ""),

		// WebRTC configuration (enabled by default for low-latency streaming)
		WebRTCEnabled: getEnvBoolAny([]string{"GRIMNIR_WEBRTC_ENABLED", "WEBRTC_ENABLED"}, true),
		WebRTCRTPPort: getEnvIntAny([]string{"GRIMNIR_WEBRTC_RTP_PORT", "WEBRTC_RTP_PORT"}, 5004),
		WebRTCSTUNURL: getEnvAny([]string{"GRIMNIR_WEBRTC_STUN_URL", "WEBRTC_STUN_URL"}, "stun:stun.l.google.com:19302"),
		// TURN server for NAT traversal (coturn at radio.reallibertymedia.com)
		WebRTCTURNURL:      getEnvAny([]string{"GRIMNIR_WEBRTC_TURN_URL", "WEBRTC_TURN_URL"}, ""),
		WebRTCTURNUsername: getEnvAny([]string{"GRIMNIR_WEBRTC_TURN_USERNAME", "WEBRTC_TURN_USERNAME"}, ""),
		WebRTCTURNPassword: getEnvAny([]string{"GRIMNIR_WEBRTC_TURN_PASSWORD", "WEBRTC_TURN_PASSWORD"}, ""),

		// Harbor (built-in Icecast source receiver)
		HarborEnabled:     getEnvBoolAny([]string{"GRIMNIR_HARBOR_ENABLED", "HARBOR_ENABLED"}, false),
		HarborBind:        getEnvAny([]string{"GRIMNIR_HARBOR_BIND", "HARBOR_BIND"}, "0.0.0.0"),
		HarborPort:        getEnvIntAny([]string{"GRIMNIR_HARBOR_PORT", "HARBOR_PORT"}, 8088),
		HarborHost:        getEnvAny([]string{"GRIMNIR_HARBOR_HOST", "HARBOR_HOST"}, ""),
		HarborPublicPort:  getEnvIntAny([]string{"GRIMNIR_HARBOR_PUBLIC_PORT", "HARBOR_PUBLIC_PORT"}, 0),
		HarborMountPrefix: getEnvAny([]string{"GRIMNIR_HARBOR_MOUNT_PREFIX", "HARBOR_MOUNT_PREFIX"}, ""),
		HarborMaxSources:  getEnvIntAny([]string{"GRIMNIR_HARBOR_MAX_SOURCES", "HARBOR_MAX_SOURCES"}, 10),

		// Media Engine configuration
		MediaEngineGRPCAddr: getEnvAny([]string{"GRIMNIR_MEDIA_ENGINE_GRPC_ADDR", "MEDIA_ENGINE_GRPC_ADDR"}, "mediaengine:9091"),
	}

	if cfg.DBBackend != DatabasePostgres && cfg.DBBackend != DatabaseMySQL && cfg.DBBackend != DatabaseSQLite {
		return nil, fmt.Errorf("unsupported database backend %q", cfg.DBBackend)
	}

	if cfg.DBDSN == "" {
		return nil, fmt.Errorf("GRIMNIR_DB_DSN or RLM_DB_DSN must be provided")
	}

	if cfg.JWTSigningKey == "" {
		return nil, fmt.Errorf("GRIMNIR_JWT_SIGNING_KEY or RLM_JWT_SIGNING_KEY must be provided")
	}

	if strings.EqualFold(cfg.Environment, "production") {
		if cfg.IcecastSourcePassword == "" || strings.EqualFold(cfg.IcecastSourcePassword, "hackme") {
			return nil, fmt.Errorf("GRIMNIR_ICECAST_SOURCE_PASSWORD or ICECAST_SOURCE_PASSWORD must be set to a non-default value in production")
		}

		if cfg.WebRTCTURNURL != "" && (cfg.WebRTCTURNUsername == "" || cfg.WebRTCTURNPassword == "") {
			return nil, fmt.Errorf("GRIMNIR_WEBRTC_TURN_USERNAME and GRIMNIR_WEBRTC_TURN_PASSWORD are required when TURN is enabled in production")
		}
	}
	cfg.LegacyEnvWarnings = detectLegacyEnvWarnings()

	return cfg, nil
}

func detectLegacyEnvWarnings() []string {
	legacy := map[string]string{
		"ENVIRONMENT":             "use GRIMNIR_ENV (or RLM_ENV)",
		"LEADER_ELECTION_ENABLED": "use GRIMNIR_LEADER_ELECTION_ENABLED",
		"JWT_SIGNING_KEY":         "use GRIMNIR_JWT_SIGNING_KEY (or RLM_JWT_SIGNING_KEY)",
		"TRACING_ENABLED":         "use GRIMNIR_TRACING_ENABLED (or RLM_TRACING_ENABLED)",
		"OTLP_ENDPOINT":           "use GRIMNIR_OTLP_ENDPOINT (or RLM_OTLP_ENDPOINT)",
		"TRACING_SAMPLE_RATE":     "use GRIMNIR_TRACING_SAMPLE_RATE (or RLM_TRACING_SAMPLE_RATE)",
	}

	warnings := make([]string, 0, len(legacy))
	for key, recommendation := range legacy {
		if os.Getenv(key) != "" {
			warnings = append(warnings, fmt.Sprintf("legacy env key %s is set; %s", key, recommendation))
		}
	}
	return warnings
}

// MaxUploadSizeBytes returns the configured upload limit in bytes.
// A value of 0 means "not configured" and callers should use endpoint defaults.
func (c *Config) MaxUploadSizeBytes() int64 {
	if c == nil || c.MaxUploadSizeMB <= 0 {
		return 0
	}
	return int64(c.MaxUploadSizeMB) * 1024 * 1024
}

func getEnv(key, def string) string {
	if val := os.Getenv(key); val != "" {
		return val
	}
	return def
}

func getEnvInt(key string, def int) int {
	if val := os.Getenv(key); val != "" {
		if parsed, err := strconv.Atoi(val); err == nil {
			return parsed
		}
	}
	return def
}

// getEnvAny returns the first non-empty environment variable value from keys, or def if none set.
func getEnvAny(keys []string, def string) string {
	for _, k := range keys {
		if v := os.Getenv(k); v != "" {
			return v
		}
	}
	return def
}

// getEnvIntAny returns the first set integer environment variable value from keys, or def.
func getEnvIntAny(keys []string, def int) int {
	for _, k := range keys {
		if v := os.Getenv(k); v != "" {
			if parsed, err := strconv.Atoi(v); err == nil {
				return parsed
			}
		}
	}
	return def
}

// getEnvBoolAny returns the first set boolean environment variable value from keys, or def.
func getEnvBoolAny(keys []string, def bool) bool {
	for _, k := range keys {
		if v := os.Getenv(k); v != "" {
			v = strings.ToLower(strings.TrimSpace(v))
			if v == "true" || v == "1" || v == "yes" {
				return true
			}
			if v == "false" || v == "0" || v == "no" {
				return false
			}
		}
	}
	return def
}

// getEnvFloatAny returns the first set float environment variable value from keys, or def.
func getEnvFloatAny(keys []string, def float64) float64 {
	for _, k := range keys {
		if v := os.Getenv(k); v != "" {
			if parsed, err := strconv.ParseFloat(v, 64); err == nil {
				return parsed
			}
		}
	}
	return def
}
