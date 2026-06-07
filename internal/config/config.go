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
	HarborSSL         bool   // GRIMNIR_HARBOR_SSL — whether DJs should enable SSL/TLS (default: false)
	HarborMaxSources  int    // GRIMNIR_HARBOR_MAX_SOURCES (default: 10)

	// Media Engine configuration
	MediaEngineGRPCAddr string // gRPC address of the media engine (e.g., "mediaengine:9091")

	// HAPCMRTPEnabled controls whether the media engine emits raw L16 PCM via
	// RTP for ingest by the new edge encoder. False (default) keeps the legacy
	// fdsink fd=3/4 dual-bitrate output as the sole output. True adds a PCM-RTP
	// branch alongside the legacy output.
	HAPCMRTPEnabled bool

	// HAPCMRTPTargets is the list of "host:port" destinations for the PCM-RTP
	// stream. Typically two entries (local edge encoder + peer edge encoder).
	// Required when HAPCMRTPEnabled is true.
	HAPCMRTPTargets []string

	// NetClockEnabled is the master switch for the GStreamer net-clock subsystem.
	// When true the process participates in master/slave clock election using
	// Redis leases; the master spawns a NetTimeProvider, slaves create a
	// NetClientClock dialed at NetClockMasterAddr. Default: false (each process
	// uses its own GstSystemClock, today's behavior).
	NetClockEnabled bool

	// NetClockPort is the TCP port the master's NetTimeProvider listens on.
	// Slaves dial NetClockMasterAddr (which typically encodes "host:port" of
	// the master's NetClockPort). Default: 9094.
	NetClockPort int

	// NetClockRegion identifies the failover group; it's part of the Redis
	// lease key (grimnir-netclock-master-<region>). Required when
	// NetClockEnabled is true. No default.
	NetClockRegion string

	// NetClockMasterAddr is the "host:port" slaves dial to reach the master's
	// NetTimeProvider. Optional; if empty, slaves can fall back to Redis-based
	// discovery (deferred to Chunk 3). Pure-packet-fallback if neither is set.
	NetClockMasterAddr string

	// LiveInputEnabled enables the engine-side mixing branch that consumes a
	// PCM/RTP feed from the fan-out node (live DJ audio). When false, engine
	// pipelines behave exactly as they did pre-fan-out: scheduled content only.
	LiveInputEnabled bool

	// LiveInputPort is the UDP port the engine pipeline's udpsrc listens on
	// for incoming PCM/RTP from the fan-out node. Default: 5008. The fan-out
	// must be configured to deliver to this engine's host:port.
	LiveInputPort int

	// LiveInputFanoutAddr is the host:port of the fan-out service (for
	// outbound gRPC calls in the other direction; reserved for future
	// engine->fanout health hints). Required when LiveInputEnabled is true.
	LiveInputFanoutAddr string

	// VRRPVIPs is the set of VIP application names this control plane should
	// poll out of Redis for the grimnir_vrrp_holder_count gauge. Names are
	// arbitrary identifiers (e.g., "listener", "dj") that keepalived's
	// notify.sh writes into the Redis hash `grimnir:vrrp:<name>`. Empty
	// disables the poller. Populated from GRIMNIR_VRRP_VIPS (comma-separated).
	VRRPVIPs []string

	LegacyEnvWarnings []string
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
		HarborSSL:         getEnvBoolAny([]string{"GRIMNIR_HARBOR_SSL", "HARBOR_SSL"}, false),
		HarborMaxSources:  getEnvIntAny([]string{"GRIMNIR_HARBOR_MAX_SOURCES", "HARBOR_MAX_SOURCES"}, 10),

		// Media Engine configuration
		MediaEngineGRPCAddr: getEnvAny([]string{"GRIMNIR_MEDIA_ENGINE_GRPC_ADDR", "MEDIA_ENGINE_GRPC_ADDR"}, "mediaengine:9091"),

		// High-availability PCM-RTP ingest for the edge encoder.
		HAPCMRTPEnabled: getEnvBoolAny([]string{"GRIMNIR_HA_PCM_RTP_ENABLED", "RLM_HA_PCM_RTP_ENABLED"}, false),

		// NetClock: master/slave GStreamer clock synchronization.
		NetClockEnabled:    getEnvBoolAny([]string{"GRIMNIR_NETCLOCK_ENABLED", "RLM_NETCLOCK_ENABLED"}, false),
		NetClockPort:       getEnvIntAny([]string{"GRIMNIR_NETCLOCK_PORT", "RLM_NETCLOCK_PORT"}, 9094),
		NetClockRegion:     getEnvAny([]string{"GRIMNIR_NETCLOCK_REGION", "RLM_NETCLOCK_REGION"}, ""),
		NetClockMasterAddr: getEnvAny([]string{"GRIMNIR_NETCLOCK_MASTER_ADDR", "RLM_NETCLOCK_MASTER_ADDR"}, ""),

		// Engine-side live input branch (fan-out -> engine PCM/RTP ingest).
		LiveInputEnabled:    getEnvBoolAny([]string{"GRIMNIR_LIVE_INPUT_ENABLED", "RLM_LIVE_INPUT_ENABLED"}, false),
		LiveInputPort:       getEnvIntAny([]string{"GRIMNIR_LIVE_INPUT_PORT", "RLM_LIVE_INPUT_PORT"}, 5008),
		LiveInputFanoutAddr: getEnvAny([]string{"GRIMNIR_LIVE_INPUT_FANOUT_ADDR", "RLM_LIVE_INPUT_FANOUT_ADDR"}, ""),
	}

	if raw := getEnvAny([]string{"GRIMNIR_HA_PCM_RTP_TARGETS", "RLM_HA_PCM_RTP_TARGETS"}, ""); raw != "" {
		for _, t := range strings.Split(raw, ",") {
			t = strings.TrimSpace(t)
			if t != "" {
				cfg.HAPCMRTPTargets = append(cfg.HAPCMRTPTargets, t)
			}
		}
	}

	if raw := getEnvAny([]string{"GRIMNIR_VRRP_VIPS"}, ""); raw != "" {
		for _, v := range strings.Split(raw, ",") {
			v = strings.TrimSpace(v)
			if v != "" {
				cfg.VRRPVIPs = append(cfg.VRRPVIPs, v)
			}
		}
	}
	if cfg.HAPCMRTPEnabled && len(cfg.HAPCMRTPTargets) == 0 {
		return nil, fmt.Errorf("GRIMNIR_HA_PCM_RTP_ENABLED=true requires non-empty GRIMNIR_HA_PCM_RTP_TARGETS")
	}

	if cfg.NetClockEnabled && cfg.NetClockRegion == "" {
		return nil, fmt.Errorf("GRIMNIR_NETCLOCK_ENABLED=true requires non-empty GRIMNIR_NETCLOCK_REGION")
	}

	if cfg.LiveInputEnabled && cfg.LiveInputFanoutAddr == "" {
		return nil, fmt.Errorf("GRIMNIR_LIVE_INPUT_ENABLED=true requires non-empty GRIMNIR_LIVE_INPUT_FANOUT_ADDR")
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
