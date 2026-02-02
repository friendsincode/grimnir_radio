/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/

package notifications

import (
	"context"
	"fmt"
	"net/smtp"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/rs/zerolog"
	"gorm.io/gorm"

	"github.com/friendsincode/grimnir_radio/internal/events"
	"github.com/friendsincode/grimnir_radio/internal/models"
)

// Config holds notification service configuration.
type Config struct {
	// SMTP settings
	SMTPHost     string
	SMTPPort     int
	SMTPUsername string
	SMTPPassword string
	SMTPFrom     string
	SMTPFromName string

	// Reminder settings
	ReminderCheckInterval time.Duration
}

// ConfigFromEnv loads configuration from environment variables.
func ConfigFromEnv() Config {
	port, _ := strconv.Atoi(getEnv("GRIMNIR_SMTP_PORT", "587"))
	interval, _ := time.ParseDuration(getEnv("GRIMNIR_REMINDER_CHECK_INTERVAL", "1m"))

	return Config{
		SMTPHost:              getEnv("GRIMNIR_SMTP_HOST", ""),
		SMTPPort:              port,
		SMTPUsername:          getEnv("GRIMNIR_SMTP_USERNAME", ""),
		SMTPPassword:          getEnv("GRIMNIR_SMTP_PASSWORD", ""),
		SMTPFrom:              getEnv("GRIMNIR_SMTP_FROM", "noreply@example.com"),
		SMTPFromName:          getEnv("GRIMNIR_SMTP_FROM_NAME", "Grimnir Radio"),
		ReminderCheckInterval: interval,
	}
}

func getEnv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

// Service handles notification delivery and reminder scheduling.
type Service struct {
	db     *gorm.DB
	bus    *events.Bus
	config Config
	logger zerolog.Logger

	mu      sync.RWMutex
	running bool
}

// NewService creates a new notification service.
func NewService(db *gorm.DB, bus *events.Bus, config Config, logger zerolog.Logger) *Service {
	return &Service{
		db:     db,
		bus:    bus,
		config: config,
		logger: logger.With().Str("component", "notifications").Logger(),
	}
}

// Start begins the notification service, subscribing to events and running the reminder scheduler.
func (s *Service) Start(ctx context.Context) {
	s.mu.Lock()
	s.running = true
	s.mu.Unlock()

	s.logger.Info().Msg("notification service starting")

	// Subscribe to relevant events
	scheduleChange := s.bus.Subscribe(events.EventScheduleUpdate)
	djConnect := s.bus.Subscribe(events.EventDJConnect)
	djDisconnect := s.bus.Subscribe(events.EventDJDisconnect)

	defer func() {
		s.bus.Unsubscribe(events.EventScheduleUpdate, scheduleChange)
		s.bus.Unsubscribe(events.EventDJConnect, djConnect)
		s.bus.Unsubscribe(events.EventDJDisconnect, djDisconnect)
		s.mu.Lock()
		s.running = false
		s.mu.Unlock()
	}()

	// Start reminder scheduler
	reminderTicker := time.NewTicker(s.config.ReminderCheckInterval)
	defer reminderTicker.Stop()

	s.logger.Info().Msg("notification service started")

	for {
		select {
		case <-ctx.Done():
			s.logger.Info().Msg("notification service stopping")
			return

		case payload := <-scheduleChange:
			s.handleScheduleChange(ctx, payload)

		case payload := <-djConnect:
			s.handleDJConnect(ctx, payload)

		case payload := <-djDisconnect:
			s.handleDJDisconnect(ctx, payload)

		case <-reminderTicker.C:
			s.processReminders(ctx)
		}
	}
}

// handleScheduleChange creates notifications for schedule changes.
func (s *Service) handleScheduleChange(ctx context.Context, payload events.Payload) {
	stationID, _ := payload["station_id"].(string)
	if stationID == "" {
		return
	}

	// Get all users associated with this station who have schedule_change notifications enabled
	var prefs []models.NotificationPreference
	s.db.Joins("JOIN station_users ON station_users.user_id = notification_preferences.user_id").
		Where("station_users.station_id = ?", stationID).
		Where("notification_preferences.notification_type = ?", models.NotificationTypeScheduleChange).
		Where("notification_preferences.enabled = ?", true).
		Preload("User").
		Find(&prefs)

	for _, pref := range prefs {
		if pref.User == nil {
			continue
		}

		notification := &models.Notification{
			ID:               uuid.NewString(),
			UserID:           pref.UserID,
			NotificationType: models.NotificationTypeScheduleChange,
			Channel:          pref.Channel,
			Subject:          "Schedule Updated",
			Body:             "The schedule has been updated for your station.",
			Status:           models.NotificationStatusPending,
			ReferenceType:    "station",
			ReferenceID:      stationID,
			CreatedAt:        time.Now(),
		}

		s.Send(ctx, notification, pref.User)
	}
}

