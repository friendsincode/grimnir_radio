/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/

package audit

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/rs/zerolog"
	"gorm.io/gorm"

	"github.com/friendsincode/grimnir_radio/internal/events"
	"github.com/friendsincode/grimnir_radio/internal/models"
)

// Service handles audit logging by subscribing to events and storing audit entries.
type Service struct {
	db     *gorm.DB
	bus    *events.Bus
	logger zerolog.Logger
}

// NewService creates a new audit service.
func NewService(db *gorm.DB, bus *events.Bus, logger zerolog.Logger) *Service {
	return &Service{
		db:     db,
		bus:    bus,
		logger: logger.With().Str("component", "audit").Logger(),
	}
}

// Start subscribes to relevant events and logs them as audit entries.
func (s *Service) Start(ctx context.Context) {
	s.logger.Info().Msg("audit service starting")

	// Subscribe to priority events
	priorityEmergency := s.bus.Subscribe(events.EventPriorityEmergency)
	priorityOverride := s.bus.Subscribe(events.EventPriorityOverride)
	priorityReleased := s.bus.Subscribe(events.EventPriorityReleased)

	// Subscribe to live events
	djConnect := s.bus.Subscribe(events.EventDJConnect)
	djDisconnect := s.bus.Subscribe(events.EventDJDisconnect)
	liveHandover := s.bus.Subscribe(events.EventLiveHandover)
	liveReleased := s.bus.Subscribe(events.EventLiveReleased)

	// Subscribe to schedule events
	scheduleUpdate := s.bus.Subscribe(events.EventScheduleUpdate)

	// Subscribe to webstream events
	webstreamFailover := s.bus.Subscribe(events.EventWebstreamFailover)

	// Subscribe to audit-specific events
	auditAPIKeyCreate := s.bus.Subscribe(events.EventAuditAPIKeyCreate)
	auditAPIKeyRevoke := s.bus.Subscribe(events.EventAuditAPIKeyRevoke)
	auditWebstreamCreate := s.bus.Subscribe(events.EventAuditWebstreamCreate)
	auditWebstreamUpdate := s.bus.Subscribe(events.EventAuditWebstreamUpdate)
	auditWebstreamDelete := s.bus.Subscribe(events.EventAuditWebstreamDelete)
	auditScheduleRefresh := s.bus.Subscribe(events.EventAuditScheduleRefresh)
	auditStationCreate := s.bus.Subscribe(events.EventAuditStationCreate)
	auditLandingPagePublish := s.bus.Subscribe(events.EventAuditLandingPagePublish)
	auditLandingPageRestore := s.bus.Subscribe(events.EventAuditLandingPageRestore)
	auditLandingPageUpdate := s.bus.Subscribe(events.EventAuditLandingPageUpdate)

	defer func() {
		s.bus.Unsubscribe(events.EventPriorityEmergency, priorityEmergency)
		s.bus.Unsubscribe(events.EventPriorityOverride, priorityOverride)
		s.bus.Unsubscribe(events.EventPriorityReleased, priorityReleased)
		s.bus.Unsubscribe(events.EventDJConnect, djConnect)
		s.bus.Unsubscribe(events.EventDJDisconnect, djDisconnect)
		s.bus.Unsubscribe(events.EventLiveHandover, liveHandover)
		s.bus.Unsubscribe(events.EventLiveReleased, liveReleased)
		s.bus.Unsubscribe(events.EventScheduleUpdate, scheduleUpdate)
		s.bus.Unsubscribe(events.EventWebstreamFailover, webstreamFailover)
		s.bus.Unsubscribe(events.EventAuditAPIKeyCreate, auditAPIKeyCreate)
		s.bus.Unsubscribe(events.EventAuditAPIKeyRevoke, auditAPIKeyRevoke)
		s.bus.Unsubscribe(events.EventAuditWebstreamCreate, auditWebstreamCreate)
		s.bus.Unsubscribe(events.EventAuditWebstreamUpdate, auditWebstreamUpdate)
		s.bus.Unsubscribe(events.EventAuditWebstreamDelete, auditWebstreamDelete)
		s.bus.Unsubscribe(events.EventAuditScheduleRefresh, auditScheduleRefresh)
		s.bus.Unsubscribe(events.EventAuditStationCreate, auditStationCreate)
		s.bus.Unsubscribe(events.EventAuditLandingPagePublish, auditLandingPagePublish)
		s.bus.Unsubscribe(events.EventAuditLandingPageRestore, auditLandingPageRestore)
		s.bus.Unsubscribe(events.EventAuditLandingPageUpdate, auditLandingPageUpdate)
	}()

	s.logger.Info().Msg("audit service started")

	for {
		select {
		case <-ctx.Done():
			s.logger.Info().Msg("audit service stopping")
			return

		case payload := <-priorityEmergency:
			s.logAuditEntry(ctx, models.AuditActionPriorityEmergency, payload)

		case payload := <-priorityOverride:
			s.logAuditEntry(ctx, models.AuditActionPriorityOverride, payload)

		case payload := <-priorityReleased:
			s.logAuditEntry(ctx, models.AuditActionPriorityRelease, payload)

		case payload := <-djConnect:
			s.logAuditEntry(ctx, models.AuditActionLiveConnect, payload)

		case payload := <-djDisconnect:
			s.logAuditEntry(ctx, models.AuditActionLiveDisconnect, payload)

		case payload := <-liveHandover:
			s.logAuditEntry(ctx, models.AuditActionLiveHandover, payload)

		case payload := <-liveReleased:
			s.logAuditEntry(ctx, models.AuditActionLiveDisconnect, payload)

		case payload := <-scheduleUpdate:
			s.logAuditEntry(ctx, models.AuditActionScheduleUpdate, payload)

		case payload := <-webstreamFailover:
			s.logAuditEntry(ctx, models.AuditActionWebstreamFailover, payload)

		case payload := <-auditAPIKeyCreate:
			s.logAuditEntry(ctx, models.AuditActionAPIKeyCreate, payload)

		case payload := <-auditAPIKeyRevoke:
			s.logAuditEntry(ctx, models.AuditActionAPIKeyRevoke, payload)

		case payload := <-auditWebstreamCreate:
			s.logAuditEntry(ctx, models.AuditActionWebstreamCreate, payload)

		case payload := <-auditWebstreamUpdate:
			s.logAuditEntry(ctx, models.AuditActionWebstreamUpdate, payload)

		case payload := <-auditWebstreamDelete:
			s.logAuditEntry(ctx, models.AuditActionWebstreamDelete, payload)

		case payload := <-auditScheduleRefresh:
			s.logAuditEntry(ctx, models.AuditActionScheduleRefresh, payload)

		case payload := <-auditStationCreate:
			s.logAuditEntry(ctx, models.AuditActionStationCreate, payload)

		case payload := <-auditLandingPagePublish:
			s.logAuditEntry(ctx, models.AuditActionLandingPagePublish, payload)

		case payload := <-auditLandingPageRestore:
			s.logAuditEntry(ctx, models.AuditActionLandingPageRestore, payload)

		case payload := <-auditLandingPageUpdate:
			s.logAuditEntry(ctx, models.AuditActionLandingPageUpdate, payload)
		}
	}
}

