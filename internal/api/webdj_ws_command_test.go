/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/

package api

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/rs/zerolog"
)

// TestHandleCommand_UnknownAndPong guards #86: the two no-op branches of the
// WebDJ command dispatch must not error and must not touch the service. An
// unknown action is logged and swallowed; a pong is ignored. Both are reached
// with a nil service precisely because they never call it — a regression that
// routed either into the service would panic here.
func TestHandleCommand_UnknownAndPong(t *testing.T) {
	h := &WebDJWebSocket{logger: zerolog.Nop()} // nil service on purpose

	if err := h.handleCommand(context.Background(), "sess", wsCommand{Action: "nope"}); err != nil {
		t.Errorf("unknown action should be a silent no-op, got %v", err)
	}
	if err := h.handleCommand(context.Background(), "sess", wsCommand{Action: "pong"}); err != nil {
		t.Errorf("pong should be a silent no-op, got %v", err)
	}
}

// TestHandleCommand_MalformedData guards #86: a command carrying an invalid Data
// payload must fail at unmarshal, before the service is called. This is what
// stops a garbled frame from reaching the mixer with zero-valued parameters.
func TestHandleCommand_MalformedData(t *testing.T) {
	h := &WebDJWebSocket{logger: zerolog.Nop()} // nil service: unmarshal fails first

	err := h.handleCommand(context.Background(), "sess", wsCommand{
		Action: "seek",
		Deck:   "a",
		Data:   json.RawMessage(`not json`),
	})
	if err == nil {
		t.Fatal("malformed seek data should return an unmarshal error, got nil")
	}
}
