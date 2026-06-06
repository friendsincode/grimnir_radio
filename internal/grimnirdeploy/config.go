/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/

package grimnirdeploy

import (
	"errors"
	"os"
	"strconv"
	"time"
)

// Config is the minimal set of cluster connection settings the early-chunk
// subcommands need. Later chunks (deploy, drain, restore) will extend this
// with SSH keys, ntfy topics, registry creds, etc.
type Config struct {
	RedisAddr     string // e.g. <node-a-ip>:6379
	RedisPassword string // matches REDIS_PW substrate env
	RedisDB       int
	DBDSN         string // optional; emergency-pause / -resume only require Redis
	Region        string // GRIMNIR_REGION; defaults to "default"
	Operator      string // explicit operator override; defaults to $USER

	// Deploy-orchestration fields (Chunk 6+).
	DeployPolicy     string        // "auto" | "window" | "manual"; default "auto"
	DeployWindowCron string        // 5-field cron expr; only consulted when DeployPolicy=="window"
	SoakWindow       time.Duration // post-roll soak; default 5m
	PeerHost         string        // hostname of the other HA node
	PeerSSHUser      string        // SSH user for peer access; default "<ssh-user>"
	PeerSSHPort      int           // SSH port for peer access; default 22
	PeerSSHKey       string        // path to private key for peer SSH

	// RollbackWindow caps how recently the last-successful deploy must have
	// completed for `deploy --rollback` to proceed without --force-aged-rollback.
	// Default 30m. Operators set GRIMNIR_DEPLOY_ROLLBACK_WINDOW (e.g. "2h").
	RollbackWindow time.Duration
}

// LoadConfig reads the GRIMNIR_DEPLOY_* (and a few legacy GRIMNIR_*) env
// vars into a Config. Returns ErrMissingRedisAddr if no Redis address is
// configured; that is the only hard requirement for the Chunk 2 commands.
func LoadConfig() (*Config, error) {
	c := &Config{
		RedisAddr:        firstNonEmpty(os.Getenv("GRIMNIR_DEPLOY_REDIS_ADDR"), os.Getenv("GRIMNIR_REDIS_ADDR")),
		RedisPassword:    firstNonEmpty(os.Getenv("GRIMNIR_DEPLOY_REDIS_PASSWORD"), os.Getenv("GRIMNIR_REDIS_PASSWORD"), os.Getenv("REDIS_PW")),
		DBDSN:            firstNonEmpty(os.Getenv("GRIMNIR_DEPLOY_DB_DSN"), os.Getenv("GRIMNIR_DB_DSN")),
		Region:           firstNonEmpty(os.Getenv("GRIMNIR_REGION"), "default"),
		Operator:         firstNonEmpty(os.Getenv("GRIMNIR_DEPLOY_OPERATOR"), os.Getenv("USER"), "unknown"),
		DeployPolicy:     firstNonEmpty(os.Getenv("GRIMNIR_DEPLOY_POLICY"), "auto"),
		DeployWindowCron: os.Getenv("GRIMNIR_DEPLOY_WINDOW_CRON"),
		SoakWindow:       parseDurationOr(os.Getenv("GRIMNIR_DEPLOY_SOAK_WINDOW"), 5*time.Minute),
		PeerHost:         os.Getenv("GRIMNIR_DEPLOY_PEER_HOST"),
		PeerSSHUser:      firstNonEmpty(os.Getenv("GRIMNIR_DEPLOY_PEER_SSH_USER"), "<ssh-user>"),
		PeerSSHPort:      parseIntOr(os.Getenv("GRIMNIR_DEPLOY_PEER_SSH_PORT"), 22),
		PeerSSHKey:       os.Getenv("GRIMNIR_DEPLOY_PEER_SSH_KEY"),
		RollbackWindow:   parseDurationOr(os.Getenv("GRIMNIR_DEPLOY_ROLLBACK_WINDOW"), 30*time.Minute),
	}
	if c.RedisAddr == "" {
		return nil, ErrMissingRedisAddr
	}
	return c, nil
}

func parseDurationOr(s string, d time.Duration) time.Duration {
	if s == "" {
		return d
	}
	v, err := time.ParseDuration(s)
	if err != nil {
		return d
	}
	return v
}

func parseIntOr(s string, d int) int {
	if s == "" {
		return d
	}
	v, err := strconv.Atoi(s)
	if err != nil {
		return d
	}
	return v
}

// ErrMissingRedisAddr is returned when LoadConfig cannot find a Redis address.
var ErrMissingRedisAddr = errors.New("GRIMNIR_DEPLOY_REDIS_ADDR (or GRIMNIR_REDIS_ADDR) is required")

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}
