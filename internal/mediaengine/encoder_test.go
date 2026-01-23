/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/


package mediaengine

import (
	"strings"
	"testing"
)

func TestEncoderBuilder_BuildMP3(t *testing.T) {
	config := EncoderConfig{
		OutputType: OutputTypeTest,
		OutputURL:  "",
		Format:     AudioFormatMP3,
		Bitrate:    192,
		SampleRate: 44100,
		Channels:   2,
	}

	builder := NewEncoderBuilder(config)
	pipeline, err := builder.Build()
	if err != nil {
		t.Fatalf("Build() failed: %v", err)
	}

	// Check pipeline contains expected elements
	if !strings.Contains(pipeline, "audioconvert") {
		t.Error("Pipeline missing audioconvert")
	}
	if !strings.Contains(pipeline, "audioresample") {
		t.Error("Pipeline missing audioresample")
	}
	if !strings.Contains(pipeline, "lamemp3enc") {
		t.Error("Pipeline missing lamemp3enc")
	}
	if !strings.Contains(pipeline, "bitrate=192") {
		t.Error("Pipeline missing bitrate=192")
	}
	if !strings.Contains(pipeline, "fakesink") {
		t.Error("Pipeline missing fakesink (test output)")
	}

	t.Logf("MP3 pipeline: %s", pipeline)
}

func TestEncoderBuilder_BuildAAC(t *testing.T) {
	config := EncoderConfig{
		OutputType: OutputTypeTest,
		Format:     AudioFormatAAC,
		Bitrate:    128,
		SampleRate: 48000,
		Channels:   2,
	}

	builder := NewEncoderBuilder(config)
	pipeline, err := builder.Build()
	if err != nil {
		t.Fatalf("Build() failed: %v", err)
	}

	if !strings.Contains(pipeline, "avenc_aac") {
		t.Error("Pipeline missing avenc_aac")
	}
	if !strings.Contains(pipeline, "rate=48000") {
		t.Error("Pipeline missing rate=48000")
	}

	t.Logf("AAC pipeline: %s", pipeline)
}

func TestEncoderBuilder_BuildOpus(t *testing.T) {
	config := EncoderConfig{
		OutputType: OutputTypeTest,
		Format:     AudioFormatOpus,
		Bitrate:    96,
		SampleRate: 48000,
		Channels:   2,
	}

	builder := NewEncoderBuilder(config)
	pipeline, err := builder.Build()
	if err != nil {
		t.Fatalf("Build() failed: %v", err)
	}

	if !strings.Contains(pipeline, "opusenc") {
		t.Error("Pipeline missing opusenc")
	}
	if !strings.Contains(pipeline, "oggmux") {
		t.Error("Pipeline missing oggmux (Opus requires Ogg container)")
	}

	t.Logf("Opus pipeline: %s", pipeline)
}

func TestEncoderBuilder_BuildVorbis(t *testing.T) {
	config := EncoderConfig{
		OutputType: OutputTypeTest,
		Format:     AudioFormatVorbis,
		Bitrate:    128,
		SampleRate: 44100,
		Channels:   2,
	}

	builder := NewEncoderBuilder(config)
	pipeline, err := builder.Build()
	if err != nil {
		t.Fatalf("Build() failed: %v", err)
	}

	if !strings.Contains(pipeline, "vorbisenc") {
		t.Error("Pipeline missing vorbisenc")
	}
	if !strings.Contains(pipeline, "oggmux") {
		t.Error("Pipeline missing oggmux (Vorbis requires Ogg container)")
	}

	t.Logf("Vorbis pipeline: %s", pipeline)
}

func TestEncoderBuilder_BuildFLAC(t *testing.T) {
	config := EncoderConfig{
		OutputType: OutputTypeTest,
		Format:     AudioFormatFLAC,
		Quality:    0.5,
		SampleRate: 44100,
		Channels:   2,
	}

	builder := NewEncoderBuilder(config)
	pipeline, err := builder.Build()
	if err != nil {
		t.Fatalf("Build() failed: %v", err)
	}

	if !strings.Contains(pipeline, "flacenc") {
		t.Error("Pipeline missing flacenc")
	}

	t.Logf("FLAC pipeline: %s", pipeline)
}

