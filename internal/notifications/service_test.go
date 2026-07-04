/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/

package notifications

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/rs/zerolog"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	"github.com/friendsincode/grimnir_radio/internal/events"
	"github.com/friendsincode/grimnir_radio/internal/models"
)

func openNotifTestDB(t *testing.T) *gorm.DB {
	t.Helper()

	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatalf("db handle: %v", err)
	}
	// A single connection keeps every session on the same in-memory database
	// and serializes access from the service goroutine in Start tests.
	sqlDB.SetMaxOpenConns(1)

	if err := db.AutoMigrate(
		&models.User{},
		&models.Station{},
		&models.StationUser{},
		&models.Show{},
		&models.ShowInstance{},
		&models.ScheduleRequest{},
		&models.Notification{},
		&models.NotificationPreference{},
	); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	return db
}

func newNotifTestService(t *testing.T, config Config) (*Service, *gorm.DB, *events.Bus) {
	t.Helper()
	if config.ReminderCheckInterval == 0 {
		config.ReminderCheckInterval = time.Minute
	}
	db := openNotifTestDB(t)
	bus := events.NewBus()
	return NewService(db, bus, config, zerolog.Nop()), db, bus
}

func seedNotifUser(t *testing.T, db *gorm.DB, id, email string) {
	t.Helper()
	if err := db.Create(&models.User{ID: id, Email: email, PlatformRole: models.PlatformRoleUser}).Error; err != nil {
		t.Fatalf("seed user %s: %v", id, err)
	}
}

func seedStationUser(t *testing.T, db *gorm.DB, userID, stationID string, role models.StationRole) {
	t.Helper()
	su := models.StationUser{ID: uuid.NewString(), UserID: userID, StationID: stationID, Role: role}
	if err := db.Create(&su).Error; err != nil {
		t.Fatalf("seed station user %s: %v", userID, err)
	}
}

// seedPref creates a preference. gorm skips zero-value fields with a default
// tag on insert, so enabled=false is applied with a follow-up update.
func seedPref(t *testing.T, db *gorm.DB, userID string, ntype models.NotificationType, channel models.NotificationChannel, enabled bool, config map[string]any) string {
	t.Helper()
	pref := models.NotificationPreference{
		ID:               uuid.NewString(),
		UserID:           userID,
		NotificationType: ntype,
		Channel:          channel,
		Enabled:          true,
		Config:           config,
	}
	if err := db.Create(&pref).Error; err != nil {
		t.Fatalf("seed pref: %v", err)
	}
	if !enabled {
		if err := db.Model(&models.NotificationPreference{}).Where("id = ?", pref.ID).Update("enabled", false).Error; err != nil {
			t.Fatalf("disable pref: %v", err)
		}
	}
	return pref.ID
}

func notificationsFor(t *testing.T, db *gorm.DB, userID string) []models.Notification {
	t.Helper()
	var rows []models.Notification
	if err := db.Where("user_id = ?", userID).Find(&rows).Error; err != nil {
		t.Fatalf("fetch notifications: %v", err)
	}
	return rows
}

func TestConfigFromEnv(t *testing.T) {
	envKeys := []string{
		"GRIMNIR_SMTP_HOST", "GRIMNIR_SMTP_PORT", "GRIMNIR_SMTP_USERNAME",
		"GRIMNIR_SMTP_PASSWORD", "GRIMNIR_SMTP_FROM", "GRIMNIR_SMTP_FROM_NAME",
		"GRIMNIR_REMINDER_CHECK_INTERVAL",
	}

	t.Run("defaults", func(t *testing.T) {
		for _, k := range envKeys {
			t.Setenv(k, "")
		}
		cfg := ConfigFromEnv()
		if cfg.SMTPHost != "" || cfg.SMTPPort != 587 {
			t.Fatalf("unexpected SMTP defaults: %+v", cfg)
		}
		if cfg.SMTPFrom != "noreply@example.com" || cfg.SMTPFromName != "Grimnir Radio" {
			t.Fatalf("unexpected from defaults: %+v", cfg)
		}
		if cfg.ReminderCheckInterval != time.Minute {
			t.Fatalf("ReminderCheckInterval = %v, want 1m", cfg.ReminderCheckInterval)
		}
	})

	t.Run("explicit values", func(t *testing.T) {
		t.Setenv("GRIMNIR_SMTP_HOST", "mail.example.com")
		t.Setenv("GRIMNIR_SMTP_PORT", "2525")
		t.Setenv("GRIMNIR_SMTP_USERNAME", "mailer")
		t.Setenv("GRIMNIR_SMTP_PASSWORD", "hunter2")
		t.Setenv("GRIMNIR_SMTP_FROM", "radio@example.com")
		t.Setenv("GRIMNIR_SMTP_FROM_NAME", "Radio")
		t.Setenv("GRIMNIR_REMINDER_CHECK_INTERVAL", "30s")

		cfg := ConfigFromEnv()
		want := Config{
			SMTPHost: "mail.example.com", SMTPPort: 2525, SMTPUsername: "mailer",
			SMTPPassword: "hunter2", SMTPFrom: "radio@example.com", SMTPFromName: "Radio",
			ReminderCheckInterval: 30 * time.Second,
		}
		if cfg != want {
			t.Fatalf("cfg = %+v, want %+v", cfg, want)
		}
	})
}

