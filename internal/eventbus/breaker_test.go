/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/

package eventbus

import (
	"testing"

	"github.com/rs/zerolog"

	"github.com/friendsincode/grimnir_radio/internal/events"
)

// TestRedisBus_CircuitBreaker guards #86: handleFailure's threshold logic (fail
// count reaches maxFails -> flip to the in-memory fallback and close the client)
// had no test. It must not trip early, must trip exactly at the threshold, and
// must stay tripped on further failures without re-flipping.
func TestRedisBus_CircuitBreaker(t *testing.T) {
	rb := &RedisBus{maxFails: 3, fallback: events.NewBus(), logger: zerolog.Nop()}

	rb.handleFailure()
	rb.handleFailure()
	if rb.useFallback {
		t.Fatal("breaker tripped before reaching maxFails")
	}

	rb.handleFailure() // third failure hits the threshold
	if !rb.useFallback {
		t.Fatal("breaker should trip to fallback at maxFails")
	}

	rb.handleFailure() // further failures are safe and stay tripped
	if !rb.useFallback {
		t.Fatal("breaker should stay in fallback")
	}
}
