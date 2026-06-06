/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/

package audit

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// Priority maps to ntfy's priority field (1..5). See
// https://docs.ntfy.sh/publish/#message-priority.
type Priority int

const (
	PriorityMin     Priority = 1
	PriorityLow     Priority = 2
	PriorityDefault Priority = 3
	PriorityHigh    Priority = 4
	PriorityUrgent  Priority = 5
)

// Poster is the notification surface the audit Wrapper depends on. The
// concrete implementation lives in this package as *NtfyPoster (Chunk 1) and
// the wider grimnir-deploy config wiring (Chunk B-4) constructs it. Tests
// stub Poster with an in-memory fake so the middleware can be exercised
// without a real ntfy endpoint.
type Poster interface {
	Post(ctx context.Context, topic, title, message string, priority Priority, tags ...string) error
}

// NtfyPoster posts notifications to a ntfy.sh server. Safe for concurrent use.
type NtfyPoster struct {
	endpoint string // e.g., "https://ntfy.sh"
	token    string // optional Bearer token
	client   *http.Client
}

// NewNtfyPoster constructs a poster pointed at the given ntfy endpoint.
// Pass an empty token to skip the Authorization header.
func NewNtfyPoster(endpoint, token string) *NtfyPoster {
	return &NtfyPoster{
		endpoint: endpoint,
		token:    token,
		client:   &http.Client{Timeout: 10 * time.Second},
	}
}

type ntfyMessage struct {
	Topic    string   `json:"topic"`
	Title    string   `json:"title,omitempty"`
	Message  string   `json:"message"`
	Priority Priority `json:"priority,omitempty"`
	Tags     []string `json:"tags,omitempty"`
}

// Post sends a notification. Returns nil on 2xx, error on non-2xx or
// transport failure. Tags are optional; pass nothing for none.
func (p *NtfyPoster) Post(ctx context.Context, topic, title, message string, priority Priority, tags ...string) error {
	body, err := json.Marshal(ntfyMessage{
		Topic:    topic,
		Title:    title,
		Message:  message,
		Priority: priority,
		Tags:     tags,
	})
	if err != nil {
		return fmt.Errorf("marshal ntfy message: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, p.endpoint, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	if p.token != "" {
		req.Header.Set("Authorization", "Bearer "+p.token)
	}
	resp, err := p.client.Do(req)
	if err != nil {
		return fmt.Errorf("ntfy POST: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		b, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("ntfy returned %d: %s", resp.StatusCode, string(b))
	}
	return nil
}