func TestSendInApp(t *testing.T) {
	svc, db, _ := newNotifTestService(t, Config{})
	seedNotifUser(t, db, "u-1", "u1@example.com")

	notification := &models.Notification{
		UserID:           "u-1",
		NotificationType: models.NotificationTypeScheduleChange,
		Channel:          models.NotificationChannelInApp,
		Subject:          "Hello",
		Body:             "World",
	}
	if err := svc.Send(context.Background(), notification, &models.User{ID: "u-1"}); err != nil {
		t.Fatalf("send: %v", err)
	}
	if notification.ID == "" {
		t.Fatal("expected ID to be generated")
	}

	var row models.Notification
	if err := db.First(&row, "id = ?", notification.ID).Error; err != nil {
		t.Fatalf("fetch: %v", err)
	}
	if row.Status != models.NotificationStatusSent {
		t.Fatalf("status = %q, want sent", row.Status)
	}
	if row.SentAt == nil {
		t.Fatal("expected sent_at to be set")
	}
}

func TestSendUnknownChannel(t *testing.T) {
	svc, db, _ := newNotifTestService(t, Config{})

	notification := &models.Notification{
		ID:      "n-unknown",
		UserID:  "u-1",
		Channel: "carrier_pigeon",
		Body:    "coo",
	}
	err := svc.Send(context.Background(), notification, nil)
	if err == nil || !strings.Contains(err.Error(), "unknown notification channel") {
		t.Fatalf("expected unknown channel error, got %v", err)
	}

	var row models.Notification
	if err := db.First(&row, "id = ?", "n-unknown").Error; err != nil {
		t.Fatalf("fetch: %v", err)
	}
	if row.Status != models.NotificationStatusFailed || row.Error == "" {
		t.Fatalf("expected failed status with error, got %+v", row)
	}
}

// startFakeSMTP runs a minimal single-session SMTP server and returns its
// address plus a channel that receives the DATA section of the message.
func startFakeSMTP(t *testing.T) (host string, port int, messages <-chan string) {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	t.Cleanup(func() { ln.Close() })

	ch := make(chan string, 1)
	go func() {
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		br := bufio.NewReader(conn)
		fmt.Fprintf(conn, "220 fake ESMTP\r\n")
		var data strings.Builder
		inData := false
		for {
			line, err := br.ReadString('\n')
			if err != nil {
				return
			}
			if inData {
				if strings.TrimRight(line, "\r\n") == "." {
					inData = false
					fmt.Fprintf(conn, "250 OK\r\n")
					ch <- data.String()
					continue
				}
				data.WriteString(line)
				continue
			}
			switch cmd := strings.ToUpper(strings.TrimSpace(line)); {
			case strings.HasPrefix(cmd, "EHLO"), strings.HasPrefix(cmd, "HELO"):
				fmt.Fprintf(conn, "250 fake\r\n")
			case strings.HasPrefix(cmd, "DATA"):
				fmt.Fprintf(conn, "354 send data\r\n")
				inData = true
			case strings.HasPrefix(cmd, "QUIT"):
				fmt.Fprintf(conn, "221 bye\r\n")
				return
			default:
				fmt.Fprintf(conn, "250 OK\r\n")
			}
		}
	}()

	hostStr, portStr, err := net.SplitHostPort(ln.Addr().String())
	if err != nil {
		t.Fatalf("split addr: %v", err)
	}
	p, _ := strconv.Atoi(portStr)
	return hostStr, p, ch
}

func TestSendEmail(t *testing.T) {
	t.Run("smtp not configured", func(t *testing.T) {
		svc, db, _ := newNotifTestService(t, Config{})
		notification := &models.Notification{
			ID: "n-mail", UserID: "u-1",
			Channel: models.NotificationChannelEmail, Body: "b",
		}
		err := svc.Send(context.Background(), notification, &models.User{ID: "u-1", Email: "u@example.com"})
		if err == nil || !strings.Contains(err.Error(), "SMTP not configured") {
			t.Fatalf("expected SMTP not configured, got %v", err)
		}
		var row models.Notification
		if err := db.First(&row, "id = ?", "n-mail").Error; err != nil {
			t.Fatalf("fetch: %v", err)
		}
		if row.Status != models.NotificationStatusFailed {
			t.Fatalf("status = %q, want failed", row.Status)
		}
	})

	t.Run("missing recipient", func(t *testing.T) {
		svc, _, _ := newNotifTestService(t, Config{SMTPHost: "localhost", SMTPPort: 25})
		notification := &models.Notification{Body: "b"}
		if err := svc.sendEmail(context.Background(), notification, nil); err == nil {
			t.Fatal("expected error for nil user")
		}
		if err := svc.sendEmail(context.Background(), notification, &models.User{ID: "u"}); err == nil {
			t.Fatal("expected error for empty email")
		}
	})

	t.Run("delivers via smtp", func(t *testing.T) {
		host, port, messages := startFakeSMTP(t)
		svc, db, _ := newNotifTestService(t, Config{
			SMTPHost: host, SMTPPort: port,
			SMTPFrom: "radio@example.com", SMTPFromName: "Grimnir",
		})
		notification := &models.Notification{
			ID: "n-smtp", UserID: "u-1",
			Channel: models.NotificationChannelEmail,
			Subject: "Show Reminder", Body: "Starts in 30 minutes.",
		}
		user := &models.User{ID: "u-1", Email: "dj@example.com"}
		if err := svc.Send(context.Background(), notification, user); err != nil {
			t.Fatalf("send: %v", err)
		}

		select {
		case msg := <-messages:
			for _, want := range []string{
				"From: Grimnir <radio@example.com>",
				"To: dj@example.com",
				"Subject: Show Reminder",
				"Starts in 30 minutes.",
			} {
				if !strings.Contains(msg, want) {
					t.Errorf("message missing %q:\n%s", want, msg)
				}
			}
		case <-time.After(5 * time.Second):
			t.Fatal("timed out waiting for SMTP message")
		}

		var row models.Notification
		if err := db.First(&row, "id = ?", "n-smtp").Error; err != nil {
			t.Fatalf("fetch: %v", err)
		}
		if row.Status != models.NotificationStatusSent || row.SentAt == nil {
			t.Fatalf("expected sent status, got %+v", row)
		}
	})
}

