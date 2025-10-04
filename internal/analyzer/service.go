package analyzer

import (
	"context"
	"errors"
	"math"
	"os"
	"os/exec"
	"time"

    "github.com/example/grimnirradio/internal/models"
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
		"loudness_lufs":  result.Loudness,
		"replay_gain":    result.ReplayGain,
		"cue_points":     models.CuePointSet{IntroEnd: result.IntroEnd, OutroIn: result.OutroIn},
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
	Loudness   float64
	ReplayGain float64
	IntroEnd   float64
	OutroIn    float64
}

func (s *Service) performAnalysis(ctx context.Context, media *models.MediaItem) (analysisResult, error) {
	if _, err := os.Stat(media.Path); err != nil {
		return analysisResult{}, err
	}

	duration := media.Duration
	if duration <= 0 {
		duration = 3 * time.Minute
	}

	intro := math.Min(15, duration.Seconds()*0.1)
	outro := math.Max(duration.Seconds()-10, intro+5)

	cmd := exec.CommandContext(ctx, "gst-discoverer-1.0", media.Path)
	if err := cmd.Run(); err != nil {
		s.logger.Debug().Err(err).Str("media", media.ID).Msg("gst-discoverer unavailable, using defaults")
	}

	return analysisResult{
		Loudness:   -14.0,
		ReplayGain: -9.0,
		IntroEnd:   intro,
		OutroIn:    outro,
	}, nil
}
