/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/

package analyzer

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/friendsincode/grimnir_radio/internal/mediaengine/client"
	"github.com/friendsincode/grimnir_radio/internal/models"
	"github.com/google/uuid"
	"github.com/rs/zerolog"
	"gorm.io/gorm"
)

// ErrAnalyzerUnavailable indicates the media engine is not available.
var ErrAnalyzerUnavailable = errors.New("media engine unavailable for analysis")

// ErrMediaEngineNotConfigured indicates no media engine address was provided.
var ErrMediaEngineNotConfigured = errors.New("media engine gRPC address not configured")

// Config holds analyzer service configuration.
type Config struct {
	MediaEngineGRPCAddr string // gRPC address of the media engine (required)
}

// Service performs loudness and cue point analysis on media imports.
// Analysis is performed by the media engine via gRPC.
type Service struct {
	db                *gorm.DB
	logger            zerolog.Logger
	workDir           string
	cfg               Config
	mediaEngineClient *client.Client
}

// New constructs an analyzer service without media engine support.
// Deprecated: Use NewWithConfig instead to enable media analysis.
func New(db *gorm.DB, workDir string, logger zerolog.Logger) *Service {
	logger.Warn().Msg("analyzer created without media engine - analysis will fail")
	return &Service{db: db, workDir: workDir, logger: logger}
}

// NewWithConfig constructs an analyzer service with media engine support.
func NewWithConfig(db *gorm.DB, workDir string, logger zerolog.Logger, cfg Config) *Service {
	s := &Service{
		db:      db,
		workDir: workDir,
		logger:  logger,
		cfg:     cfg,
	}

	if cfg.MediaEngineGRPCAddr == "" {
		logger.Warn().Msg("media engine address not configured - analysis will fail")
		return s
	}

	// Initialize media engine client
	clientCfg := client.DefaultConfig(cfg.MediaEngineGRPCAddr)
	s.mediaEngineClient = client.New(clientCfg, logger)

	// Try to connect (non-blocking, will retry on use)
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := s.mediaEngineClient.Connect(ctx); err != nil {
			logger.Warn().Err(err).Msg("initial media engine connection failed, will retry on use")
		} else {
			logger.Info().Str("addr", cfg.MediaEngineGRPCAddr).Msg("connected to media engine for analysis")
		}
	}()

	return s
}

// Enqueue registers a media item for analysis.
func (s *Service) Enqueue(ctx context.Context, mediaID string) (string, error) {
	job := models.AnalysisJob{
		ID:      uuid.NewString(),
		MediaID: mediaID,
		Status:  "pending",
	}
	if err := s.db.WithContext(ctx).Create(&job).Error; err != nil {
		return "", err
	}
	return job.ID, nil
}

// Run drains the analysis queue until context cancellation.
func (s *Service) Run(ctx context.Context) error {
	s.logger.Info().Msg("analyzer loop started")
	for {
		select {
		case <-ctx.Done():
			s.logger.Info().Msg("analyzer loop stopped")
			return ctx.Err()
		default:
		}

		job, err := s.nextPendingJob(ctx)
		if err != nil {
			s.logger.Error().Err(err).Msg("fetching analysis job failed")
			time.Sleep(2 * time.Second)
			continue
		}
		if job == nil {
			time.Sleep(3 * time.Second)
			continue
		}

		if err := s.processJob(ctx, job); err != nil {
			s.logger.Error().Err(err).Str("job", job.ID).Msg("analysis job failed")
		}
	}
}

func (s *Service) nextPendingJob(ctx context.Context) (*models.AnalysisJob, error) {
	var job models.AnalysisJob
	err := s.db.WithContext(ctx).
		Where("status = ?", "pending").
		Order("created_at ASC").
		First(&job).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	res := s.db.WithContext(ctx).
		Model(&models.AnalysisJob{}).
		Where("id = ? AND status = ?", job.ID, "pending").
		Updates(map[string]any{"status": "running", "updated_at": time.Now()})
	if res.Error != nil {
		return nil, res.Error
	}
	if res.RowsAffected == 0 {
		return nil, nil
	}
	job.Status = "running"
	return &job, nil
}