func TestSendSMSAndPush(t *testing.T) {
	channels := []struct {
		name       string
		channel    models.NotificationChannel
		envVar     string
		primary    string // primary metadata key for destination
		fallback   string // fallback metadata key
		payloadKey string // destination key in outgoing JSON
		titleKey   string // subject/title key in outgoing JSON
	}{
		{
			name: "sms", channel: models.NotificationChannelSMS,
			envVar:  "GRIMNIR_SMS_WEBHOOK_URL",
			primary: "phone", fallback: "to", payloadKey: "to", titleKey: "subject",
		},
		{
			name: "push", channel: models.NotificationChannelPush,
			envVar:  "GRIMNIR_PUSH_WEBHOOK_URL",
			primary: "device_token", fallback: "token", payloadKey: "token", titleKey: "title",
		},
	}

	for _, tc := range channels {
		t.Run(tc.name, func(t *testing.T) {
			t.Run("webhook not configured", func(t *testing.T) {
				t.Setenv(tc.envVar, "")
				svc, _, _ := newNotifTestService(t, Config{})
				n := &models.Notification{
					UserID: "u-1", Channel: tc.channel, Body: "b",
					Metadata: map[string]any{tc.primary: "dest-1"},
				}
				err := svc.Send(context.Background(), n, &models.User{ID: "u-1"})
				if err == nil || !strings.Contains(err.Error(), "not configured") {
					t.Fatalf("expected not configured error, got %v", err)
				}
			})

			t.Run("missing destination", func(t *testing.T) {
				t.Setenv(tc.envVar, "http://127.0.0.1:9")
				svc, _, _ := newNotifTestService(t, Config{})
				n := &models.Notification{UserID: "u-1", Channel: tc.channel, Body: "b"}
				err := svc.Send(context.Background(), n, &models.User{ID: "u-1"})
				if err == nil || !strings.Contains(err.Error(), "missing") {
					t.Fatalf("expected missing destination error, got %v", err)
				}
			})

			t.Run("delivers with primary destination key", func(t *testing.T) {
				bodies := make(chan map[string]any, 1)
				srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					var m map[string]any
					if err := json.NewDecoder(r.Body).Decode(&m); err != nil {
						t.Errorf("decode: %v", err)
					}
					bodies <- m
				}))
				defer srv.Close()
				t.Setenv(tc.envVar, srv.URL)

				svc, db, _ := newNotifTestService(t, Config{})
				n := &models.Notification{
					ID: "n-" + tc.name, UserID: "u-1", Channel: tc.channel,
					Subject: "Alert", Body: "Something happened",
					Metadata: map[string]any{tc.primary: "dest-1"},
				}
				if err := svc.Send(context.Background(), n, &models.User{ID: "u-1", Email: "u@example.com"}); err != nil {
					t.Fatalf("send: %v", err)
				}

				got := <-bodies
				if got[tc.payloadKey] != "dest-1" {
					t.Fatalf("payload %s = %v, want dest-1", tc.payloadKey, got[tc.payloadKey])
				}
				if got[tc.titleKey] != "Alert" || got["body"] != "Something happened" {
					t.Fatalf("unexpected payload: %v", got)
				}

				var row models.Notification
				if err := db.First(&row, "id = ?", "n-"+tc.name).Error; err != nil {
					t.Fatalf("fetch: %v", err)
				}
				if row.Status != models.NotificationStatusSent || row.SentAt == nil {
					t.Fatalf("expected sent status, got %+v", row)
				}
			})

			t.Run("falls back to secondary destination key", func(t *testing.T) {
				bodies := make(chan map[string]any, 1)
				srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					var m map[string]any
					_ = json.NewDecoder(r.Body).Decode(&m)
					bodies <- m
				}))
				defer srv.Close()
				t.Setenv(tc.envVar, srv.URL)

				svc, _, _ := newNotifTestService(t, Config{})
				n := &models.Notification{
					UserID: "u-1", Channel: tc.channel, Body: "b",
					Metadata: map[string]any{tc.fallback: "dest-2"},
				}
				if err := svc.Send(context.Background(), n, &models.User{ID: "u-1"}); err != nil {
					t.Fatalf("send: %v", err)
				}
				if got := <-bodies; got[tc.payloadKey] != "dest-2" {
					t.Fatalf("payload %s = %v, want dest-2", tc.payloadKey, got[tc.payloadKey])
				}
			})

			t.Run("non-2xx response fails", func(t *testing.T) {
				srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					w.WriteHeader(http.StatusBadGateway)
				}))
				defer srv.Close()
				t.Setenv(tc.envVar, srv.URL)

				svc, _, _ := newNotifTestService(t, Config{})
				n := &models.Notification{
					UserID: "u-1", Channel: tc.channel, Body: "b",
					Metadata: map[string]any{tc.primary: "dest-1"},
				}
				err := svc.Send(context.Background(), n, &models.User{ID: "u-1"})
				if err == nil || !strings.Contains(err.Error(), "status 502") {
					t.Fatalf("expected status 502 error, got %v", err)
				}
			})

			t.Run("nil user rejected", func(t *testing.T) {
				t.Setenv(tc.envVar, "http://127.0.0.1:9")
				svc, _, _ := newNotifTestService(t, Config{})
				n := &models.Notification{
					UserID: "u-1", Channel: tc.channel, Body: "b",
					Metadata: map[string]any{tc.primary: "dest-1"},
				}
				if err := svc.Send(context.Background(), n, nil); err == nil {
					t.Fatal("expected error for nil user")
				}
			})
		})
	}
}

