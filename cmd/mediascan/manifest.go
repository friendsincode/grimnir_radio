/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/

package main

import "time"

// Manifest is the top-level JSON structure produced by mediascan.
type Manifest struct {
	Version    int           `json:"version"`
	SourceType string        `json:"source_type"`
	ScannedAt  time.Time     `json:"scanned_at"`
	RootDirs   []string      `json:"root_dirs"`
	Files      []FileEntry   `json:"files"`
	Stats      ManifestStats `json:"stats"`
}

// FileEntry describes a single scanned media file.
type FileEntry struct {
	Path         string        `json:"path"`
	RelativePath string        `json:"relative_path"`
	Filename     string        `json:"filename"`
	Size         int64         `json:"size"`
	ModifiedAt   time.Time     `json:"modified_at"`
	ContentHash  string        `json:"content_hash"`
	Metadata     *FileMetadata `json:"metadata,omitempty"`
}

// FileMetadata holds ID3/ffprobe-extracted tags.
type FileMetadata struct {
	Title           string  `json:"title,omitempty"`
	Artist          string  `json:"artist,omitempty"`
	Album           string  `json:"album,omitempty"`
	Genre           string  `json:"genre,omitempty"`
	Year            string  `json:"year,omitempty"`
	DurationSeconds float64 `json:"duration_seconds,omitempty"`
}

// ManifestStats holds aggregate scan statistics.
type ManifestStats struct {
	TotalFiles      int     `json:"total_files"`
	TotalSize       int64   `json:"total_size"`
	Errors          int     `json:"errors"`
	DurationSeconds float64 `json:"duration_seconds"`
}
