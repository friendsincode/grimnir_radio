/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/

package mediaengine

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"image"
	_ "image/jpeg"
	_ "image/png"
	"io"
	"math"
	"os"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/rs/zerolog"

	pb "github.com/friendsincode/grimnir_radio/proto/mediaengine/v1"
)

// Analyzer performs media file analysis using GStreamer
type Analyzer struct {
	logger zerolog.Logger
}

// NewAnalyzer creates a new media analyzer
func NewAnalyzer(logger zerolog.Logger) *Analyzer {
	return &Analyzer{
		logger: logger.With().Str("component", "analyzer").Logger(),
	}
}

// AnalyzeMedia performs comprehensive media analysis
func (a *Analyzer) AnalyzeMedia(ctx context.Context, filePath string) (*pb.AnalyzeMediaResponse, error) {
	a.logger.Debug().Str("file", filePath).Msg("starting media analysis")

	// Validate file exists
	if _, err := os.Stat(filePath); err != nil {
		return &pb.AnalyzeMediaResponse{
			Success: false,
			Error:   fmt.Sprintf("file not found: %v", err),
		}, nil
	}

	resp := &pb.AnalyzeMediaResponse{
		Success:  true,
		Metadata: &pb.MediaMetadata{},
	}

	// Step 1: Use gst-discoverer for basic metadata
	if err := a.runDiscoverer(ctx, filePath, resp); err != nil {
		a.logger.Warn().Err(err).Msg("discoverer failed, continuing with defaults")
	}

	// Step 2: Run loudness analysis with ebur128
	if err := a.runLoudnessAnalysis(ctx, filePath, resp); err != nil {
		a.logger.Warn().Err(err).Msg("loudness analysis failed, using defaults")
		resp.LoudnessLufs = -14.0
		resp.ReplayGain = -9.0
	}

	// Step 3: Calculate cue points based on duration
	a.calculateCuePoints(resp)

	a.logger.Debug().
		Int64("duration_ms", resp.DurationMs).
		Float32("loudness_lufs", resp.LoudnessLufs).
		Str("codec", resp.Codec).
		Msg("analysis complete")

	return resp, nil
}

// runDiscoverer uses gst-discoverer-1.0 for metadata extraction
func (a *Analyzer) runDiscoverer(ctx context.Context, filePath string, resp *pb.AnalyzeMediaResponse) error {
	cmd := exec.CommandContext(ctx, "gst-discoverer-1.0", "-v", filePath)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("gst-discoverer failed: %w", err)
	}

	a.parseDiscovererOutput(string(output), resp)
	return nil
}

