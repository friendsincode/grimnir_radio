package webstream

import (
	"context"
	"testing"
	"time"

	"github.com/friendsincode/grimnir_radio/internal/events"
	"github.com/friendsincode/grimnir_radio/internal/models"
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

	// Migrate tables (ignore any duplicate index errors)
	_ = db.AutoMigrate(&models.Webstream{})

	return db
}

func createTestWebstream(t *testing.T, db *gorm.DB, urls []string) *models.Webstream {
	ws := &models.Webstream{
		ID:                  uuid.NewString(),
		StationID:           uuid.NewString(),
		Name:                "Test Webstream",
		Description:         "Test webstream for integration testing",
		URLs:                urls,
		HealthCheckEnabled:  true,
		HealthCheckInterval: 30 * time.Second,
		HealthCheckTimeout:  5 * time.Second,
		HealthCheckMethod:   "HEAD",
		FailoverEnabled:     true,
		FailoverGraceMs:     5000,
		AutoRecoverEnabled:  true,
		PreflightCheck:      false, // Disable for testing
		BufferSizeMS:        2000,
		ReconnectDelayMS:    1000,
		MaxReconnectAttempts: 5,
		PassthroughMetadata: true,
		OverrideMetadata:    false,
		Active:              true,
		CurrentURL:          urls[0],
		CurrentIndex:        0,
		HealthStatus:        "healthy",
	}

	if err := db.Create(ws).Error; err != nil {
		t.Fatalf("failed to create webstream: %v", err)
	}

	return ws
}

func TestWebstream_Create(t *testing.T) {
	db := setupTestDB(t)
	logger := zerolog.Nop()
	bus := events.NewBus()
	svc := NewService(db, bus, logger)

	urls := []string{
		"http://primary.example.com/stream.mp3",
		"http://backup.example.com/stream.mp3",
	}

	ws := &models.Webstream{
		ID:                  uuid.NewString(),
		StationID:           uuid.NewString(),
		Name:                "Test Stream",
		URLs:                urls,
		HealthCheckEnabled:  true,
		HealthCheckInterval: 30 * time.Second,
		HealthCheckTimeout:  5 * time.Second,
		FailoverEnabled:     true,
		FailoverGraceMs:     5000,
	}

	ctx := context.Background()
	err := svc.CreateWebstream(ctx, ws)
	if err != nil {
		t.Fatalf("CreateWebstream() failed: %v", err)
	}

	// Verify it was created
	retrieved, err := svc.GetWebstream(ctx, ws.ID)
	if err != nil {
		t.Fatalf("GetWebstream() failed: %v", err)
	}

	if retrieved.Name != ws.Name {
		t.Errorf("expected name=%s, got %s", ws.Name, retrieved.Name)
	}

	if len(retrieved.URLs) != len(urls) {
		t.Errorf("expected %d URLs, got %d", len(urls), len(retrieved.URLs))
	}
}

func TestWebstream_GetPrimaryURL(t *testing.T) {
	db := setupTestDB(t)
	urls := []string{
		"http://primary.example.com/stream.mp3",
		"http://backup.example.com/stream.mp3",
	}

	ws := createTestWebstream(t, db, urls)

	primary := ws.GetPrimaryURL()
	if primary != urls[0] {
		t.Errorf("expected primary=%s, got %s", urls[0], primary)
	}
}

func TestWebstream_GetCurrentURL(t *testing.T) {
	db := setupTestDB(t)
	urls := []string{
		"http://primary.example.com/stream.mp3",
		"http://backup.example.com/stream.mp3",
	}

	ws := createTestWebstream(t, db, urls)

	current := ws.GetCurrentURL()
	if current != urls[0] {
		t.Errorf("expected current=%s, got %s", urls[0], current)
	}
}

