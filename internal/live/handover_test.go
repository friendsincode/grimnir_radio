package live

import (
	"context"
	"testing"
	"time"

	"github.com/friendsincode/grimnir_radio/internal/events"
	"github.com/friendsincode/grimnir_radio/internal/models"
	"github.com/friendsincode/grimnir_radio/internal/priority"
	"github.com/google/uuid"
	"github.com/rs/zerolog"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func setupTestDB(t *testing.T) *gorm.DB {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{
		DisableForeignKeyConstraintWhenMigrating: true,
	})
	if err != nil {
		t.Fatalf("failed to open test db: %v", err)
	}

	// Migrate tables (ignore index already exists errors)
	_ = db.AutoMigrate(
		&models.Station{},
		&models.LiveSession{},
		&models.PrioritySource{},
	)

	return db
}

func createTestStation(t *testing.T, db *gorm.DB) *models.Station {
	station := &models.Station{
		ID:   uuid.NewString(),
		Name: "Test Station",
	}
	if err := db.Create(station).Error; err != nil {
		t.Fatalf("failed to create station: %v", err)
	}
	return station
}

func createTestSession(t *testing.T, db *gorm.DB, stationID string, priority models.PriorityLevel) *models.LiveSession {
	session := &models.LiveSession{
		ID:          uuid.NewString(),
		StationID:   stationID,
		MountID:     uuid.NewString(),
		UserID:      uuid.NewString(),
		Username:    "test-dj",
		Priority:    priority,
		Token:       "test-token",
		TokenUsed:   true,
		Active:      true,
		ConnectedAt: time.Now(),
		Metadata:    make(map[string]any),
	}
	if err := db.Create(session).Error; err != nil {
		t.Fatalf("failed to create session: %v", err)
	}
	return session
}

func TestStartHandover_Success(t *testing.T) {
	db := setupTestDB(t)
	logger := zerolog.Nop()
	bus := events.NewBus()
	prioritySvc := priority.NewService(db, bus, logger)
	svc := NewService(db, prioritySvc, bus, logger)

	station := createTestStation(t, db)
	session := createTestSession(t, db, station.ID, models.PriorityLiveOverride)

	ctx := context.Background()
	result, err := svc.StartHandover(ctx, HandoverRequest{
		SessionID:       session.ID,
		StationID:       station.ID,
		MountID:         session.MountID,
		UserID:          session.UserID,
		Priority:        models.PriorityLiveOverride,
		Immediate:       false,
		FadeTimeMs:      3000,
		RollbackOnError: true,
	})

	if err != nil {
		t.Fatalf("StartHandover() failed: %v", err)
	}

	if !result.Success {
		t.Errorf("expected success=true, got false: %s", result.Error)
	}

	if result.SessionID != session.ID {
		t.Errorf("expected session_id=%s, got %s", session.ID, result.SessionID)
	}

	if result.TransitionType == "" {
		t.Error("expected non-empty transition_type")
	}

	if result.NewSource == nil {
		t.Error("expected NewSource to be set")
	} else {
		if result.NewSource.Priority != models.PriorityLiveOverride {
			t.Errorf("expected priority=%d, got %d", models.PriorityLiveOverride, result.NewSource.Priority)
		}
		if result.NewSource.SourceType != models.SourceTypeLive {
			t.Errorf("expected source_type=live, got %s", result.NewSource.SourceType)
		}
	}
}

func TestStartHandover_SessionNotFound(t *testing.T) {
	db := setupTestDB(t)
	logger := zerolog.Nop()
	bus := events.NewBus()
	prioritySvc := priority.NewService(db, bus, logger)
	svc := NewService(db, prioritySvc, bus, logger)

	station := createTestStation(t, db)

	ctx := context.Background()
	_, err := svc.StartHandover(ctx, HandoverRequest{
		SessionID:       "non-existent",
		StationID:       station.ID,
		MountID:         uuid.NewString(),
		UserID:          uuid.NewString(),
		Priority:        models.PriorityLiveOverride,
		Immediate:       false,
		FadeTimeMs:      3000,
		RollbackOnError: true,
	})

	if err == nil {
		t.Error("expected error for non-existent session")
	}
}

func TestStartHandover_InactiveSession(t *testing.T) {
	db := setupTestDB(t)
	logger := zerolog.Nop()
	bus := events.NewBus()
	prioritySvc := priority.NewService(db, bus, logger)
	svc := NewService(db, prioritySvc, bus, logger)

	station := createTestStation(t, db)
	session := createTestSession(t, db, station.ID, models.PriorityLiveOverride)

	// Mark session as inactive
	session.Active = false
	if err := db.Save(session).Error; err != nil {
		t.Fatalf("failed to update session: %v", err)
	}

	ctx := context.Background()
	_, err := svc.StartHandover(ctx, HandoverRequest{
		SessionID:       session.ID,
		StationID:       station.ID,
		MountID:         session.MountID,
		UserID:          session.UserID,
		Priority:        models.PriorityLiveOverride,
		Immediate:       false,
		FadeTimeMs:      3000,
		RollbackOnError: true,
	})

	if err == nil {
		t.Error("expected error for inactive session")
	}
}