func TestEncoderBuilder_IcecastOutput(t *testing.T) {
	config := EncoderConfig{
		OutputType:  OutputTypeIcecast,
		OutputURL:   "http://icecast.example.com:8000/stream.mp3",
		Username:    "source",
		Password:    "hackme",
		Mount:       "/stream.mp3",
		StreamName:  "Test Station",
		Description: "Test Description",
		Genre:       "Test Genre",
		URL:         "https://test.example.com",
		Format:      AudioFormatMP3,
		Bitrate:     128,
		SampleRate:  44100,
		Channels:    2,
	}

	builder := NewEncoderBuilder(config)
	pipeline, err := builder.Build()
	if err != nil {
		t.Fatalf("Build() failed: %v", err)
	}

	// Check for shout2send with Icecast config
	if !strings.Contains(pipeline, "shout2send") {
		t.Error("Pipeline missing shout2send")
	}
	if !strings.Contains(pipeline, "ip=icecast.example.com") {
		t.Error("Pipeline missing ip=icecast.example.com")
	}
	if !strings.Contains(pipeline, "port=8000") {
		t.Error("Pipeline missing port=8000")
	}
	if !strings.Contains(pipeline, "mount=/stream.mp3") {
		t.Error("Pipeline missing mount=/stream.mp3")
	}
	if !strings.Contains(pipeline, "username=source") {
		t.Error("Pipeline missing username=source")
	}
	if !strings.Contains(pipeline, "password=hackme") {
		t.Error("Pipeline missing password=hackme")
	}
	if !strings.Contains(pipeline, "protocol=http") {
		t.Error("Pipeline missing protocol=http")
	}
	if !strings.Contains(pipeline, `streamname="Test Station"`) {
		t.Error("Pipeline missing streamname")
	}

	t.Logf("Icecast pipeline: %s", pipeline)
}

func TestEncoderBuilder_ShoutcastOutput(t *testing.T) {
	config := EncoderConfig{
		OutputType: OutputTypeShoutcast,
		OutputURL:  "http://shoutcast.example.com:8000",
		Password:   "hackme",
		StreamName: "Test Shoutcast",
		Format:     AudioFormatMP3,
		Bitrate:    128,
		SampleRate: 44100,
		Channels:   2,
	}

	builder := NewEncoderBuilder(config)
	pipeline, err := builder.Build()
	if err != nil {
		t.Fatalf("Build() failed: %v", err)
	}

	// Check for shout2send with Shoutcast config
	if !strings.Contains(pipeline, "shout2send") {
		t.Error("Pipeline missing shout2send")
	}
	if !strings.Contains(pipeline, "protocol=icy") {
		t.Error("Pipeline missing protocol=icy (Shoutcast)")
	}
	if !strings.Contains(pipeline, "password=hackme") {
		t.Error("Pipeline missing password")
	}

	t.Logf("Shoutcast pipeline: %s", pipeline)
}

func TestEncoderBuilder_FileOutput(t *testing.T) {
	config := EncoderConfig{
		OutputType: OutputTypeFile,
		OutputURL:  "/tmp/test-output.mp3",
		Format:     AudioFormatMP3,
		Bitrate:    192,
		SampleRate: 44100,
		Channels:   2,
	}

	builder := NewEncoderBuilder(config)
	pipeline, err := builder.Build()
	if err != nil {
		t.Fatalf("Build() failed: %v", err)
	}

	if !strings.Contains(pipeline, "filesink") {
		t.Error("Pipeline missing filesink")
	}
	if !strings.Contains(pipeline, `location="/tmp/test-output.mp3"`) {
		t.Error("Pipeline missing correct file location")
	}

	t.Logf("File output pipeline: %s", pipeline)
}

func TestEncoderBuilder_ValidateConfig(t *testing.T) {
	tests := []struct {
		name      string
		config    EncoderConfig
		wantError bool
	}{
		{
			name: "valid MP3 config",
			config: EncoderConfig{
				OutputType: OutputTypeTest,
				Format:     AudioFormatMP3,
				Bitrate:    128,
				SampleRate: 44100,
				Channels:   2,
			},
			wantError: false,
		},
		{
			name: "missing format",
			config: EncoderConfig{
				OutputType: OutputTypeTest,
				Bitrate:    128,
				SampleRate: 44100,
				Channels:   2,
			},
			wantError: true,
		},
		{
			name: "missing output type",
			config: EncoderConfig{
				Format:     AudioFormatMP3,
				Bitrate:    128,
				SampleRate: 44100,
				Channels:   2,
			},
			wantError: true,
		},
		{
			name: "bitrate too low",
			config: EncoderConfig{
				OutputType: OutputTypeTest,
				Format:     AudioFormatMP3,
				Bitrate:    4,
				SampleRate: 44100,
				Channels:   2,
			},
			wantError: true,
		},
		{
			name: "bitrate too high",
			config: EncoderConfig{
				OutputType: OutputTypeTest,
				Format:     AudioFormatMP3,
				Bitrate:    500,
				SampleRate: 44100,
				Channels:   2,
			},
			wantError: true,
		},
		{
			name: "invalid sample rate",
			config: EncoderConfig{
				OutputType: OutputTypeTest,
				Format:     AudioFormatMP3,
				Bitrate:    128,
				SampleRate: 12345,
				Channels:   2,
			},
			wantError: true,
		},
		{
			name: "invalid channels",
			config: EncoderConfig{
				OutputType: OutputTypeTest,
				Format:     AudioFormatMP3,
				Bitrate:    128,
				SampleRate: 44100,
				Channels:   3,
			},
			wantError: true,
		},
		{
			name: "Icecast missing password",
			config: EncoderConfig{
				OutputType: OutputTypeIcecast,
				OutputURL:  "http://icecast.example.com:8000/stream.mp3",
				Mount:      "/stream.mp3",
				Format:     AudioFormatMP3,
				Bitrate:    128,
				SampleRate: 44100,
				Channels:   2,
			},
			wantError: true,
		},
		{
			name: "Icecast missing mount",
			config: EncoderConfig{
				OutputType: OutputTypeIcecast,
				OutputURL:  "http://icecast.example.com:8000",
				Password:   "hackme",
				Format:     AudioFormatMP3,
				Bitrate:    128,
				SampleRate: 44100,
				Channels:   2,
			},
			wantError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			builder := NewEncoderBuilder(tt.config)
			err := builder.ValidateConfig()

			if tt.wantError && err == nil {
				t.Error("ValidateConfig() should have returned error")
			}
			if !tt.wantError && err != nil {
				t.Errorf("ValidateConfig() unexpected error: %v", err)
			}
		})
	}
}

