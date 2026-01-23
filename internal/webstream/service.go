package webstream

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/friendsincode/grimnir_radio/internal/events"
	"github.com/friendsincode/grimnir_radio/internal/models"
	"github.com/google/uuid"
	"github.com/rs/zerolog"
	"gorm.io/gorm"
)

var (
	// ErrWebstreamNotFound indicates the webstream was not found.
	ErrWebstreamNotFound = errors.New("webstream not found")

	// ErrNoURLsConfigured indicates the webstream has no URLs.
	ErrNoURLsConfigured = errors.New("no URLs configured")

	// ErrInvalidURL indicates a URL is invalid or unreachable.
	ErrInvalidURL = errors.New("invalid or unreachable URL")
)

// Service manages webstreams with health checking and failover.
type Service struct {
	db     *gorm.DB
	bus    *events.Bus
	logger zerolog.Logger

	mu             sync.RWMutex
	healthCheckers map[string]*HealthChecker // webstream_id -> health checker
	ctx            context.Context
	cancel         context.CancelFunc
	wg             sync.WaitGroup
}

// NewService creates a new webstream service.
func NewService(db *gorm.DB, bus *events.Bus, logger zerolog.Logger) *Service {
	ctx, cancel := context.WithCancel(context.Background())

	svc := &Service{
		db:             db,
		bus:            bus,
		logger:         logger.With().Str("component", "webstream").Logger(),
		healthCheckers: make(map[string]*HealthChecker),
		ctx:            ctx,
		cancel:         cancel,
	}

	// Start health check coordinator
	svc.wg.Add(1)
	go svc.healthCheckCoordinator()

	return svc
}

// CreateWebstream creates a new webstream.
func (s *Service) CreateWebstream(ctx context.Context, ws *models.Webstream) error {
	if len(ws.URLs) == 0 {
		return ErrNoURLsConfigured
	}

	// Generate ID if not set
	if ws.ID == "" {
		ws.ID = uuid.New().String()
	}

	// Set defaults
	if ws.HealthCheckInterval == 0 {
		ws.HealthCheckInterval = 30 * time.Second
	}
	if ws.HealthCheckTimeout == 0 {
		ws.HealthCheckTimeout = 5 * time.Second
	}
	if ws.HealthCheckMethod == "" {
		ws.HealthCheckMethod = "HEAD"
	}
	if ws.FailoverGraceMs == 0 {
		ws.FailoverGraceMs = 5000
	}
	if ws.BufferSizeMS == 0 {
		ws.BufferSizeMS = 2000
	}
	if ws.ReconnectDelayMS == 0 {
		ws.ReconnectDelayMS = 1000
	}
	if ws.MaxReconnectAttempts == 0 {
		ws.MaxReconnectAttempts = 5
	}

	// Initialize state
	ws.CurrentURL = ws.GetPrimaryURL()
	ws.CurrentIndex = 0
	ws.HealthStatus = "unknown"

	if err := s.db.WithContext(ctx).Create(ws).Error; err != nil {
		return fmt.Errorf("create webstream: %w", err)
	}

	// Optionally run preflight check
	if ws.PreflightCheck {
		if err := s.checkURL(ws.GetPrimaryURL(), ws.HealthCheckMethod, ws.HealthCheckTimeout); err != nil {
			s.logger.Warn().Err(err).Str("webstream_id", ws.ID).Msg("preflight check failed")
			ws.MarkUnhealthy()
			s.db.WithContext(ctx).Save(ws)
		} else {
			ws.MarkHealthy()
			s.db.WithContext(ctx).Save(ws)
		}
	}

	// Start health checker if enabled
	if ws.HealthCheckEnabled {
		s.startHealthChecker(ws.ID)
	}

	s.logger.Info().
		Str("webstream_id", ws.ID).
		Str("name", ws.Name).
		Int("url_count", len(ws.URLs)).
		Msg("webstream created")

	return nil
}

// UpdateWebstream updates an existing webstream.
func (s *Service) UpdateWebstream(ctx context.Context, id string, updates map[string]any) error {
	var ws models.Webstream
	if err := s.db.WithContext(ctx).First(&ws, "id = ?", id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return ErrWebstreamNotFound
		}
		return fmt.Errorf("query webstream: %w", err)
	}

	if err := s.db.WithContext(ctx).Model(&ws).Updates(updates).Error; err != nil {
		return fmt.Errorf("update webstream: %w", err)
	}

	// Restart health checker if settings changed
	if _, ok := updates["health_check_enabled"]; ok {
		s.stopHealthChecker(id)
		if ws.HealthCheckEnabled {
			s.startHealthChecker(id)
		}
	}

	s.logger.Info().Str("webstream_id", id).Msg("webstream updated")

	return nil
}

// DeleteWebstream deletes a webstream.
func (s *Service) DeleteWebstream(ctx context.Context, id string) error {
	// Stop health checker first
	s.stopHealthChecker(id)

	if err := s.db.WithContext(ctx).Delete(&models.Webstream{}, "id = ?", id).Error; err != nil {
		return fmt.Errorf("delete webstream: %w", err)
	}

	s.logger.Info().Str("webstream_id", id).Msg("webstream deleted")

	return nil
}

// GetWebstream retrieves a webstream by ID.
func (s *Service) GetWebstream(ctx context.Context, id string) (*models.Webstream, error) {
	var ws models.Webstream
	if err := s.db.WithContext(ctx).First(&ws, "id = ?", id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrWebstreamNotFound
		}
		return nil, fmt.Errorf("query webstream: %w", err)
	}

	return &ws, nil
}