func TestStartHandover_UserMismatch(t *testing.T) {
	db := setupTestDB(t)
	logger := zerolog.Nop()
	bus := events.NewBus()
	prioritySvc := priority.NewService(db, bus, logger)
	svc := NewService(db, prioritySvc, bus, logger)

	station := createTestStation(t, db)
	session := createTestSession(t, db, station.ID, models.PriorityLiveOverride)

	ctx := context.Background()
	_, err := svc.StartHandover(ctx, HandoverRequest{
		SessionID:       session.ID,
		StationID:       station.ID,
		MountID:         session.MountID,
		UserID:          "wrong-user-id", // Different user
		Priority:        models.PriorityLiveOverride,
		Immediate:       false,
		FadeTimeMs:      3000,
		RollbackOnError: true,
	})

	if err != ErrUnauthorized {
		t.Errorf("expected ErrUnauthorized, got %v", err)
	}
}

func TestStartHandover_ScheduledLive(t *testing.T) {
	db := setupTestDB(t)
	logger := zerolog.Nop()
	bus := events.NewBus()
	prioritySvc := priority.NewService(db, bus, logger)
	svc := NewService(db, prioritySvc, bus, logger)

	station := createTestStation(t, db)
	session := createTestSession(t, db, station.ID, models.PriorityLiveScheduled)

	ctx := context.Background()
	result, err := svc.StartHandover(ctx, HandoverRequest{
		SessionID:       session.ID,
		StationID:       station.ID,
		MountID:         session.MountID,
		UserID:          session.UserID,
		Priority:        models.PriorityLiveScheduled,
		Immediate:       false,
		FadeTimeMs:      3000,
		RollbackOnError: true,
	})

	if err != nil {
		t.Fatalf("StartHandover() failed: %v", err)
	}

	if !result.Success {
		t.Errorf("expected success=true, got false: %s", result.Error)
	}

	if result.NewSource == nil {
		t.Fatal("expected NewSource to be set")
	}

	if result.NewSource.Priority != models.PriorityLiveScheduled {
		t.Errorf("expected priority=%d, got %d", models.PriorityLiveScheduled, result.NewSource.Priority)
	}
}

func TestStartHandover_InvalidPriority(t *testing.T) {
	db := setupTestDB(t)
	logger := zerolog.Nop()
	bus := events.NewBus()
	prioritySvc := priority.NewService(db, bus, logger)
	svc := NewService(db, prioritySvc, bus, logger)

	station := createTestStation(t, db)
	session := createTestSession(t, db, station.ID, models.PriorityLiveOverride)

	ctx := context.Background()
	_, err := svc.StartHandover(ctx, HandoverRequest{
		SessionID:       session.ID,
		StationID:       station.ID,
		MountID:         session.MountID,
		UserID:          session.UserID,
		Priority:        models.PriorityAutomation, // Invalid for live
		Immediate:       false,
		FadeTimeMs:      3000,
		RollbackOnError: true,
	})

	if err == nil {
		t.Error("expected error for invalid priority")
	}
}

func TestReleaseHandover_Success(t *testing.T) {
	db := setupTestDB(t)
	logger := zerolog.Nop()
	bus := events.NewBus()
	prioritySvc := priority.NewService(db, bus, logger)
	svc := NewService(db, prioritySvc, bus, logger)

	station := createTestStation(t, db)
	session := createTestSession(t, db, station.ID, models.PriorityLiveOverride)

	// First start handover to create priority source
	ctx := context.Background()
	_, err := svc.StartHandover(ctx, HandoverRequest{
		SessionID:       session.ID,
		StationID:       station.ID,
		MountID:         session.MountID,
		UserID:          session.UserID,
		Priority:        models.PriorityLiveOverride,
		Immediate:       false,
		FadeTimeMs:      3000,
		RollbackOnError: true,
	})
	if err != nil {
		t.Fatalf("StartHandover() failed: %v", err)
	}

	// Now release it
	err = svc.ReleaseHandover(ctx, session.ID)
	if err != nil {
		t.Errorf("ReleaseHandover() failed: %v", err)
	}

	// Verify priority source was released
	current, err := prioritySvc.GetCurrent(ctx, station.ID)
	// After release, there should be no active priority sources
	if err == nil && current != nil && current.Priority == models.PriorityLiveOverride {
		t.Error("priority source should have been released")
	}
}