func TestEncoderBuilder_GetContentType(t *testing.T) {
	tests := []struct {
		format      AudioFormat
		contentType string
	}{
		{AudioFormatMP3, "audio/mpeg"},
		{AudioFormatAAC, "audio/aac"},
		{AudioFormatOpus, "audio/opus"},
		{AudioFormatVorbis, "audio/ogg"},
		{AudioFormatFLAC, "audio/flac"},
	}

	for _, tt := range tests {
		t.Run(string(tt.format), func(t *testing.T) {
			config := EncoderConfig{
				OutputType: OutputTypeTest,
				Format:     tt.format,
				Bitrate:    128,
				SampleRate: 44100,
				Channels:   2,
			}

			builder := NewEncoderBuilder(config)
			contentType := builder.GetContentType()

			if contentType != tt.contentType {
				t.Errorf("GetContentType() = %q, want %q", contentType, tt.contentType)
			}
		})
	}
}

func TestEncoderBuilder_GetFileExtension(t *testing.T) {
	tests := []struct {
		format    AudioFormat
		extension string
	}{
		{AudioFormatMP3, ".mp3"},
		{AudioFormatAAC, ".aac"},
		{AudioFormatOpus, ".opus"},
		{AudioFormatVorbis, ".ogg"},
		{AudioFormatFLAC, ".flac"},
	}

	for _, tt := range tests {
		t.Run(string(tt.format), func(t *testing.T) {
			config := EncoderConfig{
				OutputType: OutputTypeTest,
				Format:     tt.format,
				Bitrate:    128,
				SampleRate: 44100,
				Channels:   2,
			}

			builder := NewEncoderBuilder(config)
			extension := builder.GetFileExtension()

			if extension != tt.extension {
				t.Errorf("GetFileExtension() = %q, want %q", extension, tt.extension)
			}
		})
	}
}

func TestEncoderBuilder_Defaults(t *testing.T) {
	// Test that defaults are applied correctly
	config := EncoderConfig{
		OutputType: OutputTypeTest,
		Format:     AudioFormatMP3,
		// Omit bitrate, sample rate, channels, username
	}

	builder := NewEncoderBuilder(config)

	// Check defaults were applied
	if builder.config.Bitrate != 128 {
		t.Errorf("Default bitrate = %d, want 128", builder.config.Bitrate)
	}
	if builder.config.SampleRate != 44100 {
		t.Errorf("Default sample rate = %d, want 44100", builder.config.SampleRate)
	}
	if builder.config.Channels != 2 {
		t.Errorf("Default channels = %d, want 2", builder.config.Channels)
	}
	if builder.config.Username != "source" {
		t.Errorf("Default username = %q, want %q", builder.config.Username, "source")
	}
}

func TestEncoderBuilder_MonoEncoding(t *testing.T) {
	config := EncoderConfig{
		OutputType: OutputTypeTest,
		Format:     AudioFormatMP3,
		Bitrate:    64,
		SampleRate: 22050,
		Channels:   1, // Mono
	}

	builder := NewEncoderBuilder(config)
	pipeline, err := builder.Build()
	if err != nil {
		t.Fatalf("Build() failed: %v", err)
	}

	if !strings.Contains(pipeline, "channels=1") {
		t.Error("Pipeline missing channels=1 for mono")
	}
	if !strings.Contains(pipeline, "rate=22050") {
		t.Error("Pipeline missing rate=22050")
	}

	t.Logf("Mono pipeline: %s", pipeline)
}

func TestUpdateMetadata(t *testing.T) {
	metadata := UpdateMetadata("Test Artist", "Test Song")
	expected := "Test Artist - Test Song"

	if metadata != expected {
		t.Errorf("UpdateMetadata() = %q, want %q", metadata, expected)
	}
}

func BenchmarkEncoderBuilder_Build(b *testing.B) {
	config := EncoderConfig{
		OutputType:  OutputTypeIcecast,
		OutputURL:   "http://icecast.example.com:8000/stream.mp3",
		Username:    "source",
		Password:    "hackme",
		Mount:       "/stream.mp3",
		StreamName:  "Benchmark Station",
		Description: "Benchmark Test",
		Format:      AudioFormatMP3,
		Bitrate:     192,
		SampleRate:  44100,
		Channels:    2,
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		builder := NewEncoderBuilder(config)
		_, err := builder.Build()
		if err != nil {
			b.Fatalf("Build() failed: %v", err)
		}
	}
}
