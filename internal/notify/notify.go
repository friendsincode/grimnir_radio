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
	cfg  Config
	http *http.Client
}

// NewClient builds a Client with a 5-second HTTP timeout. ntfy POSTs are
// expected to complete in well under a second; the timeout exists to keep
// alert paths from blocking the caller forever when the ntfy server is gone.
func NewClient(cfg Config) *Client {
	return &Client{
		cfg:  cfg,
		http: &http.Client{Timeout: 5 * time.Second},
	}
}

// Notify sends a tier-1 (informational) alert to the audit topic. Priority 3.
// Use for "deploy started", "backup completed", "scan finished" — anything an
// operator would scroll through later but doesn't need to know right now.
func (c *Client) Notify(ctx context.Context, m Message) error {
	return c.post(ctx, c.cfg.AuditTopic(), c.cfg.AuditToken, m, 3, []string{"information_source"})
}

// Page sends a tier-2 (operator-paging) alert to the page topic. Priority 5
// (max), tagged with rotating_light by default. Use for soak failures, leader
// loss, anything that needs eyes on it now.
func (c *Client) Page(ctx context.Context, m Message) error {
	return c.post(ctx, c.cfg.PageTopic(), c.cfg.PageToken, m, 5, []string{"rotating_light"})
}

// PageAndRollback sends a tier-3 alert to the rollback topic. Same priority
// as Page but tagged so subscribers can wire a separate ringtone for "the
// system just rolled itself back, come look at the wreckage".
func (c *Client) PageAndRollback(ctx context.Context, m Message) error {
	return c.post(ctx, c.cfg.RollbackTopic(), c.cfg.RollbackToken, m, 5, []string{"arrows_counterclockwise", "rotating_light"})
}

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
		return fmt.Errorf("notify: post %s: %w", topic, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		return fmt.Errorf("notify: %s returned %d: %s", topic, resp.StatusCode, string(body))
	}
	return nil
}