// parseDiscovererOutput parses gst-discoverer-1.0 output
func (a *Analyzer) parseDiscovererOutput(output string, resp *pb.AnalyzeMediaResponse) {
	lines := strings.Split(output, "\n")

	// Regular expressions for parsing
	// gst-discoverer prints fractional seconds with variable precision (often nanoseconds, 9 digits).
	// Example: "Duration: 0:58:12.345000000" (the .345000000 is 345ms, not 345000000ms).
	durationRegex := regexp.MustCompile(`Duration:\s*(\d+):(\d+):(\d+)(?:\.(\d+))?`)
	bitrateRegex := regexp.MustCompile(`bitrate:\s*(\d+)`)
	samplerateRegex := regexp.MustCompile(`sample rate:\s*(\d+)`)
	channelsRegex := regexp.MustCompile(`channels:\s*(\d+)`)
	codecRegex := regexp.MustCompile(`(?i)audio:\s*(\w+)`)

	// Tag patterns
	tagPatterns := map[string]*regexp.Regexp{
		"title":        regexp.MustCompile(`(?i)^\s*title:\s*(.+)$`),
		"artist":       regexp.MustCompile(`(?i)^\s*artist:\s*(.+)$`),
		"album":        regexp.MustCompile(`(?i)^\s*album:\s*(.+)$`),
		"genre":        regexp.MustCompile(`(?i)^\s*genre:\s*(.+)$`),
		"date":         regexp.MustCompile(`(?i)^\s*(?:date|datetime):\s*(.+)$`),
		"track-number": regexp.MustCompile(`(?i)^\s*track-number:\s*(\d+)`),
		"album-artist": regexp.MustCompile(`(?i)^\s*album-artist:\s*(.+)$`),
		"composer":     regexp.MustCompile(`(?i)^\s*composer:\s*(.+)$`),
		"isrc":         regexp.MustCompile(`(?i)^\s*isrc:\s*(.+)$`),
	}

	for _, line := range lines {
		line = strings.TrimSpace(line)

		// Parse duration
		if matches := durationRegex.FindStringSubmatch(line); matches != nil {
			hours, _ := strconv.Atoi(matches[1])
			minutes, _ := strconv.Atoi(matches[2])
			seconds, _ := strconv.Atoi(matches[3])
			frac := ""
			if len(matches) >= 5 {
				frac = matches[4]
			}
			ms := fracToMilliseconds(frac)

			durationMs := int64(hours)*3600000 + int64(minutes)*60000 + int64(seconds)*1000 + ms
			resp.DurationMs = durationMs
		}

		// Parse bitrate (convert from bps to kbps)
		if matches := bitrateRegex.FindStringSubmatch(line); matches != nil {
			bitrate, _ := strconv.Atoi(matches[1])
			resp.Bitrate = int32(bitrate / 1000)
		}

		// Parse sample rate
		if matches := samplerateRegex.FindStringSubmatch(line); matches != nil {
			sampleRate, _ := strconv.Atoi(matches[1])
			resp.SampleRate = int32(sampleRate)
		}

		// Parse channels
		if matches := channelsRegex.FindStringSubmatch(line); matches != nil {
			channels, _ := strconv.Atoi(matches[1])
			resp.Channels = int32(channels)
		}

		// Parse codec
		if matches := codecRegex.FindStringSubmatch(line); matches != nil && resp.Codec == "" {
			resp.Codec = matches[1]
		}

		// Parse tags
		for tag, pattern := range tagPatterns {
			if matches := pattern.FindStringSubmatch(line); matches != nil {
				value := strings.TrimSpace(matches[1])
				switch tag {
				case "title":
					resp.Metadata.Title = value
				case "artist":
					resp.Metadata.Artist = value
				case "album":
					resp.Metadata.Album = value
				case "genre":
					resp.Metadata.Genre = value
				case "date":
					// Extract year from date
					if len(value) >= 4 {
						resp.Metadata.Year = value[:4]
					}
				case "track-number":
					if num, err := strconv.Atoi(value); err == nil {
						resp.Metadata.TrackNumber = int32(num)
					}
				case "album-artist":
					resp.Metadata.AlbumArtist = value
				case "composer":
					resp.Metadata.Composer = value
				case "isrc":
					resp.Metadata.Isrc = value
				}
			}
		}
	}
}

func fracToMilliseconds(frac string) int64 {
	// frac is the digits after the decimal point in seconds, variable precision (e.g. "12", "004", "345000000").
	// Convert to milliseconds via: floor(frac * 1000 / 10^len(frac)).
	if frac == "" {
		return 0
	}
	fracInt, err := strconv.ParseInt(frac, 10, 64)
	if err != nil || fracInt < 0 {
		return 0
	}
	denom := int64(1)
	for i := 0; i < len(frac); i++ {
		denom *= 10
		// guard against pathological precision
		if denom <= 0 {
			return 0
		}
	}
	return (fracInt * 1000) / denom
}

// runLoudnessAnalysis runs EBU R128 loudness measurement
func (a *Analyzer) runLoudnessAnalysis(ctx context.Context, filePath string, resp *pb.AnalyzeMediaResponse) error {
	// Build GStreamer pipeline for loudness analysis
	// Uses ebur128 element for EBU R128 measurement
	pipeline := fmt.Sprintf(
		`filesrc location="%s" ! decodebin ! audioconvert ! audioresample ! ebur128 ! fakesink`,
		filePath,
	)

	cmd := exec.CommandContext(ctx, "gst-launch-1.0", "-v", "-m")
	cmd.Args = append(cmd.Args, strings.Fields(pipeline)...)

	output, err := cmd.CombinedOutput()
	if err != nil {
		// Try alternative approach with ffmpeg as fallback
		return a.runFFmpegLoudness(ctx, filePath, resp)
	}

	a.parseLoudnessOutput(string(output), resp)
	return nil
}