func TestReleaseHandover_SessionNotFound(t *testing.T) {
	db := setupTestDB(t)
	logger := zerolog.Nop()
	bus := events.NewBus()
	prioritySvc := priority.NewService(db, bus, logger)
	svc := NewService(db, prioritySvc, bus, logger)

	ctx := context.Background()
	err := svc.ReleaseHandover(ctx, "non-existent")

	if err == nil {
		t.Error("expected error for non-existent session")
	}
}

func TestHandover_EventPublishing(t *testing.T) {
	db := setupTestDB(t)
	logger := zerolog.Nop()
	bus := events.NewBus()
	prioritySvc := priority.NewService(db, bus, logger)
	svc := NewService(db, prioritySvc, bus, logger)

	station := createTestStation(t, db)
	session := createTestSession(t, db, station.ID, models.PriorityLiveOverride)

	// Subscribe to handover events
	handoverSub := bus.Subscribe(events.EventLiveHandover)
	releasedSub := bus.Subscribe(events.EventLiveReleased)

	ctx := context.Background()

	// Start handover
	_, err := svc.StartHandover(ctx, HandoverRequest{
		SessionID:       session.ID,
		StationID:       station.ID,
		MountID:         session.MountID,
		UserID:          session.UserID,
		Priority:        models.PriorityLiveOverride,
		Immediate:       false,
		FadeTimeMs:      3000,
		RollbackOnError: true,
	})
	if err != nil {
		t.Fatalf("StartHandover() failed: %v", err)
	}

	// Check for handover event
	select {
	case payload := <-handoverSub:
		if payload["session_id"] != session.ID {
			t.Errorf("expected session_id=%s, got %v", session.ID, payload["session_id"])
		}
		if payload["station_id"] != station.ID {
			t.Errorf("expected station_id=%s, got %v", station.ID, payload["station_id"])
		}
	case <-time.After(100 * time.Millisecond):
		t.Error("timeout waiting for handover event")
	}

	// Release handover
	err = svc.ReleaseHandover(ctx, session.ID)
	if err != nil {
		t.Fatalf("ReleaseHandover() failed: %v", err)
	}

	// Check for released event
	select {
	case payload := <-releasedSub:
		if payload["session_id"] != session.ID {
			t.Errorf("expected session_id=%s, got %v", session.ID, payload["session_id"])
		}
		if payload["station_id"] != station.ID {
			t.Errorf("expected station_id=%s, got %v", station.ID, payload["station_id"])
		}
	case <-time.After(100 * time.Millisecond):
		t.Error("timeout waiting for released event")
	}
}

func TestHandover_MetadataUpdate(t *testing.T) {
	db := setupTestDB(t)
	logger := zerolog.Nop()
	bus := events.NewBus()
	prioritySvc := priority.NewService(db, bus, logger)
	svc := NewService(db, prioritySvc, bus, logger)

	station := createTestStation(t, db)
	session := createTestSession(t, db, station.ID, models.PriorityLiveOverride)

	ctx := context.Background()
	result, err := svc.StartHandover(ctx, HandoverRequest{
		SessionID:       session.ID,
		StationID:       station.ID,
		MountID:         session.MountID,
		UserID:          session.UserID,
		Priority:        models.PriorityLiveOverride,
		Immediate:       false,
		FadeTimeMs:      3000,
		RollbackOnError: true,
	})
	if err != nil {
		t.Fatalf("StartHandover() failed: %v", err)
	}

	// Reload session to check metadata
	var updatedSession models.LiveSession
	if err := db.First(&updatedSession, "id = ?", session.ID).Error; err != nil {
		t.Fatalf("failed to reload session: %v", err)
	}

	// Check metadata was updated
	if updatedSession.Metadata == nil {
		t.Fatal("expected metadata to be set")
	}

	handoverCompleted, ok := updatedSession.Metadata["handover_completed"].(bool)
	if !ok || !handoverCompleted {
		t.Error("expected handover_completed=true in metadata")
	}

	if updatedSession.Metadata["transition_type"] != result.TransitionType {
		t.Errorf("expected transition_type=%s in metadata", result.TransitionType)
	}
}

func BenchmarkStartHandover(b *testing.B) {
	db := setupTestDB(&testing.T{})
	logger := zerolog.Nop()
	bus := events.NewBus()
	prioritySvc := priority.NewService(db, bus, logger)
	svc := NewService(db, prioritySvc, bus, logger)

	station := createTestStation(&testing.T{}, db)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		session := createTestSession(&testing.T{}, db, station.ID, models.PriorityLiveOverride)

		ctx := context.Background()
		_, _ = svc.StartHandover(ctx, HandoverRequest{
			SessionID:       session.ID,
			StationID:       station.ID,
			MountID:         session.MountID,
			UserID:          session.UserID,
			Priority:        models.PriorityLiveOverride,
			Immediate:       false,
			FadeTimeMs:      3000,
			RollbackOnError: true,
		})
	}
}