func TestHandleScheduleChangePreferenceFiltering(t *testing.T) {
	svc, db, _ := newNotifTestService(t, Config{})

	// alice: member with enabled in-app pref -> notified.
	seedNotifUser(t, db, "alice", "alice@example.com")
	seedStationUser(t, db, "alice", "st-1", models.StationRoleDJ)
	seedPref(t, db, "alice", models.NotificationTypeScheduleChange, models.NotificationChannelInApp, true, nil)

	// bob: member but pref disabled -> not notified.
	seedNotifUser(t, db, "bob", "bob@example.com")
	seedStationUser(t, db, "bob", "st-1", models.StationRoleDJ)
	seedPref(t, db, "bob", models.NotificationTypeScheduleChange, models.NotificationChannelInApp, false, nil)

	// carol: enabled pref but member of another station -> not notified.
	seedNotifUser(t, db, "carol", "carol@example.com")
	seedStationUser(t, db, "carol", "st-2", models.StationRoleDJ)
	seedPref(t, db, "carol", models.NotificationTypeScheduleChange, models.NotificationChannelInApp, true, nil)

	// dave: member with a pref for a different notification type -> not notified.
	seedNotifUser(t, db, "dave", "dave@example.com")
	seedStationUser(t, db, "dave", "st-1", models.StationRoleDJ)
	seedPref(t, db, "dave", models.NotificationTypeShowReminder, models.NotificationChannelInApp, true, nil)

	svc.handleScheduleChange(context.Background(), events.Payload{"station_id": "st-1"})

	var rows []models.Notification
	if err := db.Find(&rows).Error; err != nil {
		t.Fatalf("fetch: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("got %d notifications, want 1 (alice only): %+v", len(rows), rows)
	}
	got := rows[0]
	if got.UserID != "alice" || got.NotificationType != models.NotificationTypeScheduleChange {
		t.Fatalf("unexpected notification: %+v", got)
	}
	if got.ReferenceType != "station" || got.ReferenceID != "st-1" {
		t.Fatalf("unexpected reference: %+v", got)
	}

	t.Run("missing station_id is ignored", func(t *testing.T) {
		svc.handleScheduleChange(context.Background(), events.Payload{})
		var count int64
		db.Model(&models.Notification{}).Count(&count)
		if count != 1 {
			t.Fatalf("expected no new notifications, got %d total", count)
		}
	})
}

func TestHandleDJConnectDisconnect(t *testing.T) {
	tests := []struct {
		name        string
		handler     func(*Service, context.Context, events.Payload)
		wantSubject string
		wantBody    string
	}{
		{
			name:        "connect",
			handler:     (*Service).handleDJConnect,
			wantSubject: "DJ Connected",
			wantBody:    "DJ Cool has connected to the station.",
		},
		{
			name:        "disconnect",
			handler:     (*Service).handleDJDisconnect,
			wantSubject: "DJ Disconnected",
			wantBody:    "DJ Cool has disconnected from the station.",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc, db, _ := newNotifTestService(t, Config{})

			// owner: manager-level role with enabled in-app pref -> notified.
			seedNotifUser(t, db, "owner", "owner@example.com")
			seedStationUser(t, db, "owner", "st-1", models.StationRoleOwner)
			seedPref(t, db, "owner", models.NotificationTypeScheduleChange, models.NotificationChannelInApp, true, nil)

			// dj: has the pref but not a manager-level role -> not notified.
			seedNotifUser(t, db, "dj", "dj@example.com")
			seedStationUser(t, db, "dj", "st-1", models.StationRoleDJ)
			seedPref(t, db, "dj", models.NotificationTypeScheduleChange, models.NotificationChannelInApp, true, nil)

			// silent-admin: manager-level role but no pref -> not notified.
			seedNotifUser(t, db, "silent-admin", "sa@example.com")
			seedStationUser(t, db, "silent-admin", "st-1", models.StationRoleAdmin)

			tt.handler(svc, context.Background(), events.Payload{
				"station_id": "st-1",
				"dj_name":    "DJ Cool",
			})

			var rows []models.Notification
			if err := db.Find(&rows).Error; err != nil {
				t.Fatalf("fetch: %v", err)
			}
			if len(rows) != 1 {
				t.Fatalf("got %d notifications, want 1 (owner only): %+v", len(rows), rows)
			}
			got := rows[0]
			if got.UserID != "owner" || got.Subject != tt.wantSubject || got.Body != tt.wantBody {
				t.Fatalf("unexpected notification: %+v", got)
			}
			if got.Channel != models.NotificationChannelInApp {
				t.Fatalf("channel = %q, want in_app", got.Channel)
			}

			// Missing station_id is ignored.
			tt.handler(svc, context.Background(), events.Payload{"dj_name": "DJ Cool"})
			var count int64
			db.Model(&models.Notification{}).Count(&count)
			if count != 1 {
				t.Fatalf("expected no new notifications, got %d total", count)
			}
		})
	}
}

