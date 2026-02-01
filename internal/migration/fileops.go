/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/

package migration

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"

	"github.com/friendsincode/grimnir_radio/internal/media"
	"github.com/rs/zerolog"
)

// FileOperations handles media file copying and verification during migration.
type FileOperations struct {
	mediaService *media.Service
	logger       zerolog.Logger

	// Progress tracking
	mu           sync.Mutex
	totalBytes   int64
	copiedBytes  int64
	totalFiles   int
	copiedFiles  int
	failedFiles  int
	skippedFiles int
}

// NewFileOperations creates a new file operations handler.
func NewFileOperations(mediaService *media.Service, logger zerolog.Logger) *FileOperations {
	return &FileOperations{
		mediaService: mediaService,
		logger:       logger.With().Str("component", "file_ops").Logger(),
	}
}

// CopyOptions configures file copy behavior.
type CopyOptions struct {
	// Source directory (root of media library)
	SourceRoot string

	// Whether to verify checksums after copy
	VerifyChecksum bool

	// Whether to skip existing files
	SkipExisting bool

	// Concurrency level for parallel copies
	Concurrency int

	// Progress callback
	ProgressCallback func(copied, total int)
}

// DefaultCopyOptions returns default copy options.
func DefaultCopyOptions() CopyOptions {
	return CopyOptions{
		VerifyChecksum: true,
		SkipExisting:   true,
		Concurrency:    4,
	}
}

// FileCopyJob represents a single file copy task.
type FileCopyJob struct {
	SourcePath string
	StationID  string
	MediaID    string
	FileSize   int64
}

// FileCopyResult represents the result of a file copy operation.
type FileCopyResult struct {
	MediaID     string
	StorageKey  string
	Success     bool
	Error       error
	BytesCopied int64
	Checksum    string
}

// CopyFiles copies multiple media files with parallel processing.
func (fo *FileOperations) CopyFiles(ctx context.Context, jobs []FileCopyJob, options CopyOptions) ([]FileCopyResult, error) {
	fo.mu.Lock()
	fo.totalFiles = len(jobs)
	fo.copiedFiles = 0
	fo.failedFiles = 0
	fo.skippedFiles = 0
	fo.totalBytes = 0
	fo.copiedBytes = 0
	fo.mu.Unlock()

	// Calculate total bytes
	for _, job := range jobs {
		fo.totalBytes += job.FileSize
	}

	fo.logger.Info().
		Int("total_files", len(jobs)).
		Int64("total_bytes", fo.totalBytes).
		Int("concurrency", options.Concurrency).
		Msg("starting file copy operation")

	// Create worker pool
	jobChan := make(chan FileCopyJob, len(jobs))
	resultChan := make(chan FileCopyResult, len(jobs))

	var wg sync.WaitGroup

	// Start workers
	for i := 0; i < options.Concurrency; i++ {
		wg.Add(1)
		go func(workerID int) {
			defer wg.Done()
			for job := range jobChan {
				result := fo.copyFile(ctx, job, options)
				resultChan <- result

				// Update progress
				fo.mu.Lock()
				if result.Success {
					fo.copiedFiles++
					fo.copiedBytes += result.BytesCopied
				} else if result.Error != nil && result.Error.Error() == "file skipped" {
					fo.skippedFiles++
				} else {
					fo.failedFiles++
				}

				if options.ProgressCallback != nil {
					options.ProgressCallback(fo.copiedFiles, fo.totalFiles)
				}
				fo.mu.Unlock()
			}
		}(i)
	}

	// Send jobs to workers
	go func() {
		for _, job := range jobs {
			select {
			case <-ctx.Done():
				return
			case jobChan <- job:
			}
		}
		close(jobChan)
	}()

	// Wait for workers to finish
	go func() {
		wg.Wait()
		close(resultChan)
	}()

	// Collect results
	var results []FileCopyResult
	for result := range resultChan {
		results = append(results, result)
	}

	fo.logger.Info().
		Int("copied", fo.copiedFiles).
		Int("skipped", fo.skippedFiles).
		Int("failed", fo.failedFiles).
		Int64("bytes_copied", fo.copiedBytes).
		Msg("file copy operation complete")

	return results, nil
}