// logAuditEntry creates an audit log entry from an event payload.
func (s *Service) logAuditEntry(ctx context.Context, action models.AuditAction, payload events.Payload) {
	entry := &models.AuditLog{
		ID:        uuid.NewString(),
		Timestamp: time.Now(),
		Action:    action,
		Details:   make(map[string]any),
		CreatedAt: time.Now(),
	}

	// Extract user info
	if userID, ok := payload["user_id"].(string); ok && userID != "" {
		entry.UserID = &userID
	}
	if userEmail, ok := payload["user_email"].(string); ok {
		entry.UserEmail = userEmail
	}

	// Extract station info
	if stationID, ok := payload["station_id"].(string); ok && stationID != "" {
		entry.StationID = &stationID
	}

	// Extract resource info
	if resourceType, ok := payload["resource_type"].(string); ok {
		entry.ResourceType = resourceType
	}
	if resourceID, ok := payload["resource_id"].(string); ok {
		entry.ResourceID = resourceID
	}

	// Extract request context
	if ipAddress, ok := payload["ip_address"].(string); ok {
		entry.IPAddress = ipAddress
	}
	if userAgent, ok := payload["user_agent"].(string); ok {
		entry.UserAgent = userAgent
	}

	// Copy remaining fields to details
	for k, v := range payload {
		switch k {
		case "user_id", "user_email", "station_id", "resource_type", "resource_id", "ip_address", "user_agent":
			// Already extracted
		default:
			entry.Details[k] = v
		}
	}

	if err := s.Log(ctx, entry); err != nil {
		s.logger.Error().Err(err).
			Str("action", string(action)).
			Msg("failed to log audit entry")
	}
}

// Log records an audit entry directly (for non-event-bus actions).
func (s *Service) Log(ctx context.Context, entry *models.AuditLog) error {
	if entry.ID == "" {
		entry.ID = uuid.NewString()
	}
	if entry.Timestamp.IsZero() {
		entry.Timestamp = time.Now()
	}
	if entry.CreatedAt.IsZero() {
		entry.CreatedAt = time.Now()
	}
	if entry.Details == nil {
		entry.Details = make(map[string]any)
	}

	if err := s.db.WithContext(ctx).Create(entry).Error; err != nil {
		return err
	}

	s.logger.Debug().
		Str("action", string(entry.Action)).
		Str("id", entry.ID).
		Msg("audit entry logged")

	return nil
}

// QueryFilters defines filters for querying audit logs.
type QueryFilters struct {
	UserID    *string
	StationID *string
	Action    *models.AuditAction
	StartTime *time.Time
	EndTime   *time.Time
	Limit     int
	Offset    int
}

// Query retrieves audit logs with filters.
func (s *Service) Query(ctx context.Context, filters QueryFilters) ([]models.AuditLog, int64, error) {
	var logs []models.AuditLog
	var total int64

	query := s.db.WithContext(ctx).Model(&models.AuditLog{})

	if filters.UserID != nil {
		query = query.Where("user_id = ?", *filters.UserID)
	}
	if filters.StationID != nil {
		query = query.Where("station_id = ?", *filters.StationID)
	}
	if filters.Action != nil {
		query = query.Where("action = ?", *filters.Action)
	}
	if filters.StartTime != nil {
		query = query.Where("timestamp >= ?", *filters.StartTime)
	}
	if filters.EndTime != nil {
		query = query.Where("timestamp <= ?", *filters.EndTime)
	}

	// Count total
	if err := query.Count(&total).Error; err != nil {
		return nil, 0, err
	}

	// Apply pagination
	if filters.Limit > 0 {
		query = query.Limit(filters.Limit)
	} else {
		query = query.Limit(100) // Default limit
	}
	if filters.Offset > 0 {
		query = query.Offset(filters.Offset)
	}

	// Order by timestamp descending (most recent first)
	if err := query.Order("timestamp DESC").Find(&logs).Error; err != nil {
		return nil, 0, err
	}

	return logs, total, nil
}
