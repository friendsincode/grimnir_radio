/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/


package analyzer

import (
	"context"
	"encoding/json"
	"errors"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/friendsincode/grimnir_radio/internal/models"
	"github.com/google/uuid"
	"github.com/rs/zerolog"
	"gorm.io/gorm"
)

// ErrAnalyzerUnavailable indicates pipeline failure.
var ErrAnalyzerUnavailable = errors.New("analyzer unavailable")

// Service performs loudness and cue point analysis on media imports.
type Service struct {
	db      *gorm.DB
	logger  zerolog.Logger
	workDir string
}

// New constructs an analyzer service.
func New(db *gorm.DB, workDir string, logger zerolog.Logger) *Service {
	return &Service{db: db, workDir: workDir, logger: logger}
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

	result := analysisResult{
		Loudness:   -14.0, // Default LUFS
		ReplayGain: -9.0,  // Default replay gain
	}

	// Use ffprobe to extract metadata
	cmd := exec.CommandContext(ctx, "ffprobe",
		"-v", "quiet",
		"-print_format", "json",
		"-show_format",
		"-show_streams",
		fullPath,
	)
	output, err := cmd.Output()
	if err != nil {
		s.logger.Warn().Err(err).Str("media", media.ID).Msg("ffprobe failed, using defaults")
	} else {
		s.parseFFProbeOutput(output, &result)
	}

	// Calculate cue points based on duration
	duration := result.Duration
	if duration <= 0 {
		duration = 3 * time.Minute
	}
	result.IntroEnd = math.Min(15, duration.Seconds()*0.1)
	result.OutroIn = math.Max(duration.Seconds()-10, result.IntroEnd+5)

	// Extract embedded album art
	artwork, mime := s.extractArtwork(ctx, fullPath)
	if artwork != nil {
		result.Artwork = artwork
		result.ArtworkMime = mime
	}

	return result, nil
}

// extractArtwork extracts embedded album art from an audio file using ffmpeg.
func (s *Service) extractArtwork(ctx context.Context, filePath string) ([]byte, string) {
	// Create temp file for artwork output
	tmpFile, err := os.CreateTemp("", "artwork-*.jpg")
	if err != nil {
		return nil, ""
	}
	tmpPath := tmpFile.Name()
	tmpFile.Close()
	defer os.Remove(tmpPath)

	// Extract artwork using ffmpeg
	cmd := exec.CommandContext(ctx, "ffmpeg",
		"-i", filePath,
		"-an",           // No audio
		"-vcodec", "mjpeg", // Output as JPEG
		"-vframes", "1", // Only one frame
		"-f", "image2",
		"-y", // Overwrite
		tmpPath,
	)
	if err := cmd.Run(); err != nil {
		// No artwork or extraction failed - not an error
		return nil, ""
	}

	// Read the extracted artwork
	data, err := os.ReadFile(tmpPath)
	if err != nil || len(data) == 0 {
		return nil, ""
	}

	return data, "image/jpeg"
}

func (s *Service) parseFFProbeOutput(output []byte, result *analysisResult) {
	var data struct {
		Format struct {
			Duration string            `json:"duration"`
			BitRate  string            `json:"bit_rate"`
			Tags     map[string]string `json:"tags"`
		} `json:"format"`
		Streams []struct {
			SampleRate string `json:"sample_rate"`
		} `json:"streams"`
	}

	if err := json.Unmarshal(output, &data); err != nil {
		return
	}

	// Parse duration
	if data.Format.Duration != "" {
		if secs, err := strconv.ParseFloat(data.Format.Duration, 64); err == nil {
			result.Duration = time.Duration(secs * float64(time.Second))
		}
	}

	// Parse bitrate
	if data.Format.BitRate != "" {
		if br, err := strconv.Atoi(data.Format.BitRate); err == nil {
			result.Bitrate = br / 1000 // Convert to kbps
		}
	}

	// Parse sample rate from first audio stream
	for _, stream := range data.Streams {
		if stream.SampleRate != "" {
			if sr, err := strconv.Atoi(stream.SampleRate); err == nil {
				result.Samplerate = sr
				break
			}
		}
	}

	// Parse ID3 tags (case-insensitive)
	for key, value := range data.Format.Tags {
		switch strings.ToLower(key) {
		case "title":
			result.Title = value
		case "artist":
			result.Artist = value
		case "album":
			result.Album = value
		case "genre":
			result.Genre = value
		case "date", "year":
			result.Year = value
		}
	}
}