// runFFmpegLoudness is a fallback using ffmpeg for loudness analysis
func (a *Analyzer) runFFmpegLoudness(ctx context.Context, filePath string, resp *pb.AnalyzeMediaResponse) error {
	cmd := exec.CommandContext(ctx, "ffmpeg",
		"-i", filePath,
		"-af", "ebur128=framelog=verbose",
		"-f", "null", "-",
	)

	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("ffmpeg loudness analysis failed: %w", err)
	}

	a.parseFFmpegLoudnessOutput(string(output), resp)
	return nil
}

// parseLoudnessOutput parses ebur128 element output
func (a *Analyzer) parseLoudnessOutput(output string, resp *pb.AnalyzeMediaResponse) {
	// Look for integrated loudness value
	// Format: "integrated loudness: -14.5 LUFS"
	integratedRegex := regexp.MustCompile(`integrated\s*(?:loudness)?:\s*([-0-9.]+)\s*(?:LUFS|LU)`)
	rangeRegex := regexp.MustCompile(`loudness\s*range:\s*([-0-9.]+)\s*(?:LU)?`)
	peakRegex := regexp.MustCompile(`true\s*peak:\s*([-0-9.]+)\s*(?:dBTP)?`)

	if matches := integratedRegex.FindStringSubmatch(output); matches != nil {
		if lufs, err := strconv.ParseFloat(matches[1], 32); err == nil {
			resp.LoudnessLufs = float32(lufs)
			// Calculate replay gain (target -18 LUFS for ReplayGain 2.0)
			resp.ReplayGain = float32(-18.0 - lufs)
		}
	}

	if matches := rangeRegex.FindStringSubmatch(output); matches != nil {
		if lr, err := strconv.ParseFloat(matches[1], 32); err == nil {
			resp.LoudnessRange = float32(lr)
		}
	}

	if matches := peakRegex.FindStringSubmatch(output); matches != nil {
		if peak, err := strconv.ParseFloat(matches[1], 32); err == nil {
			resp.TruePeak = float32(peak)
		}
	}
}

// parseFFmpegLoudnessOutput parses ffmpeg loudness output
func (a *Analyzer) parseFFmpegLoudnessOutput(output string, resp *pb.AnalyzeMediaResponse) {
	// FFmpeg format: "Integrated loudness: I: -14.5 LUFS"
	integratedRegex := regexp.MustCompile(`I:\s*([-0-9.]+)\s*LUFS`)
	rangeRegex := regexp.MustCompile(`LRA:\s*([-0-9.]+)\s*LU`)
	peakRegex := regexp.MustCompile(`Peak:\s*([-0-9.]+)\s*dBFS`)

	if matches := integratedRegex.FindStringSubmatch(output); matches != nil {
		if lufs, err := strconv.ParseFloat(matches[1], 32); err == nil {
			resp.LoudnessLufs = float32(lufs)
			resp.ReplayGain = float32(-18.0 - lufs)
		}
	}

	if matches := rangeRegex.FindStringSubmatch(output); matches != nil {
		if lr, err := strconv.ParseFloat(matches[1], 32); err == nil {
			resp.LoudnessRange = float32(lr)
		}
	}

	if matches := peakRegex.FindStringSubmatch(output); matches != nil {
		if peak, err := strconv.ParseFloat(matches[1], 32); err == nil {
			resp.TruePeak = float32(peak)
		}
	}
}

// calculateCuePoints calculates intro end and outro start based on duration
func (a *Analyzer) calculateCuePoints(resp *pb.AnalyzeMediaResponse) {
	durationSec := float64(resp.DurationMs) / 1000.0

	if durationSec <= 0 {
		durationSec = 180.0 // Default 3 minutes
	}

	// Intro end: min of 15 seconds or 10% of duration
	resp.IntroEnd = float32(math.Min(15, durationSec*0.1))

	// Outro start: max of (duration - 10 seconds) or (intro end + 5 seconds)
	resp.OutroIn = float32(math.Max(durationSec-10, float64(resp.IntroEnd)+5))
}

