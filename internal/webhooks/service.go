/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/

package webhooks

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/rs/zerolog"
	"gorm.io/gorm"

	"github.com/friendsincode/grimnir_radio/internal/events"
	"github.com/friendsincode/grimnir_radio/internal/models"
)

// EventType constants for webhooks.
const (
	EventShowStart = "show_start"
	EventShowEnd   = "show_end"
)

// WebhookPayload is the payload sent to webhook endpoints.
type WebhookPayload struct {
	Event     string       `json:"event"`
	Timestamp time.Time    `json:"timestamp"`
	StationID string       `json:"station_id"`
	Show      *ShowPayload `json:"show,omitempty"`
	NextShow  *ShowPayload `json:"next_show,omitempty"`
}

// ShowPayload represents a show in the webhook payload.
type ShowPayload struct {
	ID          string    `json:"id"`
	Name        string    `json:"name"`
	Description string    `json:"description,omitempty"`
	HostName    string    `json:"host_name,omitempty"`
	Color       string    `json:"color,omitempty"`
	StartsAt    time.Time `json:"starts_at"`
	EndsAt      time.Time `json:"ends_at"`
}

// Service handles webhook delivery.
type Service struct {
	db     *gorm.DB
	bus    *events.Bus
	logger zerolog.Logger
	client *http.Client
}

// NewService creates a new webhook service.
func NewService(db *gorm.DB, bus *events.Bus, logger zerolog.Logger) *Service {
	return &Service{
		db:     db,
		bus:    bus,
		logger: logger.With().Str("component", "webhooks").Logger(),
		client: &http.Client{
			Timeout: 10 * time.Second,
		},
	}
}

// Start begins listening for events to trigger webhooks.
func (s *Service) Start(ctx context.Context) {
	s.logger.Info().Msg("webhook service starting")

	// Subscribe to show events
	showStart := s.bus.Subscribe(events.EventShowStart)
	showEnd := s.bus.Subscribe(events.EventShowEnd)

	defer func() {
		s.bus.Unsubscribe(events.EventShowStart, showStart)
		s.bus.Unsubscribe(events.EventShowEnd, showEnd)
	}()

	// Start the show transition checker in a goroutine
	go s.runTransitionChecker(ctx)

	s.logger.Info().Msg("webhook service started")

	for {
		select {
		case <-ctx.Done():
			s.logger.Info().Msg("webhook service stopping")
			return

		case payload := <-showStart:
			s.handleShowEvent(ctx, payload, EventShowStart)

		case payload := <-showEnd:
			s.handleShowEvent(ctx, payload, EventShowEnd)
		}
	}
}

// runTransitionChecker checks for show transitions every 30 seconds.
func (s *Service) runTransitionChecker(ctx context.Context) {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	// Track active shows per station
	activeShows := make(map[string]string) // stationID -> instanceID

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.checkTransitions(ctx, activeShows)
		}
	}
}

// checkTransitions checks for show transitions and fires webhooks.
func (s *Service) checkTransitions(ctx context.Context, activeShows map[string]string) {
	now := time.Now()

	// Get all public stations
	var stations []models.Station
	if err := s.db.Where("public = ?", true).Find(&stations).Error; err != nil {
		s.logger.Error().Err(err).Msg("failed to fetch stations for transition check")
		return
	}

	for _, station := range stations {
		// Find current show
		var currentInstance models.ShowInstance
		err := s.db.Where("station_id = ? AND starts_at <= ? AND ends_at > ?", station.ID, now, now).
			Where("status = ?", models.ShowInstanceScheduled).
			Preload("Show").
			Preload("Show.Host").
			Preload("Host").
			First(&currentInstance).Error

		currentID := ""
		if err == nil && currentInstance.ID != "" {
			currentID = currentInstance.ID
		}

		previousID := activeShows[station.ID]

		// Check for transitions
		if previousID != currentID {
			if previousID != "" && currentID == "" {
				// Show ended, no new show
				s.fireWebhooks(ctx, station.ID, EventShowEnd, nil, nil)
			} else if previousID == "" && currentID != "" {
				// New show started
				next := s.getNextShow(station.ID, currentInstance.EndsAt)
				s.fireWebhooks(ctx, station.ID, EventShowStart, &currentInstance, next)
			} else if previousID != "" && currentID != "" {
				// Show changed (previous ended, new started)
				s.fireWebhooks(ctx, station.ID, EventShowEnd, nil, &currentInstance)
				next := s.getNextShow(station.ID, currentInstance.EndsAt)
				s.fireWebhooks(ctx, station.ID, EventShowStart, &currentInstance, next)
			}

			activeShows[station.ID] = currentID
		}
	}
}