// handleDJConnect creates a notification when a DJ connects.
func (s *Service) handleDJConnect(ctx context.Context, payload events.Payload) {
	stationID, _ := payload["station_id"].(string)
	djName, _ := payload["dj_name"].(string)
	if stationID == "" {
		return
	}

	// Notify station managers about DJ connection
	var stationUsers []models.StationUser
	s.db.Where("station_id = ? AND role IN (?)", stationID,
		[]models.StationRole{models.StationRoleOwner, models.StationRoleAdmin, models.StationRoleManager}).
		Find(&stationUsers)

	for _, su := range stationUsers {
		// Get user
		var user models.User
		if err := s.db.First(&user, "id = ?", su.UserID).Error; err != nil {
			continue
		}

		// Check if user has in-app notifications enabled
		var pref models.NotificationPreference
		if err := s.db.Where("user_id = ? AND notification_type = ? AND channel = ? AND enabled = ?",
			su.UserID, models.NotificationTypeScheduleChange, models.NotificationChannelInApp, true).
			First(&pref).Error; err != nil {
			continue
		}

		notification := &models.Notification{
			ID:               uuid.NewString(),
			UserID:           su.UserID,
			NotificationType: models.NotificationTypeScheduleChange,
			Channel:          models.NotificationChannelInApp,
			Subject:          "DJ Connected",
			Body:             fmt.Sprintf("%s has connected to the station.", djName),
			Status:           models.NotificationStatusPending,
			ReferenceType:    "station",
			ReferenceID:      stationID,
			Metadata:         map[string]any{"dj_name": djName},
			CreatedAt:        time.Now(),
		}

		s.Send(ctx, notification, &user)
	}
}

// handleDJDisconnect creates a notification when a DJ disconnects.
func (s *Service) handleDJDisconnect(ctx context.Context, payload events.Payload) {
	stationID, _ := payload["station_id"].(string)
	djName, _ := payload["dj_name"].(string)
	if stationID == "" {
		return
	}

	// Notify station managers about DJ disconnection
	var stationUsers []models.StationUser
	s.db.Where("station_id = ? AND role IN (?)", stationID,
		[]models.StationRole{models.StationRoleOwner, models.StationRoleAdmin, models.StationRoleManager}).
		Find(&stationUsers)

	for _, su := range stationUsers {
		// Get user
		var user models.User
		if err := s.db.First(&user, "id = ?", su.UserID).Error; err != nil {
			continue
		}

		// Check if user has in-app notifications enabled
		var pref models.NotificationPreference
		if err := s.db.Where("user_id = ? AND notification_type = ? AND channel = ? AND enabled = ?",
			su.UserID, models.NotificationTypeScheduleChange, models.NotificationChannelInApp, true).
			First(&pref).Error; err != nil {
			continue
		}

		notification := &models.Notification{
			ID:               uuid.NewString(),
			UserID:           su.UserID,
			NotificationType: models.NotificationTypeScheduleChange,
			Channel:          models.NotificationChannelInApp,
			Subject:          "DJ Disconnected",
			Body:             fmt.Sprintf("%s has disconnected from the station.", djName),
			Status:           models.NotificationStatusPending,
			ReferenceType:    "station",
			ReferenceID:      stationID,
			Metadata:         map[string]any{"dj_name": djName},
			CreatedAt:        time.Now(),
		}

		s.Send(ctx, notification, &user)
	}
}

