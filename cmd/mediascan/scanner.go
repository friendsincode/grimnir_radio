/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/

package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
)

// scanJob is a unit of work sent to hash workers.
type scanJob struct {
	fullPath string
	relPath  string
	info     os.FileInfo
	rootDir  string
}

// scanResult is the result of processing a single file.
type scanResult struct {
	entry FileEntry
	err   error
}

// scanner walks directories and produces a manifest.
type scanner struct {
	dirs       []string
	workers    int
	noMetadata bool
	sourceType string
}

func (s *scanner) scan(ctx context.Context) (*Manifest, error) {
	startTime := time.Now()

	manifest := &Manifest{
		Version:    1,
		SourceType: s.sourceType,
		ScannedAt:  startTime.UTC(),
		RootDirs:   s.dirs,
	}

	jobs := make(chan scanJob, s.workers*2)
	results := make(chan scanResult, s.workers*2)

	// Start workers
	var wg sync.WaitGroup
	for i := 0; i < s.workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for job := range jobs {
				entry, err := s.processFile(ctx, job)
				results <- scanResult{entry: entry, err: err}
			}
		}()
	}

	// Collect results in a separate goroutine
	var entries []FileEntry
	var totalSize int64
	var errCount int
	var collectDone sync.WaitGroup
	collectDone.Add(1)
	go func() {
		defer collectDone.Done()
		for r := range results {
			if r.err != nil {
				fmt.Fprintf(os.Stderr, "warning: %v\n", r.err)
				errCount++
				continue
			}
			entries = append(entries, r.entry)
			totalSize += r.entry.Size
		}
	}()

	// Walk directories and enqueue jobs
	var fileCount int
	for _, dir := range s.dirs {
		// Expand globs
		matches, err := filepath.Glob(dir)
		if err != nil {
			fmt.Fprintf(os.Stderr, "warning: invalid glob %q: %v\n", dir, err)
			errCount++
			continue
		}
		if len(matches) == 0 {
			matches = []string{dir}
		}

		for _, matchDir := range matches {
			err := filepath.Walk(matchDir, func(path string, info os.FileInfo, err error) error {
				if err != nil {
					fmt.Fprintf(os.Stderr, "warning: %s: %v\n", path, err)
					errCount++
					return nil
				}
				select {
				case <-ctx.Done():
					return ctx.Err()
				default:
				}
				if info.IsDir() {
					return nil
				}
				if !isMediaFile(info.Name()) {
					return nil
				}
				fileCount++
				jobs <- scanJob{
					fullPath: path,
					info:     info,
					rootDir:  matchDir,
				}
				return nil
			})
			if err != nil && err != context.Canceled {
				fmt.Fprintf(os.Stderr, "warning: walk %s: %v\n", matchDir, err)
			}
		}
	}

	close(jobs)
	wg.Wait()
	close(results)
	collectDone.Wait()

	manifest.Files = entries
	manifest.Stats = ManifestStats{
		TotalFiles:      len(entries),
		TotalSize:       totalSize,
		Errors:          errCount,
		DurationSeconds: time.Since(startTime).Seconds(),
	}

	return manifest, nil
}

func (s *scanner) processFile(ctx context.Context, job scanJob) (FileEntry, error) {
	// Compute relative path from root dir
	relPath, err := filepath.Rel(job.rootDir, job.fullPath)
	if err != nil {
		relPath = filepath.Base(job.fullPath)
	}

	// For AzuraCast, strip the leading station number directory if present
	// For LibreTime, the path is already relative to the stor directory

	hash, err := computeFileHash(job.fullPath)
	if err != nil {
		return FileEntry{}, fmt.Errorf("%s: hash: %w", job.fullPath, err)
	}

	entry := FileEntry{
		Path:         job.fullPath,
		RelativePath: relPath,
		Filename:     filepath.Base(job.fullPath),
		Size:         job.info.Size(),
		ModifiedAt:   job.info.ModTime().UTC(),
		ContentHash:  hash,
	}

	if !s.noMetadata {
		if meta, err := probeMetadata(ctx, job.fullPath); err == nil {
			entry.Metadata = meta
		}
	}

	return entry, nil
}

// computeFileHash computes the SHA-256 hash of a file.
// This matches internal/media/orphan_scanner.go:computeFileHash exactly.
func computeFileHash(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()

	hasher := sha256.New()
	if _, err := io.Copy(hasher, f); err != nil {
		return "", err
	}

	return hex.EncodeToString(hasher.Sum(nil)), nil
}

// probeMetadata uses ffprobe to extract tags and duration from an audio file.
func probeMetadata(ctx context.Context, filePath string) (*FileMetadata, error) {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, "ffprobe",
		"-v", "quiet",
		"-print_format", "json",
		"-show_format",
		filePath,
	)
	output, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("ffprobe: %w", err)
	}

	var probe struct {
		Format struct {
			Duration string            `json:"duration"`
			Tags     map[string]string `json:"tags"`
		} `json:"format"`
	}
	if err := json.Unmarshal(output, &probe); err != nil {
		return nil, fmt.Errorf("parse ffprobe output: %w", err)
	}

	meta := &FileMetadata{}

	if probe.Format.Duration != "" {
		if secs, err := strconv.ParseFloat(probe.Format.Duration, 64); err == nil {
			meta.DurationSeconds = secs
		}
	}

	for k, v := range probe.Format.Tags {
		switch strings.ToLower(k) {
		case "title":
			meta.Title = v
		case "artist":
			meta.Artist = v
		case "album":
			meta.Album = v
		case "genre":
			meta.Genre = v
		case "date", "year":
			meta.Year = v
		}
	}

	return meta, nil
}

func isMediaFile(name string) bool {
	ext := strings.ToLower(filepath.Ext(name))
	switch ext {
	case ".audio", ".mp3", ".flac", ".ogg", ".m4a", ".aac", ".wav", ".wma", ".opus":
		return true
	default:
		return false
	}
}
