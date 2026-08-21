/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/

package client

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/rs/zerolog"
)

// TestRetry_UsesConfiguredMaxRetries guards #83: Retry hardcoded maxRetries=3 and
// retryInterval=2s, so Config.MaxRetries / RetryInterval did nothing. A client
// configured for 5 attempts must run the operation 5 times.
func TestRetry_UsesConfiguredMaxRetries(t *testing.T) {
	c := New(&Config{Address: "x", MaxRetries: 5, RetryInterval: time.Millisecond}, zerolog.Nop())

	attempts := 0
	err := c.Retry(context.Background(), func() error {
		attempts++
		return errors.New("boom")
	})
	if err == nil {
		t.Fatal("expected failure after exhausting retries")
	}
	if attempts != 5 {
		t.Fatalf("operation ran %d times, want 5 (Config.MaxRetries was ignored)", attempts)
	}
}

// TestNew_DefaultsRetryConfig checks the fallbacks when the caller leaves the
// retry config unset, so a zero config does not mean zero attempts.
func TestNew_DefaultsRetryConfig(t *testing.T) {
	c := New(&Config{Address: "x"}, zerolog.Nop())
	if c.maxRetries != 3 || c.retryInterval != 2*time.Second {
		t.Fatalf("defaults = %d / %v, want 3 / 2s", c.maxRetries, c.retryInterval)
	}
}