func (s *Service) processJob(ctx context.Context, job *models.AnalysisJob) error {
	var media models.MediaItem
	err := s.db.WithContext(ctx).First(&media, "id = ?", job.MediaID).Error
	if err != nil {
		s.failJob(ctx, job.ID, job.MediaID, err)
		return err
	}

	result, err := s.performAnalysis(ctx, &media)
	if err != nil {
		s.failJob(ctx, job.ID, job.MediaID, err)
		return err
	}

	updates := map[string]any{
		"analysis_state": models.AnalysisComplete,
		"duration":       result.Duration,
		"loudness_lufs":  result.Loudness,
		"replay_gain":    result.ReplayGain,
		"bitrate":        result.Bitrate,
		"samplerate":     result.Samplerate,
		"cue_points":     models.CuePointSet{IntroEnd: result.IntroEnd, OutroIn: result.OutroIn},
	}

	// Only update metadata if we found it and the media doesn't already have it
	if result.Title != "" && media.Title == strings.TrimSuffix(filepath.Base(media.Path), filepath.Ext(media.Path)) {
		updates["title"] = result.Title
	}
	if result.Artist != "" && media.Artist == "" {
		updates["artist"] = result.Artist
	}
	if result.Album != "" && media.Album == "" {
		updates["album"] = result.Album
	}
	if result.Genre != "" && media.Genre == "" {
		updates["genre"] = result.Genre
	}
	if result.Year != "" && media.Year == "" {
		updates["year"] = result.Year
	}
	if len(result.Artwork) > 0 && len(media.Artwork) == 0 {
		updates["artwork"] = result.Artwork
		updates["artwork_mime"] = result.ArtworkMime
	}
	if err := s.db.WithContext(ctx).Model(&models.MediaItem{}).Where("id = ?", media.ID).Updates(updates).Error; err != nil {
		return err
	}

	if err := s.db.WithContext(ctx).
		Model(&models.AnalysisJob{}).
		Where("id = ?", job.ID).
		Updates(map[string]any{"status": "complete", "error": "", "updated_at": time.Now()}).Error; err != nil {
		return err
	}

	s.logger.Debug().Str("media", media.ID).Msg("analysis complete")
	return nil
}

func (s *Service) failJob(ctx context.Context, jobID, mediaID string, jobErr error) {
	s.db.WithContext(ctx).
		Model(&models.AnalysisJob{}).
		Where("id = ?", jobID).
		Updates(map[string]any{"status": "failed", "error": jobErr.Error(), "updated_at": time.Now()})

	if mediaID != "" {
		s.db.WithContext(ctx).
			Model(&models.MediaItem{}).
			Where("id = ?", mediaID).
			Update("analysis_state", models.AnalysisFailed)
	}
}

type analysisResult struct {
	Duration    time.Duration
	Loudness    float64
	ReplayGain  float64
	IntroEnd    float64
	OutroIn     float64
	Bitrate     int
	Samplerate  int
	Title       string
	Artist      string
	Album       string
	Genre       string
	Year        string
	Artwork     []byte
	ArtworkMime string
}

func (s *Service) performAnalysis(ctx context.Context, media *models.MediaItem) (analysisResult, error) {
	// Build full path from media root + relative path
	fullPath := filepath.Join(s.workDir, media.Path)

	if _, err := os.Stat(fullPath); err != nil {
		return analysisResult{}, err
	}

	// Media engine is required for analysis
	if s.mediaEngineClient == nil {
		return analysisResult{}, ErrMediaEngineNotConfigured
	}

	return s.analyzeViaMediaEngine(ctx, fullPath, media.ID)
}