func seedReminderInstance(t *testing.T, db *gorm.DB, id, userID string, startsAt time.Time) {
	t.Helper()
	show := models.Show{ID: "show-" + id, StationID: "st-1", Name: "The " + id + " Show"}
	if err := db.Create(&show).Error; err != nil {
		t.Fatalf("seed show: %v", err)
	}
	var hostID *string
	if userID != "" {
		hostID = &userID
	}
	inst := models.ShowInstance{
		ID:         id,
		ShowID:     show.ID,
		StationID:  "st-1",
		StartsAt:   startsAt,
		EndsAt:     startsAt.Add(time.Hour),
		HostUserID: hostID,
		Status:     models.ShowInstanceScheduled,
	}
	if err := db.Create(&inst).Error; err != nil {
		t.Fatalf("seed instance: %v", err)
	}
}

func TestProcessReminders(t *testing.T) {
	t.Run("sends reminder inside the window with default minutes", func(t *testing.T) {
		svc, db, _ := newNotifTestService(t, Config{ReminderCheckInterval: time.Minute})
		seedNotifUser(t, db, "host", "host@example.com")
		seedPref(t, db, "host", models.NotificationTypeShowReminder, models.NotificationChannelInApp, true, nil)
		// Default reminder lead is 30 minutes; the reminder became due 5s ago.
		seedReminderInstance(t, db, "inst-due", "host", time.Now().Add(30*time.Minute-5*time.Second))

		svc.processReminders(context.Background())

		rows := notificationsFor(t, db, "host")
		if len(rows) != 1 {
			t.Fatalf("got %d notifications, want 1", len(rows))
		}
		got := rows[0]
		if got.NotificationType != models.NotificationTypeShowReminder {
			t.Fatalf("type = %q", got.NotificationType)
		}
		if got.ReferenceType != "show_instance" || got.ReferenceID != "inst-due" {
			t.Fatalf("unexpected reference: %+v", got)
		}
		if !strings.Contains(got.Body, "30 minutes") || !strings.Contains(got.Subject, "The inst-due Show") {
			t.Fatalf("unexpected content: subject=%q body=%q", got.Subject, got.Body)
		}

		// A second pass must not duplicate the reminder.
		svc.processReminders(context.Background())
		if rows := notificationsFor(t, db, "host"); len(rows) != 1 {
			t.Fatalf("reminder duplicated: got %d notifications", len(rows))
		}
	})

	t.Run("honors reminder_minutes from preference config", func(t *testing.T) {
		svc, db, _ := newNotifTestService(t, Config{ReminderCheckInterval: time.Minute})
		seedNotifUser(t, db, "host", "host@example.com")
		seedPref(t, db, "host", models.NotificationTypeShowReminder, models.NotificationChannelInApp, true,
			map[string]any{"reminder_minutes": 45})
		seedReminderInstance(t, db, "inst-45", "host", time.Now().Add(45*time.Minute-5*time.Second))

		svc.processReminders(context.Background())

		rows := notificationsFor(t, db, "host")
		if len(rows) != 1 {
			t.Fatalf("got %d notifications, want 1", len(rows))
		}
		if !strings.Contains(rows[0].Body, "45 minutes") {
			t.Fatalf("body = %q, want 45 minute lead", rows[0].Body)
		}
	})

	t.Run("skips shows whose reminder is not due yet", func(t *testing.T) {
		svc, db, _ := newNotifTestService(t, Config{ReminderCheckInterval: time.Minute})
		seedNotifUser(t, db, "host", "host@example.com")
		seedPref(t, db, "host", models.NotificationTypeShowReminder, models.NotificationChannelInApp, true, nil)
		// Show is 50 minutes out; a 30-minute reminder is not due for 20 minutes.
		seedReminderInstance(t, db, "inst-early", "host", time.Now().Add(50*time.Minute))

		svc.processReminders(context.Background())

		if rows := notificationsFor(t, db, "host"); len(rows) != 0 {
			t.Fatalf("expected no reminders yet, got %d", len(rows))
		}
	})

	t.Run("skips instances without a host", func(t *testing.T) {
		svc, db, _ := newNotifTestService(t, Config{ReminderCheckInterval: time.Minute})
		seedReminderInstance(t, db, "inst-nohost", "", time.Now().Add(30*time.Minute-5*time.Second))

		svc.processReminders(context.Background())

		var count int64
		db.Model(&models.Notification{}).Count(&count)
		if count != 0 {
			t.Fatalf("expected no reminders, got %d", count)
		}
	})
}