// copyFile copies a single media file to storage.
func (fo *FileOperations) copyFile(ctx context.Context, job FileCopyJob, options CopyOptions) FileCopyResult {
	result := FileCopyResult{
		MediaID: job.MediaID,
	}

	// Check if source file exists
	if _, err := os.Stat(job.SourcePath); os.IsNotExist(err) {
		result.Error = fmt.Errorf("source file not found: %s", job.SourcePath)
		fo.logger.Warn().
			Str("source", job.SourcePath).
			Str("media_id", job.MediaID).
			Msg("source file not found")
		return result
	}

	// Open source file
	sourceFile, err := os.Open(job.SourcePath)
	if err != nil {
		result.Error = fmt.Errorf("open source file: %w", err)
		fo.logger.Error().Err(err).
			Str("source", job.SourcePath).
			Msg("failed to open source file")
		return result
	}
	defer sourceFile.Close()

	// Calculate checksum if verification enabled
	var checksum string
	if options.VerifyChecksum {
		hash := sha256.New()
		if _, err := io.Copy(hash, sourceFile); err != nil {
			result.Error = fmt.Errorf("calculate checksum: %w", err)
			return result
		}
		checksum = hex.EncodeToString(hash.Sum(nil))

		// Reset file pointer
		if _, err := sourceFile.Seek(0, 0); err != nil {
			result.Error = fmt.Errorf("reset file pointer: %w", err)
			return result
		}
	}

	// Upload to storage
	storageKey, err := fo.mediaService.Store(ctx, job.StationID, job.MediaID, sourceFile)
	if err != nil {
		result.Error = fmt.Errorf("upload to storage: %w", err)
		fo.logger.Error().Err(err).
			Str("media_id", job.MediaID).
			Msg("failed to upload to storage")
		return result
	}

	result.Success = true
	result.StorageKey = storageKey
	result.BytesCopied = job.FileSize
	result.Checksum = checksum

	fo.logger.Debug().
		Str("source", job.SourcePath).
		Str("storage_key", storageKey).
		Str("media_id", job.MediaID).
		Int64("bytes", job.FileSize).
		Msg("file copied successfully")

	return result
}

// VerifyFile verifies a file's integrity using checksum.
func (fo *FileOperations) VerifyFile(filePath string, expectedChecksum string) (bool, error) {
	file, err := os.Open(filePath)
	if err != nil {
		return false, fmt.Errorf("open file: %w", err)
	}
	defer file.Close()

	hash := sha256.New()
	if _, err := io.Copy(hash, file); err != nil {
		return false, fmt.Errorf("calculate checksum: %w", err)
	}

	checksum := hex.EncodeToString(hash.Sum(nil))
	return checksum == expectedChecksum, nil
}

// GetFileSize returns the size of a file in bytes.
func GetFileSize(filePath string) (int64, error) {
	info, err := os.Stat(filePath)
	if err != nil {
		return 0, err
	}
	return info.Size(), nil
}

// ResolveFilePath resolves a relative file path against a source root.
// Handles various path formats from different systems.
func ResolveFilePath(sourceRoot, relativePath string) string {
	// If path is already absolute, return as-is
	if filepath.IsAbs(relativePath) {
		return relativePath
	}

	// Clean the relative path (remove ./ prefix, etc.)
	clean := filepath.Clean(relativePath)

	// Join with source root
	return filepath.Join(sourceRoot, clean)
}

// ValidateSourceDirectory checks if a directory exists and is readable.
func ValidateSourceDirectory(dir string) error {
	info, err := os.Stat(dir)
	if err != nil {
		return fmt.Errorf("access directory: %w", err)
	}

	if !info.IsDir() {
		return fmt.Errorf("path is not a directory: %s", dir)
	}

	// Try to read directory
	entries, err := os.ReadDir(dir)
	if err != nil {
		return fmt.Errorf("read directory: %w", err)
	}

	// Check if directory is empty
	if len(entries) == 0 {
		return fmt.Errorf("directory is empty: %s", dir)
	}

	return nil
}