func TestWebstream_GetNextFailoverURL(t *testing.T) {
	db := setupTestDB(t)
	urls := []string{
		"http://primary.example.com/stream.mp3",
		"http://backup.example.com/stream.mp3",
		"http://backup2.example.com/stream.mp3",
	}

	ws := createTestWebstream(t, db, urls)

	// Currently at index 0, next should be index 1
	next := ws.GetNextFailoverURL()
	if next != urls[1] {
		t.Errorf("expected next=%s, got %s", urls[1], next)
	}

	// Advance to index 1
	ws.CurrentIndex = 1
	ws.CurrentURL = urls[1]

	// Next should be index 2
	next = ws.GetNextFailoverURL()
	if next != urls[2] {
		t.Errorf("expected next=%s, got %s", urls[2], next)
	}
}

func TestWebstream_GetNextFailoverURL_WrapAround(t *testing.T) {
	db := setupTestDB(t)
	urls := []string{
		"http://primary.example.com/stream.mp3",
		"http://backup.example.com/stream.mp3",
	}

	ws := createTestWebstream(t, db, urls)

	// At last URL (index 1), with auto-recover enabled
	ws.CurrentIndex = 1
	ws.CurrentURL = urls[1]

	// Should wrap around to primary
	next := ws.GetNextFailoverURL()
	if next != urls[0] {
		t.Errorf("expected next=%s (wrap around), got %s", urls[0], next)
	}
}

func TestWebstream_FailoverToNext(t *testing.T) {
	db := setupTestDB(t)
	urls := []string{
		"http://primary.example.com/stream.mp3",
		"http://backup.example.com/stream.mp3",
	}

	ws := createTestWebstream(t, db, urls)

	// Initially at primary (index 0)
	if ws.CurrentIndex != 0 {
		t.Errorf("expected initial index=0, got %d", ws.CurrentIndex)
	}

	// Failover to next
	success := ws.FailoverToNext()
	if !success {
		t.Error("FailoverToNext() should succeed")
	}

	// Should now be at backup (index 1)
	if ws.CurrentIndex != 1 {
		t.Errorf("expected index=1 after failover, got %d", ws.CurrentIndex)
	}

	if ws.CurrentURL != urls[1] {
		t.Errorf("expected current_url=%s after failover, got %s", urls[1], ws.CurrentURL)
	}
}

func TestWebstream_ResetToPrimary(t *testing.T) {
	db := setupTestDB(t)
	urls := []string{
		"http://primary.example.com/stream.mp3",
		"http://backup.example.com/stream.mp3",
	}

	ws := createTestWebstream(t, db, urls)

	// Failover to backup
	ws.CurrentIndex = 1
	ws.CurrentURL = urls[1]

	// Reset to primary
	ws.ResetToPrimary()

	if ws.CurrentIndex != 0 {
		t.Errorf("expected index=0 after reset, got %d", ws.CurrentIndex)
	}

	if ws.CurrentURL != urls[0] {
		t.Errorf("expected current_url=%s after reset, got %s", urls[0], ws.CurrentURL)
	}
}

func TestWebstream_MarkHealthy(t *testing.T) {
	db := setupTestDB(t)
	urls := []string{"http://example.com/stream.mp3"}
	ws := createTestWebstream(t, db, urls)

	ws.HealthStatus = "unhealthy"
	ws.MarkHealthy()

	if ws.HealthStatus != "healthy" {
		t.Errorf("expected health_status=healthy, got %s", ws.HealthStatus)
	}

	if ws.LastHealthCheck == nil {
		t.Error("expected LastHealthCheck to be set")
	}
}

func TestWebstream_MarkUnhealthy(t *testing.T) {
	db := setupTestDB(t)
	urls := []string{"http://example.com/stream.mp3"}
	ws := createTestWebstream(t, db, urls)

	ws.HealthStatus = "healthy"
	ws.MarkUnhealthy()

	if ws.HealthStatus != "unhealthy" {
		t.Errorf("expected health_status=unhealthy, got %s", ws.HealthStatus)
	}

	if ws.LastHealthCheck == nil {
		t.Error("expected LastHealthCheck to be set")
	}
}