func TestNotifyRequestStatus(t *testing.T) {
	tests := []struct {
		name        string
		status      string
		reviewNote  string
		wantSubject string
		wantInBody  string
	}{
		{
			name: "approved", status: "approved",
			wantSubject: "Schedule Request Approved",
			wantInBody:  "has been approved",
		},
		{
			name: "rejected with note", status: "rejected", reviewNote: "slot taken",
			wantSubject: "Schedule Request Rejected",
			wantInBody:  "Notes: slot taken",
		},
		{
			name: "other status", status: "escalated",
			wantSubject: "Schedule Request Updated",
			wantInBody:  "changed to: escalated",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc, db, _ := newNotifTestService(t, Config{})
			seedNotifUser(t, db, "req-user", "req@example.com")
			seedPref(t, db, "req-user", models.NotificationTypeRequestStatus, models.NotificationChannelInApp, true, nil)

			request := &models.ScheduleRequest{
				ID:          "req-1",
				StationID:   "st-1",
				RequesterID: "req-user",
				Requester:   &models.User{ID: "req-user", Email: "req@example.com"},
				ReviewNote:  tt.reviewNote,
			}
			if err := svc.NotifyRequestStatus(context.Background(), request, tt.status); err != nil {
				t.Fatalf("notify: %v", err)
			}

			rows := notificationsFor(t, db, "req-user")
			if len(rows) != 1 {
				t.Fatalf("got %d notifications, want 1", len(rows))
			}
			if rows[0].Subject != tt.wantSubject {
				t.Fatalf("subject = %q, want %q", rows[0].Subject, tt.wantSubject)
			}
			if !strings.Contains(rows[0].Body, tt.wantInBody) {
				t.Fatalf("body = %q, want substring %q", rows[0].Body, tt.wantInBody)
			}
			if rows[0].ReferenceType != "schedule_request" || rows[0].ReferenceID != "req-1" {
				t.Fatalf("unexpected reference: %+v", rows[0])
			}
		})
	}

	t.Run("nil requester is an error", func(t *testing.T) {
		svc, _, _ := newNotifTestService(t, Config{})
		err := svc.NotifyRequestStatus(context.Background(), &models.ScheduleRequest{ID: "r"}, "approved")
		if err == nil {
			t.Fatal("expected error for request without requester")
		}
	})
}

func TestNotifyNewAssignment(t *testing.T) {
	starts := time.Date(2026, 7, 6, 21, 0, 0, 0, time.UTC)
	hostID := "dj-1"

	t.Run("notifies host per enabled preference", func(t *testing.T) {
		svc, db, _ := newNotifTestService(t, Config{})
		seedNotifUser(t, db, hostID, "dj@example.com")
		seedPref(t, db, hostID, models.NotificationTypeNewAssignment, models.NotificationChannelInApp, true, nil)

		instance := &models.ShowInstance{
			ID:         "inst-1",
			StartsAt:   starts,
			HostUserID: &hostID,
			Host:       &models.User{ID: hostID, Email: "dj@example.com"},
			Show:       &models.Show{Name: "Night Beats"},
		}
		if err := svc.NotifyNewAssignment(context.Background(), instance); err != nil {
			t.Fatalf("notify: %v", err)
		}

		rows := notificationsFor(t, db, hostID)
		if len(rows) != 1 {
			t.Fatalf("got %d notifications, want 1", len(rows))
		}
		if rows[0].Subject != "New Assignment: Night Beats" {
			t.Fatalf("subject = %q", rows[0].Subject)
		}
		if !strings.Contains(rows[0].Body, "Night Beats") || !strings.Contains(rows[0].Body, "9:00 PM") {
			t.Fatalf("body = %q", rows[0].Body)
		}
	})

	t.Run("unknown show name fallback", func(t *testing.T) {
		svc, db, _ := newNotifTestService(t, Config{})
		seedNotifUser(t, db, hostID, "dj@example.com")
		seedPref(t, db, hostID, models.NotificationTypeNewAssignment, models.NotificationChannelInApp, true, nil)

		instance := &models.ShowInstance{
			ID: "inst-2", StartsAt: starts, HostUserID: &hostID,
			Host: &models.User{ID: hostID},
		}
		if err := svc.NotifyNewAssignment(context.Background(), instance); err != nil {
			t.Fatalf("notify: %v", err)
		}
		rows := notificationsFor(t, db, hostID)
		if len(rows) != 1 || rows[0].Subject != "New Assignment: Unknown Show" {
			t.Fatalf("unexpected rows: %+v", rows)
		}
	})

	t.Run("instance without host is a no-op", func(t *testing.T) {
		svc, db, _ := newNotifTestService(t, Config{})
		if err := svc.NotifyNewAssignment(context.Background(), &models.ShowInstance{ID: "inst-3"}); err != nil {
			t.Fatalf("notify: %v", err)
		}
		var count int64
		db.Model(&models.Notification{}).Count(&count)
		if count != 0 {
			t.Fatalf("expected no notifications, got %d", count)
		}
	})
}