// ExtractArtwork extracts embedded artwork from a media file
func (a *Analyzer) ExtractArtwork(ctx context.Context, req *pb.ExtractArtworkRequest) (*pb.ExtractArtworkResponse, error) {
	a.logger.Debug().Str("file", req.FilePath).Msg("extracting artwork")

	// Validate file exists
	if _, err := os.Stat(req.FilePath); err != nil {
		return &pb.ExtractArtworkResponse{
			Success: false,
			Error:   fmt.Sprintf("file not found: %v", err),
		}, nil
	}

	// Determine output format
	format := strings.ToLower(req.Format)
	if format == "" {
		format = "jpeg"
	}

	// Create temp file for artwork
	tmpFile, err := os.CreateTemp("", "artwork-*."+format)
	if err != nil {
		return &pb.ExtractArtworkResponse{
			Success: false,
			Error:   fmt.Sprintf("failed to create temp file: %v", err),
		}, nil
	}
	tmpPath := tmpFile.Name()
	tmpFile.Close()
	defer os.Remove(tmpPath)

	// Try GStreamer first for artwork extraction
	if err := a.extractArtworkGStreamer(ctx, req.FilePath, tmpPath, format); err != nil {
		// Fall back to ffmpeg
		if err := a.extractArtworkFFmpeg(ctx, req, tmpPath); err != nil {
			return &pb.ExtractArtworkResponse{
				Success: false,
				Error:   fmt.Sprintf("artwork extraction failed: %v", err),
			}, nil
		}
	}

	// Read extracted artwork
	data, err := os.ReadFile(tmpPath)
	if err != nil || len(data) == 0 {
		return &pb.ExtractArtworkResponse{
			Success: false,
			Error:   "no artwork found in file",
		}, nil
	}

	// Determine MIME type
	mimeType := "image/jpeg"
	switch format {
	case "png":
		mimeType = "image/png"
	case "webp":
		mimeType = "image/webp"
	}

	width := int32(0)
	height := int32(0)
	if cfg, _, err := image.DecodeConfig(bytes.NewReader(data)); err == nil {
		width = int32(cfg.Width)
		height = int32(cfg.Height)
	} else {
		a.logger.Debug().Err(err).Msg("failed to decode extracted artwork dimensions")
	}

	return &pb.ExtractArtworkResponse{
		Success:     true,
		ArtworkData: data,
		MimeType:    mimeType,
		Width:       width,
		Height:      height,
	}, nil
}

// extractArtworkGStreamer extracts artwork using GStreamer
func (a *Analyzer) extractArtworkGStreamer(ctx context.Context, inputPath, outputPath, format string) error {
	// GStreamer pipeline to extract image from media
	// Note: This extracts video frames from the "video" stream which contains album art
	pipeline := fmt.Sprintf(
		`filesrc location="%s" ! decodebin ! videoconvert ! jpegenc ! filesink location="%s"`,
		inputPath, outputPath,
	)

	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, "gst-launch-1.0", "-q")
	cmd.Args = append(cmd.Args, strings.Fields(pipeline)...)

	if err := cmd.Run(); err != nil {
		return err
	}

	// Verify output was created
	if info, err := os.Stat(outputPath); err != nil || info.Size() == 0 {
		return fmt.Errorf("no artwork output created")
	}

	return nil
}

// extractArtworkFFmpeg extracts artwork using FFmpeg
func (a *Analyzer) extractArtworkFFmpeg(ctx context.Context, req *pb.ExtractArtworkRequest, outputPath string) error {
	args := []string{
		"-i", req.FilePath,
		"-an", // No audio
	}

	// Add resize filter if dimensions specified
	if req.MaxWidth > 0 || req.MaxHeight > 0 {
		w := req.MaxWidth
		h := req.MaxHeight
		if w == 0 {
			w = -1
		}
		if h == 0 {
			h = -1
		}
		args = append(args, "-vf", fmt.Sprintf("scale=%d:%d:force_original_aspect_ratio=decrease", w, h))
	}

	// Set quality for JPEG/WebP
	quality := req.Quality
	if quality == 0 {
		quality = 85
	}

	// Output format specific options
	format := strings.ToLower(req.Format)
	switch format {
	case "png":
		args = append(args, "-vcodec", "png")
	case "webp":
		args = append(args, "-vcodec", "libwebp", "-quality", strconv.Itoa(int(quality)))
	default: // jpeg
		args = append(args, "-vcodec", "mjpeg", "-q:v", strconv.Itoa(int((100-quality)/5+2)))
	}

	args = append(args, "-vframes", "1", "-f", "image2", "-y", outputPath)

	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, "ffmpeg", args...)
	return cmd.Run()
}

