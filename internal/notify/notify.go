/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/

// Package notify provides a typed client for the self-hosted ntfy.sh server.
//
// Three exported methods match the three alert tiers in Section 8.1 of the
// HA design:
//
//   - Notify           — tier-1 (informational, audit-grade)
//   - Page             — tier-2 (wake the operator)
//   - PageAndRollback  — tier-3 (operator is informed; the system has
//     already triggered grimnir-deploy --rollback)
//
// The tier abstraction lives in the caller. The client just routes each
// method to the configured per-region topic with the right priority.
package notify

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// Message is the wire-level shape sent to ntfy. Title becomes the push
// notification headline; Body is the visible message text. Tags map to ntfy
// emoji short names (see https://docs.ntfy.sh/emojis/). Click optionally
// turns the notification into a deep-link when tapped.
type Message struct {
	Title   string   // ntfy "Title" header
	Body    string   // request body
	Tags    []string // ntfy "Tags" header (emoji short names)
	Click   string   // optional URL on tap
	Actions []string // optional ntfy actions (max 3)
}

// Client posts notifications to ntfy. Safe for concurrent use. Construct via
// NewClient; the zero value is unusable.
type Client struct {
	cfg     Config
	http    *http.Client
	backoff []time.Duration // per-attempt sleep; len == max attempts
}

// defaultBackoff is the per-attempt sleep schedule for retried POSTs. Three
// attempts at 200ms / 500ms / 1500ms — capped under 2s so a failed alert
// surfaces back to the caller within the 5s HTTP timeout budget.
var defaultBackoff = []time.Duration{200 * time.Millisecond, 500 * time.Millisecond, 1500 * time.Millisecond}

// NewClient builds a Client with a 5-second HTTP timeout. ntfy POSTs are
// expected to complete in well under a second; the timeout exists to keep
// alert paths from blocking the caller forever when the ntfy server is gone.
func NewClient(cfg Config) *Client {
	return &Client{
		cfg:     cfg,
		http:    &http.Client{Timeout: 5 * time.Second},
		backoff: defaultBackoff,
	}
}

// Notify sends a tier-1 (informational) alert to the audit topic. Priority 3.
// Use for "deploy started", "backup completed", "scan finished" — anything an
// operator would scroll through later but doesn't need to know right now.
func (c *Client) Notify(ctx context.Context, m Message) error {
	return c.postWithRetry(ctx, c.cfg.AuditTopic(), c.cfg.AuditToken, m, 3, []string{"information_source"})
}

// Page sends a tier-2 (operator-paging) alert to the page topic. Priority 5
// (max), tagged with rotating_light by default. Use for soak failures, leader
// loss, anything that needs eyes on it now.
func (c *Client) Page(ctx context.Context, m Message) error {
	return c.postWithRetry(ctx, c.cfg.PageTopic(), c.cfg.PageToken, m, 5, []string{"rotating_light"})
}

// PageAndRollback sends a tier-3 alert to the rollback topic. Same priority
// as Page but tagged so subscribers can wire a separate ringtone for "the
// system just rolled itself back, come look at the wreckage".
func (c *Client) PageAndRollback(ctx context.Context, m Message) error {
	return c.postWithRetry(ctx, c.cfg.RollbackTopic(), c.cfg.RollbackToken, m, 5, []string{"arrows_counterclockwise", "rotating_light"})
}

// postError carries the HTTP status from a failed POST so the retry loop can
// distinguish 4xx (config error, do not retry) from 5xx (transient, retry).
// A status of 0 means transport-level failure (DNS, connection refused, etc.)
// which is also retried.
type postError struct {
	topic  string
	status int
	body   string
	cause  error
}

func (e *postError) Error() string {
	if e.cause != nil {
		return fmt.Sprintf("notify: post %s: %v", e.topic, e.cause)
	}
	return fmt.Sprintf("notify: %s returned %d: %s", e.topic, e.status, e.body)
}

func (e *postError) Unwrap() error { return e.cause }

// post runs a single attempt against the ntfy server. Use postWithRetry for
// the caller-facing path; this helper only exists so tests can target the
// transport behavior.
func (c *Client) post(ctx context.Context, topic, token string, m Message, prio int, defaultTags []string) error {
	url := strings.TrimRight(c.cfg.BaseURL, "/") + "/" + topic
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewBufferString(m.Body))
	if err != nil {
		return err
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	if m.Title != "" {
		req.Header.Set("Title", m.Title)
	}
	req.Header.Set("Priority", fmt.Sprintf("%d", prio))
	tags := m.Tags
	if len(tags) == 0 {
		tags = defaultTags
	}
	req.Header.Set("Tags", strings.Join(tags, ","))
	if m.Click != "" {
		req.Header.Set("Click", m.Click)
	}
	for _, a := range m.Actions {
		req.Header.Add("Actions", a)
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return &postError{topic: topic, cause: err}
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		return &postError{topic: topic, status: resp.StatusCode, body: string(body)}
	}
	return nil
}

// postWithRetry wraps post with the retry policy: 3 attempts on transport
// failure or 5xx, immediate fail on 4xx. ctx cancellation aborts between
// attempts.
func (c *Client) postWithRetry(ctx context.Context, topic, token string, m Message, prio int, defaultTags []string) error {
	var lastErr error
	for i, sleep := range c.backoff {
		if i > 0 {
			select {
			case <-time.After(sleep):
			case <-ctx.Done():
				return ctx.Err()
			}
		}
		err := c.post(ctx, topic, token, m, prio, defaultTags)
		if err == nil {
			return nil
		}
		lastErr = err
		// 4xx is a config error (auth, bad topic) — retrying won't help.
		if pe, ok := err.(*postError); ok && pe.status >= 400 && pe.status < 500 {
			return err
		}
	}
	return lastErr
}
