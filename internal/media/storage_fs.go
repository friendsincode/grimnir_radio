/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/

package media

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/rs/zerolog"
)

// FilesystemStorage implements Storage using local filesystem.
type FilesystemStorage struct {
	rootDir string
	logger  zerolog.Logger
}

// NewFilesystemStorage creates a filesystem-based storage backend.
func NewFilesystemStorage(rootDir string, logger zerolog.Logger) *FilesystemStorage {
	return &FilesystemStorage{
		rootDir: rootDir,
		logger:  logger,
	}
}

// Store saves a file to the local filesystem.
func (fs *FilesystemStorage) Store(ctx context.Context, stationID, mediaID string, file io.Reader) (string, error) {
	// Infer extension from context or use default
	extension := ".audio" // Default extension

	// Build hierarchical path
	relativePath := buildMediaPath(stationID, mediaID, extension)
	fullPath := filepath.Join(fs.rootDir, relativePath)

	// Create parent directories
	if err := os.MkdirAll(filepath.Dir(fullPath), 0755); err != nil {
		return "", fmt.Errorf("create directories: %w", err)
	}

	// Create destination file
	dest, err := os.Create(fullPath)
	if err != nil {
		return "", fmt.Errorf("create file: %w", err)
	}
	defer dest.Close()

	// Copy uploaded file to destination
	if _, err := io.Copy(dest, file); err != nil {
		os.Remove(fullPath) // Clean up on failure
		return "", fmt.Errorf("write file: %w", err)
	}

	fs.logger.Debug().
		Str("path", fullPath).
		Str("relative_path", relativePath).
		Str("station_id", stationID).
		Str("media_id", mediaID).
		Msg("filesystem storage: file stored")

	// Return relative path for database storage (not fullPath)
	// The media root will be joined when reading
	return relativePath, nil
}

// Delete removes a file from the filesystem.
func (fs *FilesystemStorage) Delete(ctx context.Context, path string) error {
	// Join relative path with root directory (path should be relative from Store())
	fullPath := filepath.Join(fs.rootDir, path)
	if err := os.Remove(fullPath); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("remove file: %w", err)
	}

	fs.logger.Debug().Str("path", fullPath).Msg("filesystem storage: file deleted")
	return nil
}

// URL returns the local filesystem path.
func (fs *FilesystemStorage) URL(path string) string {
	return path
}

// CheckAccess verifies the storage directory exists and is accessible.
func (fs *FilesystemStorage) CheckAccess(ctx context.Context) error {
	info, err := os.Stat(fs.rootDir)
	if err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("media root directory does not exist: %s", fs.rootDir)
		}
		return fmt.Errorf("cannot access media root: %w", err)
	}
	if !info.IsDir() {
		return fmt.Errorf("media root is not a directory: %s", fs.rootDir)
	}
	return nil
}