// getNextShow fetches the next scheduled show after the given time.
func (s *Service) getNextShow(stationID string, after time.Time) *models.ShowInstance {
	var next models.ShowInstance
	if err := s.db.Where("station_id = ? AND starts_at >= ?", stationID, after).
		Where("status = ?", models.ShowInstanceScheduled).
		Preload("Show").
		Preload("Show.Host").
		Preload("Host").
		Order("starts_at ASC").
		First(&next).Error; err != nil {
		return nil
	}
	return &next
}

// handleShowEvent handles show events from the event bus.
func (s *Service) handleShowEvent(ctx context.Context, payload events.Payload, eventType string) {
	stationID, ok := payload["station_id"].(string)
	if !ok {
		return
	}

	instanceID, _ := payload["instance_id"].(string)

	var instance *models.ShowInstance
	if instanceID != "" {
		var inst models.ShowInstance
		if err := s.db.Preload("Show").Preload("Show.Host").Preload("Host").
			First(&inst, "id = ?", instanceID).Error; err == nil {
			instance = &inst
		}
	}

	var next *models.ShowInstance
	if instance != nil {
		next = s.getNextShow(stationID, instance.EndsAt)
	} else {
		next = s.getNextShow(stationID, time.Now())
	}

	s.fireWebhooks(ctx, stationID, eventType, instance, next)
}

// fireWebhooks sends webhooks for a given event.
func (s *Service) fireWebhooks(ctx context.Context, stationID, eventType string, current, next *models.ShowInstance) {
	// Get active webhooks for this station and event
	var webhooks []models.WebhookTarget
	if err := s.db.Where("station_id = ? AND active = ?", stationID, true).Find(&webhooks).Error; err != nil {
		s.logger.Error().Err(err).Str("station", stationID).Msg("failed to fetch webhooks")
		return
	}

	for _, webhook := range webhooks {
		// Check if webhook is subscribed to this event
		if !s.webhookHandlesEvent(webhook, eventType) {
			continue
		}

		go s.sendWebhook(ctx, webhook, eventType, current, next)
	}
}

// webhookHandlesEvent checks if a webhook is subscribed to an event type.
func (s *Service) webhookHandlesEvent(webhook models.WebhookTarget, eventType string) bool {
	if webhook.Events == "" {
		return true // Default: handle all events
	}
	for _, e := range strings.Split(webhook.Events, ",") {
		if strings.TrimSpace(e) == eventType {
			return true
		}
	}
	return false
}

