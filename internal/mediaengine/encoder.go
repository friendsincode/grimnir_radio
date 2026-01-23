/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/


package mediaengine

import (
	"fmt"
	"net/url"
	"strings"
)

// EncoderConfig contains configuration for audio encoding and output
type EncoderConfig struct {
	// Output settings
	OutputType OutputType // icecast, shoutcast, http, file, test
	OutputURL  string     // Full URL for streaming (e.g., http://icecast:8000/stream.mp3)

	// Icecast/Shoutcast settings
	Username    string // Usually "source" for Icecast
	Password    string
	Mount       string // Mount point (e.g., "/stream.mp3")
	StreamName  string // Station name
	Description string
	Genre       string
	URL         string // Station URL

	// Encoder settings
	Format     AudioFormat // mp3, aac, opus, vorbis, flac
	Bitrate    int         // Bitrate in kbps (e.g., 128, 192, 256)
	SampleRate int         // Sample rate in Hz (e.g., 44100, 48000)
	Channels   int         // Number of channels (1 = mono, 2 = stereo)
	Quality    float32     // Quality setting for VBR codecs (0.0-1.0)

	// Advanced encoder options
	EncoderOptions map[string]string
}

// OutputType represents the type of output destination
type OutputType string

const (
	OutputTypeIcecast   OutputType = "icecast"   // Icecast2 server
	OutputTypeShoutcast OutputType = "shoutcast" // Shoutcast server
	OutputTypeHTTP      OutputType = "http"      // Generic HTTP PUT/POST
	OutputTypeFile      OutputType = "file"      // File output
	OutputTypeTest      OutputType = "test"      // Test sink (no actual output)
)

// AudioFormat represents supported audio encoding formats
type AudioFormat string

const (
	AudioFormatMP3    AudioFormat = "mp3"
	AudioFormatAAC    AudioFormat = "aac"
	AudioFormatOpus   AudioFormat = "opus"
	AudioFormatVorbis AudioFormat = "vorbis"
	AudioFormatFLAC   AudioFormat = "flac"
)

// EncoderBuilder builds GStreamer encoder and output elements
type EncoderBuilder struct {
	config EncoderConfig
}

// NewEncoderBuilder creates a new encoder builder
func NewEncoderBuilder(config EncoderConfig) *EncoderBuilder {
	// Set defaults
	if config.Bitrate == 0 {
		config.Bitrate = 128 // Default to 128kbps
	}
	if config.SampleRate == 0 {
		config.SampleRate = 44100 // Default to 44.1kHz
	}
	if config.Channels == 0 {
		config.Channels = 2 // Default to stereo
	}
	if config.Username == "" {
		config.Username = "source" // Icecast default
	}
	if config.Mount == "" && config.OutputURL != "" {
		// Extract mount from URL
		if parsedURL, err := url.Parse(config.OutputURL); err == nil {
			config.Mount = parsedURL.Path
		}
	}

	return &EncoderBuilder{
		config: config,
	}
}

// Build generates the GStreamer pipeline string for encoder and output
func (eb *EncoderBuilder) Build() (string, error) {
	var pipeline strings.Builder

	// Add audio conversion and resampling
	pipeline.WriteString("audioconvert ! ")
	pipeline.WriteString(fmt.Sprintf("audioresample ! audio/x-raw,rate=%d,channels=%d ! ",
		eb.config.SampleRate, eb.config.Channels))

	// Add encoder
	encoderElement, err := eb.buildEncoder()
	if err != nil {
		return "", fmt.Errorf("build encoder: %w", err)
	}
	pipeline.WriteString(encoderElement)
	pipeline.WriteString(" ! ")

	// Add muxer if needed
	muxerElement := eb.buildMuxer()
	if muxerElement != "" {
		pipeline.WriteString(muxerElement)
		pipeline.WriteString(" ! ")
	}

	// Add output
	outputElement, err := eb.buildOutput()
	if err != nil {
		return "", fmt.Errorf("build output: %w", err)
	}
	pipeline.WriteString(outputElement)

	return pipeline.String(), nil
}

// buildEncoder generates the encoder element based on format
func (eb *EncoderBuilder) buildEncoder() (string, error) {
	switch eb.config.Format {
	case AudioFormatMP3:
		return eb.buildMP3Encoder(), nil

	case AudioFormatAAC:
		return eb.buildAACEncoder(), nil

	case AudioFormatOpus:
		return eb.buildOpusEncoder(), nil

	case AudioFormatVorbis:
		return eb.buildVorbisEncoder(), nil

	case AudioFormatFLAC:
		return eb.buildFLACEncoder(), nil

	default:
		return "", fmt.Errorf("unsupported audio format: %s", eb.config.Format)
	}
}

// buildMP3Encoder builds an MP3 encoder (using lamemp3enc)
func (eb *EncoderBuilder) buildMP3Encoder() string {
	// Use lamemp3enc for MP3 encoding
	// target=1 means CBR (constant bitrate)
	// cbr=true ensures constant bitrate
	return fmt.Sprintf("lamemp3enc target=1 bitrate=%d cbr=true",
		eb.config.Bitrate)
}