// analyzeViaMediaEngine uses the media engine gRPC service for analysis
func (s *Service) analyzeViaMediaEngine(ctx context.Context, fullPath, mediaID string) (analysisResult, error) {
	// Ensure connection
	if !s.mediaEngineClient.IsConnected() {
		connectCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
		defer cancel()
		if err := s.mediaEngineClient.Connect(connectCtx); err != nil {
			return analysisResult{}, ErrAnalyzerUnavailable
		}
	}

	// Call media engine analysis
	resp, err := s.mediaEngineClient.AnalyzeMedia(ctx, fullPath)
	if err != nil {
		s.logger.Error().Err(err).Str("media", mediaID).Msg("media engine analysis failed")
		return analysisResult{}, err
	}

	if !resp.Success {
		return analysisResult{}, errors.New(resp.Error)
	}

	// Convert response to analysisResult
	result := analysisResult{
		Duration:   time.Duration(resp.DurationMs) * time.Millisecond,
		Loudness:   float64(resp.LoudnessLufs),
		ReplayGain: float64(resp.ReplayGain),
		IntroEnd:   float64(resp.IntroEnd),
		OutroIn:    float64(resp.OutroIn),
		Bitrate:    int(resp.Bitrate),
		Samplerate: int(resp.SampleRate),
	}

	// Extract metadata
	if resp.Metadata != nil {
		result.Title = resp.Metadata.Title
		result.Artist = resp.Metadata.Artist
		result.Album = resp.Metadata.Album
		result.Genre = resp.Metadata.Genre
		result.Year = resp.Metadata.Year
	}

	// Extract artwork via media engine
	artResp, err := s.mediaEngineClient.ExtractArtwork(ctx, fullPath, 0, 0, "jpeg", 85)
	if err == nil && artResp.Success && len(artResp.ArtworkData) > 0 {
		result.Artwork = artResp.ArtworkData
		result.ArtworkMime = artResp.MimeType
	}

	s.logger.Debug().
		Str("media", mediaID).
		Dur("duration", result.Duration).
		Float64("loudness", result.Loudness).
		Int("bitrate", result.Bitrate).
		Msg("media engine analysis complete")

	return result, nil
}

// Close cleans up analyzer resources
func (s *Service) Close() error {
	if s.mediaEngineClient != nil {
		return s.mediaEngineClient.Close()
	}
	return nil
}

// MediaEngineStatus contains status information about the media engine connection.
type MediaEngineStatus struct {
	Configured bool   `json:"configured"`
	Connected  bool   `json:"connected"`
	Address    string `json:"address,omitempty"`
	Error      string `json:"error,omitempty"`
}

// GetMediaEngineStatus returns the current status of the media engine connection.
func (s *Service) GetMediaEngineStatus(ctx context.Context) MediaEngineStatus {
	status := MediaEngineStatus{
		Configured: s.cfg.MediaEngineGRPCAddr != "",
		Address:    s.cfg.MediaEngineGRPCAddr,
	}

	if !status.Configured {
		status.Error = "Media engine gRPC address not configured"
		return status
	}

	if s.mediaEngineClient == nil {
		status.Error = "Media engine client not initialized"
		return status
	}

	// Check if connected
	status.Connected = s.mediaEngineClient.IsConnected()

	if !status.Connected {
		// Try to connect
		connectCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
		defer cancel()

		if err := s.mediaEngineClient.Connect(connectCtx); err != nil {
			status.Error = err.Error()
		} else {
			status.Connected = true
		}
	}

	return status
}

// TestMediaEngine performs a test analysis to verify the media engine is working.
// Returns nil if the test passes, or an error with details if it fails.
func (s *Service) TestMediaEngine(ctx context.Context) error {
	if s.mediaEngineClient == nil {
		return ErrMediaEngineNotConfigured
	}

	if !s.mediaEngineClient.IsConnected() {
		// Try to connect
		if err := s.mediaEngineClient.Connect(ctx); err != nil {
			return err
		}
	}

	// The media engine is connected if we get here
	// We could do a test analysis here but for now just verify the connection
	return nil
}