func TestNotifyShowCancelled(t *testing.T) {
	starts := time.Date(2026, 7, 6, 21, 0, 0, 0, time.UTC)
	hostID := "dj-1"

	tests := []struct {
		name       string
		reason     string
		wantReason bool
	}{
		{name: "with reason", reason: "transmitter maintenance", wantReason: true},
		{name: "without reason", reason: "", wantReason: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc, db, _ := newNotifTestService(t, Config{})
			seedNotifUser(t, db, hostID, "dj@example.com")
			seedPref(t, db, hostID, models.NotificationTypeShowCancelled, models.NotificationChannelInApp, true, nil)

			instance := &models.ShowInstance{
				ID: "inst-c", StartsAt: starts, HostUserID: &hostID,
				Host: &models.User{ID: hostID},
				Show: &models.Show{Name: "Night Beats"},
			}
			if err := svc.NotifyShowCancelled(context.Background(), instance, tt.reason); err != nil {
				t.Fatalf("notify: %v", err)
			}

			rows := notificationsFor(t, db, hostID)
			if len(rows) != 1 {
				t.Fatalf("got %d notifications, want 1", len(rows))
			}
			if rows[0].Subject != "Show Cancelled: Night Beats" {
				t.Fatalf("subject = %q", rows[0].Subject)
			}
			hasReason := strings.Contains(rows[0].Body, "Reason: transmitter maintenance")
			if hasReason != tt.wantReason {
				t.Fatalf("body = %q, wantReason=%v", rows[0].Body, tt.wantReason)
			}
		})
	}

	t.Run("instance without host is a no-op", func(t *testing.T) {
		svc, db, _ := newNotifTestService(t, Config{})
		if err := svc.NotifyShowCancelled(context.Background(), &models.ShowInstance{ID: "x"}, "r"); err != nil {
			t.Fatalf("notify: %v", err)
		}
		var count int64
		db.Model(&models.Notification{}).Count(&count)
		if count != 0 {
			t.Fatalf("expected no notifications, got %d", count)
		}
	})
}

func seedNotification(t *testing.T, db *gorm.DB, userID string, channel models.NotificationChannel, status models.NotificationStatus, createdAt time.Time) string {
	t.Helper()
	n := models.Notification{
		ID:               uuid.NewString(),
		UserID:           userID,
		NotificationType: models.NotificationTypeScheduleChange,
		Channel:          channel,
		Body:             "body",
		Status:           status,
		CreatedAt:        createdAt,
	}
	if err := db.Create(&n).Error; err != nil {
		t.Fatalf("seed notification: %v", err)
	}
	return n.ID
}

func TestGetUserNotifications(t *testing.T) {
	svc, db, _ := newNotifTestService(t, Config{})
	base := time.Now().Add(-time.Hour)

	newest := seedNotification(t, db, "u-1", models.NotificationChannelInApp, models.NotificationStatusSent, base.Add(30*time.Minute))
	middle := seedNotification(t, db, "u-1", models.NotificationChannelInApp, models.NotificationStatusRead, base.Add(20*time.Minute))
	oldest := seedNotification(t, db, "u-1", models.NotificationChannelInApp, models.NotificationStatusPending, base.Add(10*time.Minute))
	seedNotification(t, db, "u-other", models.NotificationChannelInApp, models.NotificationStatusSent, base)

	t.Run("all notifications newest first", func(t *testing.T) {
		rows, total, err := svc.GetUserNotifications(context.Background(), "u-1", false, 0, 0)
		if err != nil {
			t.Fatalf("get: %v", err)
		}
		if total != 3 || len(rows) != 3 {
			t.Fatalf("total=%d len=%d, want 3/3", total, len(rows))
		}
		if rows[0].ID != newest || rows[1].ID != middle || rows[2].ID != oldest {
			t.Fatalf("wrong order: %s %s %s", rows[0].ID, rows[1].ID, rows[2].ID)
		}
	})

	t.Run("unread only excludes read", func(t *testing.T) {
		rows, total, err := svc.GetUserNotifications(context.Background(), "u-1", true, 0, 0)
		if err != nil {
			t.Fatalf("get: %v", err)
		}
		if total != 2 || len(rows) != 2 {
			t.Fatalf("total=%d len=%d, want 2/2", total, len(rows))
		}
		for _, row := range rows {
			if row.Status == models.NotificationStatusRead {
				t.Fatalf("read notification returned: %+v", row)
			}
		}
	})

	t.Run("limit and offset", func(t *testing.T) {
		rows, total, err := svc.GetUserNotifications(context.Background(), "u-1", false, 1, 1)
		if err != nil {
			t.Fatalf("get: %v", err)
		}
		if total != 3 || len(rows) != 1 || rows[0].ID != middle {
			t.Fatalf("total=%d rows=%+v, want middle only", total, rows)
		}
	})
}

func TestMarkAsRead(t *testing.T) {
	svc, db, _ := newNotifTestService(t, Config{})
	id := seedNotification(t, db, "u-1", models.NotificationChannelInApp, models.NotificationStatusSent, time.Now())

	if err := svc.MarkAsRead(context.Background(), id, "u-1"); err != nil {
		t.Fatalf("mark as read: %v", err)
	}
	var row models.Notification
	if err := db.First(&row, "id = ?", id).Error; err != nil {
		t.Fatalf("fetch: %v", err)
	}
	if row.Status != models.NotificationStatusRead || row.ReadAt == nil {
		t.Fatalf("expected read status with read_at, got %+v", row)
	}

	t.Run("wrong user cannot mark", func(t *testing.T) {
		other := seedNotification(t, db, "u-1", models.NotificationChannelInApp, models.NotificationStatusSent, time.Now())
		if err := svc.MarkAsRead(context.Background(), other, "u-2"); err == nil {
			t.Fatal("expected not found error for wrong user")
		}
	})

	t.Run("unknown notification", func(t *testing.T) {
		if err := svc.MarkAsRead(context.Background(), "missing", "u-1"); err == nil {
			t.Fatal("expected not found error")
		}
	})
}