// buildAACEncoder builds an AAC encoder (using avenc_aac or fdkaacenc)
func (eb *EncoderBuilder) buildAACEncoder() string {
	// Try fdkaacenc first (better quality), fall back to avenc_aac
	// Note: In production, you'd detect which is available
	// For now, use avenc_aac which is more widely available
	return fmt.Sprintf("avenc_aac bitrate=%d",
		eb.config.Bitrate*1000) // avenc_aac takes bitrate in bps
}

// buildOpusEncoder builds an Opus encoder
func (eb *EncoderBuilder) buildOpusEncoder() string {
	// Opus encoder with bitrate
	return fmt.Sprintf("opusenc bitrate=%d",
		eb.config.Bitrate*1000) // opusenc takes bitrate in bps
}

// buildVorbisEncoder builds a Vorbis encoder
func (eb *EncoderBuilder) buildVorbisEncoder() string {
	// Vorbis encoder with quality setting
	// Quality ranges from -0.1 to 1.0 (higher is better)
	// If quality not specified, use bitrate-based encoding
	if eb.config.Quality > 0 {
		return fmt.Sprintf("vorbisenc quality=%.2f", eb.config.Quality)
	}
	return fmt.Sprintf("vorbisenc bitrate=%d", eb.config.Bitrate*1000)
}

// buildFLACEncoder builds a FLAC encoder (lossless)
func (eb *EncoderBuilder) buildFLACEncoder() string {
	// FLAC encoder (lossless, no bitrate setting)
	// quality: 0-8 (0=fast/larger, 8=slow/smaller)
	quality := 5 // Default medium quality
	if eb.config.Quality > 0 {
		quality = int(eb.config.Quality * 8)
	}
	return fmt.Sprintf("flacenc quality=%d", quality)
}

// buildMuxer generates the muxer element if needed
func (eb *EncoderBuilder) buildMuxer() string {
	switch eb.config.Format {
	case AudioFormatMP3:
		// MP3 doesn't need a muxer for streaming
		return ""

	case AudioFormatAAC:
		// AAC in ADTS container (no muxer needed for streaming)
		return ""

	case AudioFormatOpus:
		// Opus in Ogg container
		return "oggmux"

	case AudioFormatVorbis:
		// Vorbis in Ogg container
		return "oggmux"

	case AudioFormatFLAC:
		// FLAC can be raw or in Ogg container
		// For streaming, use raw
		return ""

	default:
		return ""
	}
}

// buildOutput generates the output element based on output type
func (eb *EncoderBuilder) buildOutput() (string, error) {
	switch eb.config.OutputType {
	case OutputTypeIcecast:
		return eb.buildIcecastOutput(), nil

	case OutputTypeShoutcast:
		return eb.buildShoutcastOutput(), nil

	case OutputTypeHTTP:
		return eb.buildHTTPOutput(), nil

	case OutputTypeFile:
		return eb.buildFileOutput(), nil

	case OutputTypeTest:
		return "fakesink", nil

	default:
		return "", fmt.Errorf("unsupported output type: %s", eb.config.OutputType)
	}
}

// buildIcecastOutput builds output for Icecast2 server
func (eb *EncoderBuilder) buildIcecastOutput() string {
	// Use shout2send element for Icecast
	// Parse URL to extract host and port
	parsedURL, err := url.Parse(eb.config.OutputURL)
	if err != nil {
		// Fallback to basic config
		return fmt.Sprintf("shout2send ip=localhost port=8000 mount=%s password=%s",
			eb.config.Mount, eb.config.Password)
	}

	host := parsedURL.Hostname()
	port := parsedURL.Port()
	if port == "" {
		port = "8000" // Default Icecast port
	}

	// Build shout2send element
	var output strings.Builder
	output.WriteString("shout2send ")
	output.WriteString(fmt.Sprintf("ip=%s ", host))
	output.WriteString(fmt.Sprintf("port=%s ", port))
	output.WriteString(fmt.Sprintf("mount=%s ", eb.config.Mount))
	output.WriteString(fmt.Sprintf("username=%s ", eb.config.Username))
	output.WriteString(fmt.Sprintf("password=%s ", eb.config.Password))
	output.WriteString("protocol=http ") // Icecast uses HTTP protocol

	// Add metadata
	if eb.config.StreamName != "" {
		output.WriteString(fmt.Sprintf("streamname=\"%s\" ", escapeQuotes(eb.config.StreamName)))
	}
	if eb.config.Description != "" {
		output.WriteString(fmt.Sprintf("description=\"%s\" ", escapeQuotes(eb.config.Description)))
	}
	if eb.config.Genre != "" {
		output.WriteString(fmt.Sprintf("genre=\"%s\" ", escapeQuotes(eb.config.Genre)))
	}
	if eb.config.URL != "" {
		output.WriteString(fmt.Sprintf("url=\"%s\" ", eb.config.URL))
	}

	return output.String()
}