// GenerateWaveform generates waveform data for visualization
func (a *Analyzer) GenerateWaveform(ctx context.Context, req *pb.GenerateWaveformRequest) (*pb.GenerateWaveformResponse, error) {
	a.logger.Debug().
		Str("file", req.FilePath).
		Int32("samples_per_second", req.SamplesPerSecond).
		Msg("generating waveform")

	// Validate file exists
	if _, err := os.Stat(req.FilePath); err != nil {
		return &pb.GenerateWaveformResponse{
			Success: false,
			Error:   fmt.Sprintf("file not found: %v", err),
		}, nil
	}

	samplesPerSecond := req.SamplesPerSecond
	if samplesPerSecond <= 0 {
		samplesPerSecond = 10 // Default 10 samples per second
	}

	// Calculate interval in nanoseconds for GStreamer level element
	intervalNs := int64(1e9 / float64(samplesPerSecond))

	// Build GStreamer pipeline for level analysis
	pipeline := fmt.Sprintf(
		`filesrc location="%s" ! decodebin ! audioconvert ! level interval=%d post-messages=true ! fakesink sync=false`,
		req.FilePath, intervalNs,
	)

	ctx, cancel := context.WithTimeout(ctx, 5*time.Minute) // Allow up to 5 min for long files
	defer cancel()

	cmd := exec.CommandContext(ctx, "gst-launch-1.0", "-v", "-m")
	cmd.Args = append(cmd.Args, strings.Fields(pipeline)...)

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return &pb.GenerateWaveformResponse{
			Success: false,
			Error:   fmt.Sprintf("failed to create stdout pipe: %v", err),
		}, nil
	}

	stderr, err := cmd.StderrPipe()
	if err != nil {
		return &pb.GenerateWaveformResponse{
			Success: false,
			Error:   fmt.Sprintf("failed to create stderr pipe: %v", err),
		}, nil
	}

	if err := cmd.Start(); err != nil {
		return &pb.GenerateWaveformResponse{
			Success: false,
			Error:   fmt.Sprintf("failed to start gstreamer: %v", err),
		}, nil
	}

	// Parse level output
	resp := &pb.GenerateWaveformResponse{
		Success:    true,
		SampleRate: samplesPerSecond,
		PeakLeft:   make([]float32, 0),
		PeakRight:  make([]float32, 0),
		RmsLeft:    make([]float32, 0),
		RmsRight:   make([]float32, 0),
	}

	// Parse stdout in a goroutine
	go func() {
		a.parseWaveformOutput(stdout, resp, req.Type)
	}()

	// Also check stderr for output
	go func() {
		a.parseWaveformOutput(stderr, resp, req.Type)
	}()

	if err := cmd.Wait(); err != nil {
		// Process may exit with error after producing valid output
		a.logger.Debug().Err(err).Msg("gstreamer process exited")
	}

	// Get duration from sample count
	if len(resp.PeakLeft) > 0 {
		resp.DurationMs = int64(float64(len(resp.PeakLeft)) / float64(samplesPerSecond) * 1000)
	}

	return resp, nil
}

// parseWaveformOutput parses GStreamer level element output
func (a *Analyzer) parseWaveformOutput(output io.Reader, resp *pb.GenerateWaveformResponse, waveformType pb.WaveformType) {
	scanner := bufio.NewScanner(output)

	// GStreamer level output format (from bus messages):
	// level, peak=(double){ -20.5, -21.3 }, rms=(double){ -25.2, -26.1 }
	peakRegex := regexp.MustCompile(`peak=\(double\)\{\s*([-0-9.e+]+),\s*([-0-9.e+]+)\s*\}`)
	rmsRegex := regexp.MustCompile(`rms=\(double\)\{\s*([-0-9.e+]+),\s*([-0-9.e+]+)\s*\}`)

	for scanner.Scan() {
		line := scanner.Text()

		// Parse peak values
		if waveformType == pb.WaveformType_WAVEFORM_TYPE_UNSPECIFIED ||
			waveformType == pb.WaveformType_WAVEFORM_TYPE_PEAK ||
			waveformType == pb.WaveformType_WAVEFORM_TYPE_BOTH {
			if matches := peakRegex.FindStringSubmatch(line); matches != nil {
				left, _ := strconv.ParseFloat(matches[1], 32)
				right, _ := strconv.ParseFloat(matches[2], 32)
				// Convert dB to linear (0-1 range)
				resp.PeakLeft = append(resp.PeakLeft, dbToLinear(float32(left)))
				resp.PeakRight = append(resp.PeakRight, dbToLinear(float32(right)))
			}
		}

		// Parse RMS values
		if waveformType == pb.WaveformType_WAVEFORM_TYPE_UNSPECIFIED ||
			waveformType == pb.WaveformType_WAVEFORM_TYPE_RMS ||
			waveformType == pb.WaveformType_WAVEFORM_TYPE_BOTH {
			if matches := rmsRegex.FindStringSubmatch(line); matches != nil {
				left, _ := strconv.ParseFloat(matches[1], 32)
				right, _ := strconv.ParseFloat(matches[2], 32)
				// Convert dB to linear (0-1 range)
				resp.RmsLeft = append(resp.RmsLeft, dbToLinear(float32(left)))
				resp.RmsRight = append(resp.RmsRight, dbToLinear(float32(right)))
			}
		}
	}
}