func TestMarkAllAsReadAndUnreadCount(t *testing.T) {
	svc, db, _ := newNotifTestService(t, Config{})
	now := time.Now()

	seedNotification(t, db, "u-1", models.NotificationChannelInApp, models.NotificationStatusSent, now)
	seedNotification(t, db, "u-1", models.NotificationChannelInApp, models.NotificationStatusPending, now)
	seedNotification(t, db, "u-1", models.NotificationChannelInApp, models.NotificationStatusRead, now)
	// Unread email must not count toward the in-app badge.
	seedNotification(t, db, "u-1", models.NotificationChannelEmail, models.NotificationStatusSent, now)
	seedNotification(t, db, "u-2", models.NotificationChannelInApp, models.NotificationStatusSent, now)

	count, err := svc.GetUnreadCount(context.Background(), "u-1")
	if err != nil {
		t.Fatalf("unread count: %v", err)
	}
	if count != 2 {
		t.Fatalf("unread count = %d, want 2 (in-app only)", count)
	}

	if err := svc.MarkAllAsRead(context.Background(), "u-1"); err != nil {
		t.Fatalf("mark all: %v", err)
	}
	count, err = svc.GetUnreadCount(context.Background(), "u-1")
	if err != nil {
		t.Fatalf("unread count: %v", err)
	}
	if count != 0 {
		t.Fatalf("unread count after mark all = %d, want 0", count)
	}

	// Another user's notifications stay untouched.
	count, err = svc.GetUnreadCount(context.Background(), "u-2")
	if err != nil {
		t.Fatalf("unread count: %v", err)
	}
	if count != 1 {
		t.Fatalf("u-2 unread count = %d, want 1", count)
	}
}

func TestPreferences(t *testing.T) {
	svc, db, _ := newNotifTestService(t, Config{})
	ctx := context.Background()
	seedNotifUser(t, db, "u-1", "u1@example.com")

	if err := svc.CreateDefaultPreferences(ctx, "u-1"); err != nil {
		t.Fatalf("create defaults: %v", err)
	}

	prefs, err := svc.GetUserPreferences(ctx, "u-1")
	if err != nil {
		t.Fatalf("get prefs: %v", err)
	}
	wantDefaults := len(models.DefaultNotificationPreferences("u-1"))
	if len(prefs) != wantDefaults {
		t.Fatalf("got %d prefs, want %d", len(prefs), wantDefaults)
	}
	for _, p := range prefs {
		if p.ID == "" {
			t.Fatalf("preference without ID: %+v", p)
		}
	}

	t.Run("update preference", func(t *testing.T) {
		target := prefs[0]
		if err := svc.UpdatePreference(ctx, target.ID, "u-1", false, map[string]any{"reminder_minutes": 10}); err != nil {
			t.Fatalf("update: %v", err)
		}
		var got models.NotificationPreference
		if err := db.First(&got, "id = ?", target.ID).Error; err != nil {
			t.Fatalf("fetch: %v", err)
		}
		if got.Enabled {
			t.Fatal("expected preference disabled")
		}
		if rm, ok := got.Config["reminder_minutes"].(float64); !ok || rm != 10 {
			t.Fatalf("config = %v, want reminder_minutes 10", got.Config)
		}
	})

	t.Run("wrong user cannot update", func(t *testing.T) {
		if err := svc.UpdatePreference(ctx, prefs[0].ID, "u-2", true, nil); err == nil {
			t.Fatal("expected not found error for wrong user")
		}
	})

	t.Run("unknown preference", func(t *testing.T) {
		if err := svc.UpdatePreference(ctx, "missing", "u-1", true, nil); err == nil {
			t.Fatal("expected not found error")
		}
	})
}

func TestStartProcessesBusEvents(t *testing.T) {
	svc, db, bus := newNotifTestService(t, Config{ReminderCheckInterval: 25 * time.Millisecond})

	seedNotifUser(t, db, "owner", "owner@example.com")
	seedStationUser(t, db, "owner", "st-1", models.StationRoleOwner)
	seedPref(t, db, "owner", models.NotificationTypeScheduleChange, models.NotificationChannelInApp, true, nil)

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		svc.Start(ctx)
		close(done)
	}()

	// Start subscribes asynchronously; publish until each event lands.
	waitForSubject := func(event events.EventType, payload events.Payload, subject string) {
		t.Helper()
		deadline := time.Now().Add(5 * time.Second)
		for time.Now().Before(deadline) {
			bus.Publish(event, payload)
			time.Sleep(20 * time.Millisecond)
			var count int64
			db.Model(&models.Notification{}).Where("user_id = ? AND subject = ?", "owner", subject).Count(&count)
			if count > 0 {
				return
			}
		}
		t.Fatalf("timed out waiting for %q notification", subject)
	}

	waitForSubject(events.EventScheduleUpdate, events.Payload{"station_id": "st-1"}, "Schedule Updated")
	waitForSubject(events.EventDJConnect, events.Payload{"station_id": "st-1", "dj_name": "DJ Cool"}, "DJ Connected")
	waitForSubject(events.EventDJDisconnect, events.Payload{"station_id": "st-1", "dj_name": "DJ Cool"}, "DJ Disconnected")

	cancel()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("Start did not stop on context cancel")
	}
}
