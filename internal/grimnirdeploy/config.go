/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/

package grimnirdeploy

import (
	"errors"
	"os"
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
}

// LoadConfig reads the GRIMNIR_DEPLOY_* (and a few legacy GRIMNIR_*) env
// vars into a Config. Returns ErrMissingRedisAddr if no Redis address is
// configured; that is the only hard requirement for the Chunk 2 commands.
func LoadConfig() (*Config, error) {
	c := &Config{
		RedisAddr:     firstNonEmpty(os.Getenv("GRIMNIR_DEPLOY_REDIS_ADDR"), os.Getenv("GRIMNIR_REDIS_ADDR")),
		RedisPassword: firstNonEmpty(os.Getenv("GRIMNIR_DEPLOY_REDIS_PASSWORD"), os.Getenv("GRIMNIR_REDIS_PASSWORD"), os.Getenv("REDIS_PW")),
		DBDSN:         firstNonEmpty(os.Getenv("GRIMNIR_DEPLOY_DB_DSN"), os.Getenv("GRIMNIR_DB_DSN")),
		Region:        firstNonEmpty(os.Getenv("GRIMNIR_REGION"), "default"),
		Operator:      firstNonEmpty(os.Getenv("GRIMNIR_DEPLOY_OPERATOR"), os.Getenv("USER"), "unknown"),
	}
	if c.RedisAddr == "" {
		return nil, ErrMissingRedisAddr
	}
	return c, nil
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
