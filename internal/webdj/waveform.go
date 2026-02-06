/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/

package webdj

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"path/filepath"
	"time"

	"github.com/friendsincode/grimnir_radio/internal/media"
	"github.com/friendsincode/grimnir_radio/internal/mediaengine/client"
	"github.com/friendsincode/grimnir_radio/internal/models"
	pb "github.com/friendsincode/grimnir_radio/proto/mediaengine/v1"
	"github.com/rs/zerolog"
	"gorm.io/gorm"
)

var (
	// ErrWaveformNotFound indicates the waveform data was not found.
	ErrWaveformNotFound = errors.New("waveform not found")

	// ErrWaveformGenerationFailed indicates waveform generation failed.
	ErrWaveformGenerationFailed = errors.New("waveform generation failed")
)

// WaveformData contains waveform visualization data.
type WaveformData struct {
	MediaID       string    `json:"media_id"`
	SamplesPerSec int       `json:"samples_per_sec"`
	DurationMS    int64     `json:"duration_ms"`
	PeakLeft      []float32 `json:"peak_left"`
	PeakRight     []float32 `json:"peak_right"`
	GeneratedAt   time.Time `json:"generated_at"`
}

// WaveformService handles waveform generation and caching.
type WaveformService struct {
	db        *gorm.DB
	mediaSvc  *media.Service
	meClient  *client.Client
	mediaRoot string
	logger    zerolog.Logger
}

// NewWaveformService creates a new waveform service.
func NewWaveformService(db *gorm.DB, mediaSvc *media.Service, meClient *client.Client, mediaRoot string, logger zerolog.Logger) *WaveformService {
	return &WaveformService{
		db:        db,
		mediaSvc:  mediaSvc,
		meClient:  meClient,
		mediaRoot: mediaRoot,
		logger:    logger.With().Str("component", "waveform").Logger(),
	}
}

// GetWaveform retrieves waveform data for a media item, generating if needed.
func (w *WaveformService) GetWaveform(ctx context.Context, mediaID string) (*WaveformData, error) {
	// Check cache first
	var cache models.WaveformCache
	err := w.db.WithContext(ctx).First(&cache, "media_id = ?", mediaID).Error
	if err == nil {
		// Found in cache, decompress and return
		data, err := w.decompressWaveform(&cache)
		if err != nil {
			w.logger.Warn().Err(err).Str("media_id", mediaID).Msg("failed to decompress cached waveform, regenerating")
		} else {
			return data, nil
		}
	} else if !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, fmt.Errorf("query waveform cache: %w", err)
	}

	// Get media item for path
	var mediaItem models.MediaItem
	if err := w.db.WithContext(ctx).First(&mediaItem, "id = ?", mediaID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrMediaNotFound
		}
		return nil, fmt.Errorf("query media: %w", err)
	}

	// Generate waveform
	fullPath := filepath.Join(w.mediaRoot, mediaItem.Path)
	data, err := w.generateWaveform(ctx, mediaID, fullPath, mediaItem.Duration.Milliseconds())
	if err != nil {
		return nil, err
	}

	// Cache it
	if err := w.cacheWaveform(ctx, data); err != nil {
		w.logger.Warn().Err(err).Str("media_id", mediaID).Msg("failed to cache waveform")
	}

	return data, nil
}

// generateWaveform generates waveform data for a media file via the media engine.
func (w *WaveformService) generateWaveform(ctx context.Context, mediaID, path string, durationMS int64) (*WaveformData, error) {
	w.logger.Info().
		Str("media_id", mediaID).
		Str("path", path).
		Int64("duration_ms", durationMS).
		Msg("generating waveform")

	// Default to 10 samples per second for visualization
	const samplesPerSec int32 = 10

	// Check if media engine client is available
	if w.meClient == nil || !w.meClient.IsConnected() {
		w.logger.Warn().Msg("media engine not connected, using placeholder waveform")
		return w.generatePlaceholderWaveform(mediaID, durationMS)
	}

	// Call media engine to generate waveform
	resp, err := w.meClient.GenerateWaveform(ctx, path, samplesPerSec, pb.WaveformType_WAVEFORM_TYPE_PEAK)
	if err != nil {
		w.logger.Warn().Err(err).Str("media_id", mediaID).Msg("media engine waveform generation failed, using placeholder")
		return w.generatePlaceholderWaveform(mediaID, durationMS)
	}

	if !resp.Success {
		w.logger.Warn().Str("error", resp.Error).Str("media_id", mediaID).Msg("waveform generation returned error, using placeholder")
		return w.generatePlaceholderWaveform(mediaID, durationMS)
	}

	// Convert proto float slices to float32 slices
	peakLeft := make([]float32, len(resp.PeakLeft))
	peakRight := make([]float32, len(resp.PeakRight))
	for i, v := range resp.PeakLeft {
		peakLeft[i] = v
	}
	for i, v := range resp.PeakRight {
		peakRight[i] = v
	}

	data := &WaveformData{
		MediaID:       mediaID,
		SamplesPerSec: int(resp.SampleRate),
		DurationMS:    resp.DurationMs,
		PeakLeft:      peakLeft,
		PeakRight:     peakRight,
		GeneratedAt:   time.Now(),
	}

	w.logger.Info().
		Str("media_id", mediaID).
		Int("num_samples", len(peakLeft)).
		Msg("waveform generated via media engine")

	return data, nil
}