// buildShoutcastOutput builds output for Shoutcast server
func (eb *EncoderBuilder) buildShoutcastOutput() string {
	// Shoutcast uses similar config but different protocol
	parsedURL, err := url.Parse(eb.config.OutputURL)
	if err != nil {
		return fmt.Sprintf("shout2send ip=localhost port=8000 password=%s protocol=icy",
			eb.config.Password)
	}

	host := parsedURL.Hostname()
	port := parsedURL.Port()
	if port == "" {
		port = "8000" // Default Shoutcast port
	}

	var output strings.Builder
	output.WriteString("shout2send ")
	output.WriteString(fmt.Sprintf("ip=%s ", host))
	output.WriteString(fmt.Sprintf("port=%s ", port))
	output.WriteString(fmt.Sprintf("password=%s ", eb.config.Password))
	output.WriteString("protocol=icy ") // Shoutcast uses ICY protocol

	// Shoutcast metadata
	if eb.config.StreamName != "" {
		output.WriteString(fmt.Sprintf("streamname=\"%s\" ", escapeQuotes(eb.config.StreamName)))
	}

	return output.String()
}

// buildHTTPOutput builds generic HTTP output
func (eb *EncoderBuilder) buildHTTPOutput() string {
	// Use souphttpclientsink for generic HTTP streaming
	return fmt.Sprintf("souphttpclientsink location=\"%s\"", eb.config.OutputURL)
}

// buildFileOutput builds file output
func (eb *EncoderBuilder) buildFileOutput() string {
	// Use filesink for file output
	return fmt.Sprintf("filesink location=\"%s\"", eb.config.OutputURL)
}

// GetContentType returns the MIME content type for the configured format
func (eb *EncoderBuilder) GetContentType() string {
	switch eb.config.Format {
	case AudioFormatMP3:
		return "audio/mpeg"
	case AudioFormatAAC:
		return "audio/aac"
	case AudioFormatOpus:
		return "audio/opus"
	case AudioFormatVorbis:
		return "audio/ogg"
	case AudioFormatFLAC:
		return "audio/flac"
	default:
		return "application/octet-stream"
	}
}

// GetFileExtension returns the file extension for the configured format
func (eb *EncoderBuilder) GetFileExtension() string {
	switch eb.config.Format {
	case AudioFormatMP3:
		return ".mp3"
	case AudioFormatAAC:
		return ".aac"
	case AudioFormatOpus:
		return ".opus"
	case AudioFormatVorbis:
		return ".ogg"
	case AudioFormatFLAC:
		return ".flac"
	default:
		return ".bin"
	}
}

// ValidateConfig validates the encoder configuration
func (eb *EncoderBuilder) ValidateConfig() error {
	// Check format
	if eb.config.Format == "" {
		return fmt.Errorf("audio format is required")
	}

	// Check output type and URL
	if eb.config.OutputType == "" {
		return fmt.Errorf("output type is required")
	}

	if eb.config.OutputType != OutputTypeTest && eb.config.OutputURL == "" {
		return fmt.Errorf("output URL is required for output type: %s", eb.config.OutputType)
	}

	// Check Icecast/Shoutcast specific settings
	if eb.config.OutputType == OutputTypeIcecast || eb.config.OutputType == OutputTypeShoutcast {
		if eb.config.Password == "" {
			return fmt.Errorf("password is required for %s output", eb.config.OutputType)
		}
		if eb.config.Mount == "" && eb.config.OutputType == OutputTypeIcecast {
			return fmt.Errorf("mount point is required for Icecast output")
		}
	}

	// Check bitrate is reasonable
	if eb.config.Bitrate < 8 || eb.config.Bitrate > 320 {
		return fmt.Errorf("bitrate must be between 8 and 320 kbps, got: %d", eb.config.Bitrate)
	}

	// Check sample rate
	validSampleRates := []int{8000, 11025, 16000, 22050, 32000, 44100, 48000, 96000}
	validRate := false
	for _, rate := range validSampleRates {
		if eb.config.SampleRate == rate {
			validRate = true
			break
		}
	}
	if !validRate {
		return fmt.Errorf("invalid sample rate: %d (must be one of: 8000, 11025, 16000, 22050, 32000, 44100, 48000, 96000)", eb.config.SampleRate)
	}

	// Check channels
	if eb.config.Channels != 1 && eb.config.Channels != 2 {
		return fmt.Errorf("channels must be 1 (mono) or 2 (stereo), got: %d", eb.config.Channels)
	}

	return nil
}

// Helper function to escape quotes in string values
func escapeQuotes(s string) string {
	return strings.ReplaceAll(s, "\"", "\\\"")
}

// UpdateMetadata generates a shout2send metadata update command
// This can be used to update ICY metadata (song title, artist) without restarting the stream
func UpdateMetadata(artist, title string) string {
	// Format: artist - title
	metadata := fmt.Sprintf("%s - %s", artist, title)
	// Note: Actual metadata updates require GObject property setting
	// This would be: g_object_set(shout2send_element, "meta", metadata, NULL)
	// For now, return the formatted metadata string
	return metadata
}
