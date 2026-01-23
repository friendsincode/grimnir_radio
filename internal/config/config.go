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
	DBBackend          DatabaseBackend
	DBDSN              string
	MediaRoot          string
	ObjectStorageURL   string
	GStreamerBin       string
	SchedulerLookahead time.Duration
	JWTSigningKey      string
	MetricsBind        string

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
}

// Load reads environment variables, applies defaults, and validates the result.
func Load() (*Config, error) {
    cfg := &Config{
        Environment:        getEnvAny([]string{"GRIMNIR_ENV", "RLM_ENV"}, "development"),
        HTTPBind:           getEnvAny([]string{"GRIMNIR_HTTP_BIND", "RLM_HTTP_BIND"}, "0.0.0.0"),
        HTTPPort:           getEnvIntAny([]string{"GRIMNIR_HTTP_PORT", "RLM_HTTP_PORT"}, 8080),
        DBBackend:          DatabaseBackend(getEnvAny([]string{"GRIMNIR_DB_BACKEND", "RLM_DB_BACKEND"}, string(DatabasePostgres))),
        DBDSN:              getEnvAny([]string{"GRIMNIR_DB_DSN", "RLM_DB_DSN"}, ""),
        MediaRoot:          getEnvAny([]string{"GRIMNIR_MEDIA_ROOT", "RLM_MEDIA_ROOT"}, "./media"),
        ObjectStorageURL:   getEnvAny([]string{"GRIMNIR_OBJECT_STORAGE_URL", "RLM_OBJECT_STORAGE_URL"}, ""),
        GStreamerBin:       getEnvAny([]string{"GRIMNIR_GSTREAMER_BIN", "RLM_GSTREAMER_BIN"}, "gst-launch-1.0"),
        SchedulerLookahead: time.Duration(getEnvIntAny([]string{"GRIMNIR_SCHEDULER_LOOKAHEAD_MINUTES", "RLM_SCHEDULER_LOOKAHEAD_MINUTES"}, 48)) * time.Hour,
        JWTSigningKey:      getEnvAny([]string{"GRIMNIR_JWT_SIGNING_KEY", "RLM_JWT_SIGNING_KEY"}, ""),
        MetricsBind:        getEnvAny([]string{"GRIMNIR_METRICS_BIND", "RLM_METRICS_BIND"}, "127.0.0.1:9000"),

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

	return cfg, nil
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