// generatePlaceholderWaveform creates synthetic waveform data when media engine is unavailable.
func (w *WaveformService) generatePlaceholderWaveform(mediaID string, durationMS int64) (*WaveformData, error) {
	const samplesPerSec = 10
	numSamples := int((durationMS * int64(samplesPerSec)) / 1000)
	if numSamples < 10 {
		numSamples = 10
	}
	if numSamples > 10000 {
		numSamples = 10000 // Cap at 10000 samples
	}

	peakLeft := make([]float32, numSamples)
	peakRight := make([]float32, numSamples)

	// Generate placeholder waveform (simulated audio envelope)
	for i := 0; i < numSamples; i++ {
		t := float64(i) / float64(numSamples)
		base := float32(0.3 + 0.5*t)
		if t > 0.8 {
			base = float32(0.3 + 0.5*(1-t)*5)
		}
		variation := float32(0.1 * (float64(i%20) / 20.0))
		peakLeft[i] = base + variation
		peakRight[i] = base + variation*0.9
	}

	return &WaveformData{
		MediaID:       mediaID,
		SamplesPerSec: samplesPerSec,
		DurationMS:    durationMS,
		PeakLeft:      peakLeft,
		PeakRight:     peakRight,
		GeneratedAt:   time.Now(),
	}, nil
}

// cacheWaveform stores waveform data in the database.
func (w *WaveformService) cacheWaveform(ctx context.Context, data *WaveformData) error {
	// Compress waveform data
	compressed, err := w.compressWaveform(data)
	if err != nil {
		return fmt.Errorf("compress waveform: %w", err)
	}

	cache := models.WaveformCache{
		MediaID:       data.MediaID,
		SamplesPerSec: data.SamplesPerSec,
		DurationMS:    data.DurationMS,
		PeakData:      compressed,
		GeneratedAt:   data.GeneratedAt,
	}

	// Upsert cache entry
	result := w.db.WithContext(ctx).
		Where(models.WaveformCache{MediaID: data.MediaID}).
		Assign(cache).
		FirstOrCreate(&cache)

	if result.Error != nil {
		return fmt.Errorf("save waveform cache: %w", result.Error)
	}

	return nil
}

// compressWaveform compresses waveform data for storage.
func (w *WaveformService) compressWaveform(data *WaveformData) ([]byte, error) {
	var buf bytes.Buffer

	// Write header
	binary.Write(&buf, binary.LittleEndian, int32(len(data.PeakLeft)))

	// Write peak data as interleaved left/right
	for i := 0; i < len(data.PeakLeft); i++ {
		binary.Write(&buf, binary.LittleEndian, data.PeakLeft[i])
		if i < len(data.PeakRight) {
			binary.Write(&buf, binary.LittleEndian, data.PeakRight[i])
		} else {
			binary.Write(&buf, binary.LittleEndian, data.PeakLeft[i])
		}
	}

	// Gzip compress
	var compressed bytes.Buffer
	gz := gzip.NewWriter(&compressed)
	if _, err := gz.Write(buf.Bytes()); err != nil {
		return nil, err
	}
	if err := gz.Close(); err != nil {
		return nil, err
	}

	return compressed.Bytes(), nil
}

// decompressWaveform decompresses cached waveform data.
func (w *WaveformService) decompressWaveform(cache *models.WaveformCache) (*WaveformData, error) {
	// Gzip decompress
	gz, err := gzip.NewReader(bytes.NewReader(cache.PeakData))
	if err != nil {
		return nil, fmt.Errorf("create gzip reader: %w", err)
	}
	defer gz.Close()

	decompressed, err := io.ReadAll(gz)
	if err != nil {
		return nil, fmt.Errorf("decompress: %w", err)
	}

	buf := bytes.NewReader(decompressed)

	// Read header
	var numSamples int32
	if err := binary.Read(buf, binary.LittleEndian, &numSamples); err != nil {
		return nil, fmt.Errorf("read header: %w", err)
	}

	// Read peak data
	peakLeft := make([]float32, numSamples)
	peakRight := make([]float32, numSamples)

	for i := int32(0); i < numSamples; i++ {
		if err := binary.Read(buf, binary.LittleEndian, &peakLeft[i]); err != nil {
			return nil, fmt.Errorf("read peak left: %w", err)
		}
		if err := binary.Read(buf, binary.LittleEndian, &peakRight[i]); err != nil {
			return nil, fmt.Errorf("read peak right: %w", err)
		}
	}

	return &WaveformData{
		MediaID:       cache.MediaID,
		SamplesPerSec: cache.SamplesPerSec,
		DurationMS:    cache.DurationMS,
		PeakLeft:      peakLeft,
		PeakRight:     peakRight,
		GeneratedAt:   cache.GeneratedAt,
	}, nil
}

// DeleteWaveform removes cached waveform data.
func (w *WaveformService) DeleteWaveform(ctx context.Context, mediaID string) error {
	result := w.db.WithContext(ctx).
		Where("media_id = ?", mediaID).
		Delete(&models.WaveformCache{})

	if result.Error != nil {
		return fmt.Errorf("delete waveform: %w", result.Error)
	}

	return nil
}

// InvalidateWaveform marks a waveform as needing regeneration.
func (w *WaveformService) InvalidateWaveform(ctx context.Context, mediaID string) error {
	return w.DeleteWaveform(ctx, mediaID)
}