// processReminders checks for upcoming shows and sends reminders.
func (s *Service) processReminders(ctx context.Context) {
	now := time.Now()

	// Find show instances in the next hour that haven't had reminders sent
	var instances []models.ShowInstance
	s.db.Where("starts_at > ? AND starts_at < ?", now, now.Add(time.Hour)).
		Where("status = ?", models.ShowInstanceScheduled).
		Preload("Show").
		Preload("Host").
		Find(&instances)

	for _, instance := range instances {
		if instance.Host == nil || instance.Show == nil || instance.HostUserID == nil {
			continue
		}

		// Get user's reminder preferences
		var prefs []models.NotificationPreference
		s.db.Where("user_id = ? AND notification_type = ? AND enabled = ?",
			*instance.HostUserID, models.NotificationTypeShowReminder, true).
			Find(&prefs)

		for _, pref := range prefs {
			// Get reminder minutes from config (default to 30)
			reminderMinutes := 30
			if rm, ok := pref.Config["reminder_minutes"].(float64); ok {
				reminderMinutes = int(rm)
			}

			// Check if it's time to send this reminder
			reminderTime := instance.StartsAt.Add(-time.Duration(reminderMinutes) * time.Minute)
			if now.Before(reminderTime) || now.After(reminderTime.Add(s.config.ReminderCheckInterval)) {
				continue
			}

			// Check if we already sent this reminder
			var existingCount int64
			s.db.Model(&models.Notification{}).
				Where("user_id = ? AND reference_type = ? AND reference_id = ? AND channel = ? AND notification_type = ?",
					*instance.HostUserID, "show_instance", instance.ID, pref.Channel, models.NotificationTypeShowReminder).
				Count(&existingCount)

			if existingCount > 0 {
				continue
			}

			// Create reminder notification
			notification := &models.Notification{
				ID:               uuid.NewString(),
				UserID:           *instance.HostUserID,
				NotificationType: models.NotificationTypeShowReminder,
				Channel:          pref.Channel,
				Subject:          fmt.Sprintf("Reminder: %s starting soon", instance.Show.Name),
				Body: fmt.Sprintf("Your show '%s' starts in %d minutes at %s.",
					instance.Show.Name, reminderMinutes, instance.StartsAt.Format("3:04 PM")),
				Status:        models.NotificationStatusPending,
				ReferenceType: "show_instance",
				ReferenceID:   instance.ID,
				Metadata: map[string]any{
					"show_name":        instance.Show.Name,
					"start_time":       instance.StartsAt,
					"reminder_minutes": reminderMinutes,
				},
				CreatedAt: time.Now(),
			}

			s.Send(ctx, notification, instance.Host)
		}
	}
}

// Send delivers a notification via the configured channel.
func (s *Service) Send(ctx context.Context, notification *models.Notification, user *models.User) error {
	if notification.ID == "" {
		notification.ID = uuid.NewString()
	}
	if notification.CreatedAt.IsZero() {
		notification.CreatedAt = time.Now()
	}

	// Save notification first
	if err := s.db.WithContext(ctx).Create(notification).Error; err != nil {
		s.logger.Error().Err(err).Str("id", notification.ID).Msg("failed to save notification")
		return err
	}

	// Deliver based on channel
	var err error
	switch notification.Channel {
	case models.NotificationChannelEmail:
		err = s.sendEmail(ctx, notification, user)
	case models.NotificationChannelInApp:
		// In-app notifications are already stored, mark as sent
		notification.Status = models.NotificationStatusSent
		now := time.Now()
		notification.SentAt = &now
	case models.NotificationChannelSMS:
		// SMS not implemented yet
		s.logger.Warn().Str("channel", "sms").Msg("SMS notifications not implemented")
		notification.Status = models.NotificationStatusFailed
		notification.Error = "SMS notifications not implemented"
	case models.NotificationChannelPush:
		// Push not implemented yet
		s.logger.Warn().Str("channel", "push").Msg("Push notifications not implemented")
		notification.Status = models.NotificationStatusFailed
		notification.Error = "Push notifications not implemented"
	default:
		err = fmt.Errorf("unknown notification channel: %s", notification.Channel)
	}

	if err != nil {
		notification.Status = models.NotificationStatusFailed
		notification.Error = err.Error()
		s.logger.Error().Err(err).
			Str("id", notification.ID).
			Str("channel", string(notification.Channel)).
			Msg("failed to send notification")
	}

	// Update notification status
	s.db.WithContext(ctx).Model(notification).Updates(map[string]any{
		"status":  notification.Status,
		"sent_at": notification.SentAt,
		"error":   notification.Error,
	})

	return err
}