// sendWebhook sends a single webhook request.
func (s *Service) sendWebhook(ctx context.Context, webhook models.WebhookTarget, eventType string, current, next *models.ShowInstance) {
	payload := WebhookPayload{
		Event:     eventType,
		Timestamp: time.Now().UTC(),
		StationID: webhook.StationID,
	}

	if current != nil && current.Show != nil {
		payload.Show = s.instanceToPayload(current)
	}

	if next != nil && next.Show != nil {
		payload.NextShow = s.instanceToPayload(next)
	}

	body, err := json.Marshal(payload)
	if err != nil {
		s.logger.Error().Err(err).Str("webhook", webhook.ID).Msg("failed to marshal webhook payload")
		return
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, webhook.URL, bytes.NewReader(body))
	if err != nil {
		s.logger.Error().Err(err).Str("webhook", webhook.ID).Msg("failed to create webhook request")
		s.logWebhookDelivery(webhook, eventType, http.StatusInternalServerError, err.Error())
		return
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "Grimnir-Radio-Webhook/1.0")
	req.Header.Set("X-Grimnir-Event", eventType)
	req.Header.Set("X-Grimnir-Timestamp", fmt.Sprintf("%d", time.Now().Unix()))

	// Add HMAC signature if secret is configured
	if webhook.Secret != "" {
		sig := s.signPayload(body, webhook.Secret)
		req.Header.Set("X-Grimnir-Signature", sig)
	}

	resp, err := s.client.Do(req)
	if err != nil {
		s.logger.Error().Err(err).Str("webhook", webhook.ID).Str("url", webhook.URL).Msg("webhook delivery failed")
		s.logWebhookDelivery(webhook, eventType, 0, err.Error())
		return
	}
	defer resp.Body.Close()

	s.logWebhookDelivery(webhook, eventType, resp.StatusCode, "")

	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		s.logger.Debug().Str("webhook", webhook.ID).Str("event", eventType).Int("status", resp.StatusCode).Msg("webhook delivered")
	} else {
		s.logger.Warn().Str("webhook", webhook.ID).Str("event", eventType).Int("status", resp.StatusCode).Msg("webhook returned error status")
	}
}

// signPayload creates an HMAC-SHA256 signature.
func (s *Service) signPayload(payload []byte, secret string) string {
	h := hmac.New(sha256.New, []byte(secret))
	h.Write(payload)
	return "sha256=" + hex.EncodeToString(h.Sum(nil))
}

// instanceToPayload converts a ShowInstance to a ShowPayload.
func (s *Service) instanceToPayload(inst *models.ShowInstance) *ShowPayload {
	if inst == nil || inst.Show == nil {
		return nil
	}

	p := &ShowPayload{
		ID:          inst.ID,
		Name:        inst.Show.Name,
		Description: inst.Show.Description,
		Color:       inst.Show.Color,
		StartsAt:    inst.StartsAt,
		EndsAt:      inst.EndsAt,
	}

	if inst.Host != nil {
		p.HostName = inst.Host.Email
	} else if inst.Show.Host != nil {
		p.HostName = inst.Show.Host.Email
	}

	return p
}

// logWebhookDelivery logs a webhook delivery attempt.
func (s *Service) logWebhookDelivery(webhook models.WebhookTarget, eventType string, statusCode int, errorMsg string) {
	log := &models.WebhookLog{
		ID:         uuid.NewString(),
		TargetID:   webhook.ID,
		Event:      eventType,
		Payload:    "", // Payload is logged separately if needed
		StatusCode: statusCode,
		Error:      errorMsg,
	}

	if err := s.db.Create(log).Error; err != nil {
		s.logger.Error().Err(err).Msg("failed to log webhook delivery")
	}
}

// TestWebhook sends a test payload to a webhook.
func (s *Service) TestWebhook(webhook *models.WebhookTarget) error {
	payload := WebhookPayload{
		Event:     "test",
		Timestamp: time.Now().UTC(),
		StationID: webhook.StationID,
		Show: &ShowPayload{
			ID:          "test-show-id",
			Name:        "Test Show",
			Description: "This is a test webhook delivery",
			HostName:    "test@example.com",
			StartsAt:    time.Now(),
			EndsAt:      time.Now().Add(time.Hour),
		},
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("failed to marshal payload: %w", err)
	}

	req, err := http.NewRequestWithContext(context.Background(), http.MethodPost, webhook.URL, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "Grimnir-Radio-Webhook/1.0")
	req.Header.Set("X-Grimnir-Event", "test")
	req.Header.Set("X-Grimnir-Timestamp", fmt.Sprintf("%d", time.Now().Unix()))

	if webhook.Secret != "" {
		sig := s.signPayload(body, webhook.Secret)
		req.Header.Set("X-Grimnir-Signature", sig)
	}

	resp, err := s.client.Do(req)
	if err != nil {
		return fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("webhook returned status %d", resp.StatusCode)
	}

	return nil
}