func TestWebstream_IsHealthy(t *testing.T) {
	db := setupTestDB(t)
	urls := []string{"http://example.com/stream.mp3"}
	ws := createTestWebstream(t, db, urls)

	ws.HealthStatus = "healthy"
	if !ws.IsHealthy() {
		t.Error("expected IsHealthy()=true for healthy status")
	}

	ws.HealthStatus = "unhealthy"
	if ws.IsHealthy() {
		t.Error("expected IsHealthy()=false for unhealthy status")
	}

	ws.HealthStatus = "degraded"
	if ws.IsHealthy() {
		t.Error("expected IsHealthy()=false for degraded status")
	}
}

func TestWebstreamService_TriggerFailover(t *testing.T) {
	db := setupTestDB(t)
	logger := zerolog.Nop()
	bus := events.NewBus()
	svc := NewService(db, bus, logger)

	urls := []string{
		"http://primary.example.com/stream.mp3",
		"http://backup.example.com/stream.mp3",
	}

	ws := createTestWebstream(t, db, urls)

	// Subscribe to failover events
	failoverSub := bus.Subscribe(events.EventWebstreamFailover)

	ctx := context.Background()
	err := svc.TriggerFailover(ctx, ws.ID)
	if err != nil {
		t.Fatalf("TriggerFailover() failed: %v", err)
	}

	// Verify failover occurred
	updated, err := svc.GetWebstream(ctx, ws.ID)
	if err != nil {
		t.Fatalf("GetWebstream() failed: %v", err)
	}

	if updated.CurrentIndex != 1 {
		t.Errorf("expected index=1 after failover, got %d", updated.CurrentIndex)
	}

	if updated.CurrentURL != urls[1] {
		t.Errorf("expected current_url=%s after failover, got %s", urls[1], updated.CurrentURL)
	}

	// Check for failover event
	select {
	case payload := <-failoverSub:
		if payload["webstream_id"] != ws.ID {
			t.Errorf("expected webstream_id=%s in event, got %v", ws.ID, payload["webstream_id"])
		}
	case <-time.After(100 * time.Millisecond):
		t.Error("timeout waiting for failover event")
	}
}

func TestWebstreamService_ResetToPrimary(t *testing.T) {
	db := setupTestDB(t)
	logger := zerolog.Nop()
	bus := events.NewBus()
	svc := NewService(db, bus, logger)

	urls := []string{
		"http://primary.example.com/stream.mp3",
		"http://backup.example.com/stream.mp3",
	}

	ws := createTestWebstream(t, db, urls)

	// Subscribe to recovered events
	recoveredSub := bus.Subscribe(events.EventWebstreamRecovered)

	// First failover to backup
	ctx := context.Background()
	err := svc.TriggerFailover(ctx, ws.ID)
	if err != nil {
		t.Fatalf("TriggerFailover() failed: %v", err)
	}

	// Drain the failover event
	select {
	case <-recoveredSub:
	default:
	}

	// Now reset to primary
	err = svc.ResetToPrimary(ctx, ws.ID)
	if err != nil {
		t.Fatalf("ResetToPrimary() failed: %v", err)
	}

	// Verify reset occurred
	updated, err := svc.GetWebstream(ctx, ws.ID)
	if err != nil {
		t.Fatalf("GetWebstream() failed: %v", err)
	}

	if updated.CurrentIndex != 0 {
		t.Errorf("expected index=0 after reset, got %d", updated.CurrentIndex)
	}

	if updated.CurrentURL != urls[0] {
		t.Errorf("expected current_url=%s after reset, got %s", urls[0], updated.CurrentURL)
	}

	// Check for recovered event
	select {
	case payload := <-recoveredSub:
		if payload["webstream_id"] != ws.ID {
			t.Errorf("expected webstream_id=%s in event, got %v", ws.ID, payload["webstream_id"])
		}
	case <-time.After(100 * time.Millisecond):
		t.Error("timeout waiting for recovered event")
	}
}

