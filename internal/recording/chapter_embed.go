/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/

package recording

import (
	"fmt"
	"os/exec"
	"time"

	"github.com/rs/zerolog"

	"github.com/friendsincode/grimnir_radio/internal/models"
)

// EmbedChapters writes chapter markers into the recording file after it is
// finalized. Chapters are always stored in the database regardless of
// whether embedding succeeds.
//
// FLAC: uses Vorbis comments (CHAPTER001, CHAPTER001NAME, etc.)
// Opus/Ogg: uses the same Vorbis comment convention.
func EmbedChapters(absPath string, format string, chapters []models.RecordingChapter, logger zerolog.Logger) error {
	if len(chapters) == 0 {
		return nil
	}

	switch format {
	case models.RecordingFormatFLAC:
		return embedFLACChapters(absPath, chapters, logger)
	case models.RecordingFormatOpus:
		return embedOggChapters(absPath, chapters, logger)
	default:
		return fmt.Errorf("unsupported format for chapter embedding: %s", format)
	}
}

// embedFLACChapters uses metaflac to set Vorbis comment chapter tags.
// Format: CHAPTER001=HH:MM:SS.mmm, CHAPTER001NAME=Artist - Title
func embedFLACChapters(absPath string, chapters []models.RecordingChapter, logger zerolog.Logger) error {
	metaflac, err := exec.LookPath("metaflac")
	if err != nil {
		logger.Warn().Msg("metaflac not found, skipping FLAC chapter embedding")
		return nil
	}

	args := []string{}
	for i, ch := range chapters {
		num := fmt.Sprintf("%03d", i+1)
		ts := formatChapterTimestamp(ch.OffsetMs)
		name := formatChapterName(ch.Title, ch.Artist)

		args = append(args,
			fmt.Sprintf("--set-tag=CHAPTER%s=%s", num, ts),
			fmt.Sprintf("--set-tag=CHAPTER%sNAME=%s", num, name),
		)
	}
	args = append(args, absPath)

	cmd := exec.Command(metaflac, args...)
	if output, err := cmd.CombinedOutput(); err != nil {
		logger.Warn().
			Err(err).
			Str("output", string(output)).
			Msg("metaflac chapter embedding failed")
		return fmt.Errorf("metaflac: %w", err)
	}

	logger.Info().
		Int("chapters", len(chapters)).
		Str("path", absPath).
		Msg("FLAC chapters embedded")
	return nil
}

// embedOggChapters uses vorbiscomment to set chapter tags on Ogg files.
func embedOggChapters(absPath string, chapters []models.RecordingChapter, logger zerolog.Logger) error {
	vorbiscomment, err := exec.LookPath("vorbiscomment")
	if err != nil {
		logger.Warn().Msg("vorbiscomment not found, skipping Ogg chapter embedding")
		return nil
	}

	args := []string{"-a"}
	for i, ch := range chapters {
		num := fmt.Sprintf("%03d", i+1)
		ts := formatChapterTimestamp(ch.OffsetMs)
		name := formatChapterName(ch.Title, ch.Artist)

		args = append(args,
			"-t", fmt.Sprintf("CHAPTER%s=%s", num, ts),
			"-t", fmt.Sprintf("CHAPTER%sNAME=%s", num, name),
		)
	}
	args = append(args, absPath)

	cmd := exec.Command(vorbiscomment, args...)
	if output, err := cmd.CombinedOutput(); err != nil {
		logger.Warn().
			Err(err).
			Str("output", string(output)).
			Msg("vorbiscomment chapter embedding failed")
		return fmt.Errorf("vorbiscomment: %w", err)
	}

	logger.Info().
		Int("chapters", len(chapters)).
		Str("path", absPath).
		Msg("Ogg chapters embedded")
	return nil
}

// formatChapterTimestamp converts milliseconds to HH:MM:SS.mmm format.
func formatChapterTimestamp(offsetMs int64) string {
	d := time.Duration(offsetMs) * time.Millisecond
	h := int(d.Hours())
	m := int(d.Minutes()) % 60
	s := int(d.Seconds()) % 60
	ms := int(d.Milliseconds()) % 1000
	return fmt.Sprintf("%02d:%02d:%02d.%03d", h, m, s, ms)
}

// formatChapterName builds "Artist - Title" or just "Title".
func formatChapterName(title, artist string) string {
	if artist != "" && title != "" {
		return artist + " - " + title
	}
	if title != "" {
		return title
	}
	return artist
}