// sendEmail sends an email notification.
func (s *Service) sendEmail(ctx context.Context, notification *models.Notification, user *models.User) error {
	if s.config.SMTPHost == "" {
		return fmt.Errorf("SMTP not configured")
	}

	if user == nil || user.Email == "" {
		return fmt.Errorf("user has no email address")
	}

	// Build email
	from := s.config.SMTPFrom
	if s.config.SMTPFromName != "" {
		from = fmt.Sprintf("%s <%s>", s.config.SMTPFromName, s.config.SMTPFrom)
	}

	msg := strings.Builder{}
	msg.WriteString(fmt.Sprintf("From: %s\r\n", from))
	msg.WriteString(fmt.Sprintf("To: %s\r\n", user.Email))
	msg.WriteString(fmt.Sprintf("Subject: %s\r\n", notification.Subject))
	msg.WriteString("MIME-Version: 1.0\r\n")
	msg.WriteString("Content-Type: text/plain; charset=UTF-8\r\n")
	msg.WriteString("\r\n")
	msg.WriteString(notification.Body)

	// Send via SMTP
	addr := fmt.Sprintf("%s:%d", s.config.SMTPHost, s.config.SMTPPort)
	var auth smtp.Auth
	if s.config.SMTPUsername != "" {
		auth = smtp.PlainAuth("", s.config.SMTPUsername, s.config.SMTPPassword, s.config.SMTPHost)
	}

	err := smtp.SendMail(addr, auth, s.config.SMTPFrom, []string{user.Email}, []byte(msg.String()))
	if err != nil {
		return fmt.Errorf("SMTP send failed: %w", err)
	}

	notification.Status = models.NotificationStatusSent
	now := time.Now()
	notification.SentAt = &now

	s.logger.Info().
		Str("id", notification.ID).
		Str("to", user.Email).
		Str("subject", notification.Subject).
		Msg("email notification sent")

	return nil
}

// NotifyRequestStatus sends a notification about a schedule request status change.
func (s *Service) NotifyRequestStatus(ctx context.Context, request *models.ScheduleRequest, newStatus string) error {
	if request.Requester == nil {
		return fmt.Errorf("request has no requester")
	}

	// Get user's notification preferences
	var prefs []models.NotificationPreference
	s.db.Where("user_id = ? AND notification_type = ? AND enabled = ?",
		request.RequesterID, models.NotificationTypeRequestStatus, true).
		Find(&prefs)

	var subject, body string
	switch newStatus {
	case "approved":
		subject = "Schedule Request Approved"
		body = fmt.Sprintf("Your schedule request has been approved.")
	case "rejected":
		subject = "Schedule Request Rejected"
		body = fmt.Sprintf("Your schedule request has been rejected.")
		if request.ReviewNote != "" {
			body += fmt.Sprintf("\n\nNotes: %s", request.ReviewNote)
		}
	default:
		subject = "Schedule Request Updated"
		body = fmt.Sprintf("Your schedule request status has changed to: %s", newStatus)
	}

	for _, pref := range prefs {
		notification := &models.Notification{
			ID:               uuid.NewString(),
			UserID:           request.RequesterID,
			NotificationType: models.NotificationTypeRequestStatus,
			Channel:          pref.Channel,
			Subject:          subject,
			Body:             body,
			Status:           models.NotificationStatusPending,
			ReferenceType:    "schedule_request",
			ReferenceID:      request.ID,
			Metadata:         map[string]any{"status": newStatus},
			CreatedAt:        time.Now(),
		}

		s.Send(ctx, notification, request.Requester)
	}

	return nil
}

// NotifyNewAssignment sends a notification when a DJ is assigned to a show.
func (s *Service) NotifyNewAssignment(ctx context.Context, instance *models.ShowInstance) error {
	if instance.Host == nil || instance.HostUserID == nil {
		return nil
	}

	// Get user's notification preferences
	var prefs []models.NotificationPreference
	s.db.Where("user_id = ? AND notification_type = ? AND enabled = ?",
		*instance.HostUserID, models.NotificationTypeNewAssignment, true).
		Find(&prefs)

	showName := "Unknown Show"
	if instance.Show != nil {
		showName = instance.Show.Name
	}

	for _, pref := range prefs {
		notification := &models.Notification{
			ID:               uuid.NewString(),
			UserID:           *instance.HostUserID,
			NotificationType: models.NotificationTypeNewAssignment,
			Channel:          pref.Channel,
			Subject:          fmt.Sprintf("New Assignment: %s", showName),
			Body: fmt.Sprintf("You have been assigned to '%s' on %s at %s.",
				showName,
				instance.StartsAt.Format("Monday, January 2"),
				instance.StartsAt.Format("3:04 PM")),
			Status:        models.NotificationStatusPending,
			ReferenceType: "show_instance",
			ReferenceID:   instance.ID,
			Metadata: map[string]any{
				"show_name":  showName,
				"start_time": instance.StartsAt,
			},
			CreatedAt: time.Now(),
		}

		s.Send(ctx, notification, instance.Host)
	}

	return nil
}