func TestWebstreamService_ListWebstreams(t *testing.T) {
	db := setupTestDB(t)
	logger := zerolog.Nop()
	bus := events.NewBus()
	svc := NewService(db, bus, logger)

	stationID := uuid.NewString()

	// Create multiple webstreams for the same station
	urls := []string{"http://example.com/stream1.mp3"}
	ws1 := &models.Webstream{
		ID:                  uuid.NewString(),
		StationID:           stationID,
		Name:                "Stream 1",
		URLs:                urls,
		HealthCheckEnabled:  true,
		HealthCheckInterval: 30 * time.Second,
	}
	ws2 := &models.Webstream{
		ID:                  uuid.NewString(),
		StationID:           stationID,
		Name:                "Stream 2",
		URLs:                urls,
		HealthCheckEnabled:  true,
		HealthCheckInterval: 30 * time.Second,
	}

	ctx := context.Background()
	if err := svc.CreateWebstream(ctx, ws1); err != nil {
		t.Fatalf("CreateWebstream() failed: %v", err)
	}
	if err := svc.CreateWebstream(ctx, ws2); err != nil {
		t.Fatalf("CreateWebstream() failed: %v", err)
	}

	// List webstreams for station
	webstreams, err := svc.ListWebstreams(ctx, stationID)
	if err != nil {
		t.Fatalf("ListWebstreams() failed: %v", err)
	}

	if len(webstreams) != 2 {
		t.Errorf("expected 2 webstreams, got %d", len(webstreams))
	}
}

func TestWebstreamService_UpdateWebstream(t *testing.T) {
	db := setupTestDB(t)
	logger := zerolog.Nop()
	bus := events.NewBus()
	svc := NewService(db, bus, logger)

	urls := []string{"http://example.com/stream.mp3"}
	ws := createTestWebstream(t, db, urls)

	ctx := context.Background()
	updates := map[string]any{
		"name":        "Updated Name",
		"description": "Updated description",
	}

	err := svc.UpdateWebstream(ctx, ws.ID, updates)
	if err != nil {
		t.Fatalf("UpdateWebstream() failed: %v", err)
	}

	// Verify updates
	updated, err := svc.GetWebstream(ctx, ws.ID)
	if err != nil {
		t.Fatalf("GetWebstream() failed: %v", err)
	}

	if updated.Name != "Updated Name" {
		t.Errorf("expected name='Updated Name', got %s", updated.Name)
	}

	if updated.Description != "Updated description" {
		t.Errorf("expected description='Updated description', got %s", updated.Description)
	}
}

func TestWebstreamService_DeleteWebstream(t *testing.T) {
	db := setupTestDB(t)
	logger := zerolog.Nop()
	bus := events.NewBus()
	svc := NewService(db, bus, logger)

	urls := []string{"http://example.com/stream.mp3"}
	ws := createTestWebstream(t, db, urls)

	ctx := context.Background()
	err := svc.DeleteWebstream(ctx, ws.ID)
	if err != nil {
		t.Fatalf("DeleteWebstream() failed: %v", err)
	}

	// Verify deletion
	_, err = svc.GetWebstream(ctx, ws.ID)
	if err == nil {
		t.Error("expected error when getting deleted webstream")
	}
}

func BenchmarkWebstream_FailoverToNext(b *testing.B) {
	db := setupTestDB(&testing.T{})
	urls := []string{
		"http://primary.example.com/stream.mp3",
		"http://backup.example.com/stream.mp3",
		"http://backup2.example.com/stream.mp3",
	}

	ws := createTestWebstream(&testing.T{}, db, urls)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		ws.FailoverToNext()
		if ws.CurrentIndex >= len(urls)-1 {
			ws.ResetToPrimary()
		}
	}
}