// ListWebstreams lists all webstreams for a station.
func (s *Service) ListWebstreams(ctx context.Context, stationID string) ([]models.Webstream, error) {
	var webstreams []models.Webstream
	query := s.db.WithContext(ctx)

	if stationID != "" {
		query = query.Where("station_id = ?", stationID)
	}

	if err := query.Order("name ASC").Find(&webstreams).Error; err != nil {
		return nil, fmt.Errorf("query webstreams: %w", err)
	}

	return webstreams, nil
}

// TriggerFailover manually triggers failover to the next URL.
func (s *Service) TriggerFailover(ctx context.Context, id string) error {
	var ws models.Webstream
	if err := s.db.WithContext(ctx).First(&ws, "id = ?", id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return ErrWebstreamNotFound
		}
		return fmt.Errorf("query webstream: %w", err)
	}

	if !ws.FailoverEnabled {
		return fmt.Errorf("failover not enabled for this webstream")
	}

	nextURL := ws.GetNextFailoverURL()
	if nextURL == "" {
		return fmt.Errorf("no failover URLs available")
	}

	// Attempt to fail over
	if ws.FailoverToNext() {
		if err := s.db.WithContext(ctx).Save(&ws).Error; err != nil {
			return fmt.Errorf("save failover state: %w", err)
		}

		s.logger.Warn().
			Str("webstream_id", id).
			Str("from_url", ws.URLs[ws.CurrentIndex-1]).
			Str("to_url", ws.CurrentURL).
			Msg("manual failover triggered")

		// Emit failover event
		s.bus.Publish(events.EventWebstreamFailover, events.Payload{
			"webstream_id":  id,
			"station_id":    ws.StationID,
			"from_url":      ws.URLs[ws.CurrentIndex-1],
			"to_url":        ws.CurrentURL,
			"current_index": ws.CurrentIndex,
			"manual":        true,
		})

		return nil
	}

	return fmt.Errorf("failover failed")
}

// ResetToPrimary resets a webstream to use its primary URL.
func (s *Service) ResetToPrimary(ctx context.Context, id string) error {
	var ws models.Webstream
	if err := s.db.WithContext(ctx).First(&ws, "id = ?", id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return ErrWebstreamNotFound
		}
		return fmt.Errorf("query webstream: %w", err)
	}

	oldURL := ws.CurrentURL
	ws.ResetToPrimary()

	if err := s.db.WithContext(ctx).Save(&ws).Error; err != nil {
		return fmt.Errorf("save reset state: %w", err)
	}

	s.logger.Info().
		Str("webstream_id", id).
		Str("from_url", oldURL).
		Str("to_url", ws.CurrentURL).
		Msg("reset to primary URL")

	// Emit recovery event
	s.bus.Publish(events.EventWebstreamRecovered, events.Payload{
		"webstream_id": id,
		"station_id":   ws.StationID,
		"url":          ws.CurrentURL,
	})

	return nil
}

// Shutdown stops the service and all health checkers.
func (s *Service) Shutdown() error {
	s.logger.Info().Msg("shutting down webstream service")

	s.cancel()
	s.wg.Wait()

	s.mu.Lock()
	for id := range s.healthCheckers {
		s.stopHealthCheckerLocked(id)
	}
	s.mu.Unlock()

	s.logger.Info().Msg("webstream service shutdown complete")

	return nil
}

// Health check implementation

func (s *Service) healthCheckCoordinator() {
	defer s.wg.Done()

	ticker := time.NewTicker(10 * time.Second) // Check which health checkers need to run
	defer ticker.Stop()

	for {
		select {
		case <-s.ctx.Done():
			return
		case <-ticker.C:
			s.ensureHealthCheckers()
		}
	}
}

func (s *Service) ensureHealthCheckers() {
	// Get all webstreams with health checks enabled
	var webstreams []models.Webstream
	if err := s.db.Where("health_check_enabled = ?", true).Find(&webstreams).Error; err != nil {
		s.logger.Error().Err(err).Msg("failed to query webstreams for health checks")
		return
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	// Start health checkers for new webstreams
	for _, ws := range webstreams {
		if _, exists := s.healthCheckers[ws.ID]; !exists {
			s.startHealthCheckerLocked(ws.ID)
		}
	}

	// Stop health checkers for deleted webstreams
	for id := range s.healthCheckers {
		found := false
		for _, ws := range webstreams {
			if ws.ID == id {
				found = true
				break
			}
		}
		if !found {
			s.stopHealthCheckerLocked(id)
		}
	}
}

func (s *Service) startHealthChecker(id string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.startHealthCheckerLocked(id)
}

func (s *Service) startHealthCheckerLocked(id string) {
	if _, exists := s.healthCheckers[id]; exists {
		return
	}

	checker := NewHealthChecker(id, s.db, s.bus, s.logger)
	s.healthCheckers[id] = checker

	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		checker.Run(s.ctx)
	}()

	s.logger.Debug().Str("webstream_id", id).Msg("started health checker")
}

func (s *Service) stopHealthChecker(id string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.stopHealthCheckerLocked(id)
}

func (s *Service) stopHealthCheckerLocked(id string) {
	if checker, exists := s.healthCheckers[id]; exists {
		checker.Stop()
		delete(s.healthCheckers, id)
		s.logger.Debug().Str("webstream_id", id).Msg("stopped health checker")
	}
}

func (s *Service) checkURL(url, method string, timeout time.Duration) error {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, method, url, nil)
	if err != nil {
		return fmt.Errorf("create request: %w", err)
	}

	req.Header.Set("User-Agent", "Grimnir-Radio/1.0")
	req.Header.Set("Icy-MetaData", "1")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		return fmt.Errorf("HTTP %d", resp.StatusCode)
	}

	return nil
}