// NotifyShowCancelled sends a notification when a show is cancelled.
func (s *Service) NotifyShowCancelled(ctx context.Context, instance *models.ShowInstance, reason string) error {
	if instance.Host == nil || instance.HostUserID == nil {
		return nil
	}

	// Get user's notification preferences
	var prefs []models.NotificationPreference
	s.db.Where("user_id = ? AND notification_type = ? AND enabled = ?",
		*instance.HostUserID, models.NotificationTypeShowCancelled, true).
		Find(&prefs)

	showName := "Unknown Show"
	if instance.Show != nil {
		showName = instance.Show.Name
	}

	body := fmt.Sprintf("Your show '%s' scheduled for %s at %s has been cancelled.",
		showName,
		instance.StartsAt.Format("Monday, January 2"),
		instance.StartsAt.Format("3:04 PM"))

	if reason != "" {
		body += fmt.Sprintf("\n\nReason: %s", reason)
	}

	for _, pref := range prefs {
		notification := &models.Notification{
			ID:               uuid.NewString(),
			UserID:           *instance.HostUserID,
			NotificationType: models.NotificationTypeShowCancelled,
			Channel:          pref.Channel,
			Subject:          fmt.Sprintf("Show Cancelled: %s", showName),
			Body:             body,
			Status:           models.NotificationStatusPending,
			ReferenceType:    "show_instance",
			ReferenceID:      instance.ID,
			Metadata: map[string]any{
				"show_name":  showName,
				"start_time": instance.StartsAt,
				"reason":     reason,
			},
			CreatedAt: time.Now(),
		}

		s.Send(ctx, notification, instance.Host)
	}

	return nil
}

// GetUserNotifications retrieves notifications for a user.
func (s *Service) GetUserNotifications(ctx context.Context, userID string, unreadOnly bool, limit, offset int) ([]models.Notification, int64, error) {
	var notifications []models.Notification
	var total int64

	query := s.db.WithContext(ctx).Model(&models.Notification{}).Where("user_id = ?", userID)

	if unreadOnly {
		query = query.Where("status != ?", models.NotificationStatusRead)
	}

	if err := query.Count(&total).Error; err != nil {
		return nil, 0, err
	}

	if limit <= 0 {
		limit = 50
	}

	if err := query.Order("created_at DESC").Limit(limit).Offset(offset).Find(&notifications).Error; err != nil {
		return nil, 0, err
	}

	return notifications, total, nil
}

// MarkAsRead marks a notification as read.
func (s *Service) MarkAsRead(ctx context.Context, notificationID, userID string) error {
	now := time.Now()
	result := s.db.WithContext(ctx).Model(&models.Notification{}).
		Where("id = ? AND user_id = ?", notificationID, userID).
		Updates(map[string]any{
			"status":  models.NotificationStatusRead,
			"read_at": now,
		})

	if result.RowsAffected == 0 {
		return fmt.Errorf("notification not found")
	}

	return result.Error
}

// MarkAllAsRead marks all notifications as read for a user.
func (s *Service) MarkAllAsRead(ctx context.Context, userID string) error {
	now := time.Now()
	return s.db.WithContext(ctx).Model(&models.Notification{}).
		Where("user_id = ? AND status != ?", userID, models.NotificationStatusRead).
		Updates(map[string]any{
			"status":  models.NotificationStatusRead,
			"read_at": now,
		}).Error
}

// GetUnreadCount returns the count of unread notifications for a user.
func (s *Service) GetUnreadCount(ctx context.Context, userID string) (int64, error) {
	var count int64
	err := s.db.WithContext(ctx).Model(&models.Notification{}).
		Where("user_id = ? AND status != ?", userID, models.NotificationStatusRead).
		Where("channel = ?", models.NotificationChannelInApp).
		Count(&count).Error
	return count, err
}

// CreateDefaultPreferences creates default notification preferences for a new user.
func (s *Service) CreateDefaultPreferences(ctx context.Context, userID string) error {
	prefs := models.DefaultNotificationPreferences(userID)
	for i := range prefs {
		prefs[i].ID = uuid.NewString()
	}
	return s.db.WithContext(ctx).Create(&prefs).Error
}

// GetUserPreferences retrieves notification preferences for a user.
func (s *Service) GetUserPreferences(ctx context.Context, userID string) ([]models.NotificationPreference, error) {
	var prefs []models.NotificationPreference
	err := s.db.WithContext(ctx).Where("user_id = ?", userID).Find(&prefs).Error
	return prefs, err
}

// UpdatePreference updates a notification preference.
func (s *Service) UpdatePreference(ctx context.Context, prefID, userID string, enabled bool, config map[string]any) error {
	updates := map[string]any{"enabled": enabled}
	if config != nil {
		updates["config"] = config
	}

	result := s.db.WithContext(ctx).Model(&models.NotificationPreference{}).
		Where("id = ? AND user_id = ?", prefID, userID).
		Updates(updates)

	if result.RowsAffected == 0 {
		return fmt.Errorf("preference not found")
	}

	return result.Error
}