// dbToLinear converts decibels to linear amplitude (0-1)
func dbToLinear(db float32) float32 {
	if db <= -60 {
		return 0
	}
	// dB to linear: 10^(dB/20)
	linear := float32(math.Pow(10, float64(db)/20.0))
	if linear > 1 {
		linear = 1
	}
	return linear
}

// AnalyzeMediaSync is a synchronous version that blocks until complete
// This is useful for batch processing
func (a *Analyzer) AnalyzeMediaSync(filePath string, timeout time.Duration) (*pb.AnalyzeMediaResponse, error) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	return a.AnalyzeMedia(ctx, filePath)
}

// BatchAnalyze analyzes multiple files concurrently
func (a *Analyzer) BatchAnalyze(ctx context.Context, filePaths []string, concurrency int) map[string]*pb.AnalyzeMediaResponse {
	results := make(map[string]*pb.AnalyzeMediaResponse)
	resultCh := make(chan struct {
		path string
		resp *pb.AnalyzeMediaResponse
	}, len(filePaths))

	// Semaphore for concurrency control
	sem := make(chan struct{}, concurrency)

	for _, path := range filePaths {
		go func(p string) {
			sem <- struct{}{}
			defer func() { <-sem }()

			resp, err := a.AnalyzeMedia(ctx, p)
			if err != nil {
				resp = &pb.AnalyzeMediaResponse{
					Success: false,
					Error:   err.Error(),
				}
			}
			resultCh <- struct {
				path string
				resp *pb.AnalyzeMediaResponse
			}{p, resp}
		}(path)
	}

	// Collect results
	for range filePaths {
		result := <-resultCh
		results[result.path] = result.resp
	}

	return results
}

// DiscovererResult contains parsed discoverer output (for debugging/comparison)
type DiscovererResult struct {
	Duration   time.Duration
	Bitrate    int
	SampleRate int
	Channels   int
	Codec      string
	Container  string
	Tags       map[string]string
	HasVideo   bool
	HasArtwork bool
	RawOutput  string
}

// RunDiscovererRaw runs gst-discoverer and returns raw parsed results
// Useful for debugging and comparing with other analysis methods
func (a *Analyzer) RunDiscovererRaw(ctx context.Context, filePath string) (*DiscovererResult, error) {
	cmd := exec.CommandContext(ctx, "gst-discoverer-1.0", "-v", filePath)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("gst-discoverer failed: %w", err)
	}

	result := &DiscovererResult{
		Tags:      make(map[string]string),
		RawOutput: string(output),
	}

	// Parse basic info
	resp := &pb.AnalyzeMediaResponse{Metadata: &pb.MediaMetadata{}}
	a.parseDiscovererOutput(string(output), resp)

	result.Duration = time.Duration(resp.DurationMs) * time.Millisecond
	result.Bitrate = int(resp.Bitrate)
	result.SampleRate = int(resp.SampleRate)
	result.Channels = int(resp.Channels)
	result.Codec = resp.Codec

	// Check for video/image streams (album art)
	if strings.Contains(string(output), "video") || strings.Contains(string(output), "image") {
		result.HasVideo = true
		result.HasArtwork = true
	}

	// Extract tags
	if resp.Metadata.Title != "" {
		result.Tags["title"] = resp.Metadata.Title
	}
	if resp.Metadata.Artist != "" {
		result.Tags["artist"] = resp.Metadata.Artist
	}
	if resp.Metadata.Album != "" {
		result.Tags["album"] = resp.Metadata.Album
	}
	if resp.Metadata.Genre != "" {
		result.Tags["genre"] = resp.Metadata.Genre
	}
	if resp.Metadata.Year != "" {
		result.Tags["year"] = resp.Metadata.Year
	}

	return result, nil
}

// CompareWithFFprobe compares GStreamer analysis with FFprobe for validation
func (a *Analyzer) CompareWithFFprobe(ctx context.Context, filePath string) (map[string]interface{}, error) {
	comparison := make(map[string]interface{})

	// Run GStreamer analysis
	gstResp, err := a.AnalyzeMedia(ctx, filePath)
	if err != nil {
		return nil, fmt.Errorf("gstreamer analysis failed: %w", err)
	}
	comparison["gstreamer"] = gstResp

	// Run FFprobe for comparison
	cmd := exec.CommandContext(ctx, "ffprobe",
		"-v", "quiet",
		"-print_format", "json",
		"-show_format",
		"-show_streams",
		filePath,
	)
	output, err := cmd.Output()
	if err != nil {
		comparison["ffprobe_error"] = err.Error()
		return comparison, nil
	}

	var ffprobeResult map[string]interface{}
	if err := json.Unmarshal(output, &ffprobeResult); err != nil {
		comparison["ffprobe_parse_error"] = err.Error()
	} else {
		comparison["ffprobe"] = ffprobeResult
	}

	// Calculate differences
	if format, ok := ffprobeResult["format"].(map[string]interface{}); ok {
		diffs := make(map[string]string)

		// Compare duration
		if durStr, ok := format["duration"].(string); ok {
			ffDur, _ := strconv.ParseFloat(durStr, 64)
			gstDur := float64(gstResp.DurationMs) / 1000.0
			if math.Abs(ffDur-gstDur) > 0.1 {
				diffs["duration"] = fmt.Sprintf("ffprobe=%.3fs, gstreamer=%.3fs", ffDur, gstDur)
			}
		}

		// Compare bitrate
		if brStr, ok := format["bit_rate"].(string); ok {
			ffBr, _ := strconv.Atoi(brStr)
			ffBrKbps := ffBr / 1000
			if math.Abs(float64(ffBrKbps-int(gstResp.Bitrate))) > 10 {
				diffs["bitrate"] = fmt.Sprintf("ffprobe=%d, gstreamer=%d", ffBrKbps, gstResp.Bitrate)
			}
		}

		if len(diffs) > 0 {
			comparison["differences"] = diffs
		}
	}

	return comparison, nil
}

// parseWaveformOutputFromBuffer parses waveform data from a bytes buffer
func (a *Analyzer) parseWaveformOutputFromBuffer(output *bytes.Buffer, resp *pb.GenerateWaveformResponse, waveformType pb.WaveformType) {
	scanner := bufio.NewScanner(output)

	peakRegex := regexp.MustCompile(`peak=\(double\)\{\s*([-0-9.e+]+),\s*([-0-9.e+]+)\s*\}`)
	rmsRegex := regexp.MustCompile(`rms=\(double\)\{\s*([-0-9.e+]+),\s*([-0-9.e+]+)\s*\}`)

	for scanner.Scan() {
		line := scanner.Text()

		if waveformType == pb.WaveformType_WAVEFORM_TYPE_UNSPECIFIED ||
			waveformType == pb.WaveformType_WAVEFORM_TYPE_PEAK ||
			waveformType == pb.WaveformType_WAVEFORM_TYPE_BOTH {
			if matches := peakRegex.FindStringSubmatch(line); matches != nil {
				left, _ := strconv.ParseFloat(matches[1], 32)
				right, _ := strconv.ParseFloat(matches[2], 32)
				resp.PeakLeft = append(resp.PeakLeft, dbToLinear(float32(left)))
				resp.PeakRight = append(resp.PeakRight, dbToLinear(float32(right)))
			}
		}

		if waveformType == pb.WaveformType_WAVEFORM_TYPE_UNSPECIFIED ||
			waveformType == pb.WaveformType_WAVEFORM_TYPE_RMS ||
			waveformType == pb.WaveformType_WAVEFORM_TYPE_BOTH {
			if matches := rmsRegex.FindStringSubmatch(line); matches != nil {
				left, _ := strconv.ParseFloat(matches[1], 32)
				right, _ := strconv.ParseFloat(matches[2], 32)
				resp.RmsLeft = append(resp.RmsLeft, dbToLinear(float32(left)))
				resp.RmsRight = append(resp.RmsRight, dbToLinear(float32(right)))
			}
		}
	}
}
