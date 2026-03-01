/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/

package main

import (
	"encoding/json"
	"fmt"
	"os"
	"time"

	"github.com/friendsincode/grimnir_radio/internal/models"
	"github.com/spf13/cobra"
	"gorm.io/gorm"
)

// Manifest types (duplicated from cmd/mediascan to avoid import dependency).

type backfillManifest struct {
	Version    int                `json:"version"`
	SourceType string             `json:"source_type"`
	ScannedAt  time.Time          `json:"scanned_at"`
	RootDirs   []string           `json:"root_dirs"`
	Files      []backfillFileEntry `json:"files"`
}

type backfillFileEntry struct {
	Path         string                 `json:"path"`
	RelativePath string                 `json:"relative_path"`
	Filename     string                 `json:"filename"`
	Size         int64                  `json:"size"`
	ModifiedAt   time.Time              `json:"modified_at"`
	ContentHash  string                 `json:"content_hash"`
	Metadata     *backfillFileMetadata  `json:"metadata,omitempty"`
}

type backfillFileMetadata struct {
	Title           string  `json:"title,omitempty"`
	Artist          string  `json:"artist,omitempty"`
	Album           string  `json:"album,omitempty"`
	Genre           string  `json:"genre,omitempty"`
	Year            string  `json:"year,omitempty"`
	DurationSeconds float64 `json:"duration_seconds,omitempty"`
}

// Backfill flags
var (
	backfillManifestPath string
	backfillFillMetadata bool
	backfillForce        bool
	backfillDryRun       bool
	backfillStationID    string
)

var backfillCmd = &cobra.Command{
	Use:   "backfill",
	Short: "Backfill media metadata from a mediascan manifest",
	Long: `Reads a JSON manifest produced by mediascan and updates matching media items
in the database with original filenames, file modification dates, and
optionally ID3 metadata (title, artist, album, genre, year).

Matching is done by content_hash (SHA-256). Only blank fields are updated
unless --force is specified.

Examples:
  grimnirradio backfill --manifest azuracast-manifest.json --dry-run
  grimnirradio backfill --manifest azuracast-manifest.json --fill-metadata
  grimnirradio backfill --manifest manifest.json --force --station-id <uuid>`,
	RunE: runBackfill,
}

func init() {
	rootCmd.AddCommand(backfillCmd)

	backfillCmd.Flags().StringVar(&backfillManifestPath, "manifest", "", "Path to mediascan JSON manifest (required)")
	backfillCmd.Flags().BoolVar(&backfillFillMetadata, "fill-metadata", false, "Also backfill title/artist/album/genre/year from manifest")
	backfillCmd.Flags().BoolVar(&backfillForce, "force", false, "Overwrite existing values (default: only fill blanks)")
	backfillCmd.Flags().BoolVar(&backfillDryRun, "dry-run", false, "Report what would change without writing")
	backfillCmd.Flags().StringVar(&backfillStationID, "station-id", "", "Limit to a specific station (optional)")
	backfillCmd.MarkFlagRequired("manifest")
}

func runBackfill(cmd *cobra.Command, args []string) error {
	if err := loadConfig(); err != nil {
		return err
	}

	// Read and parse manifest
	data, err := os.ReadFile(backfillManifestPath)
	if err != nil {
		return fmt.Errorf("read manifest: %w", err)
	}

	var manifest backfillManifest
	if err := json.Unmarshal(data, &manifest); err != nil {
		return fmt.Errorf("parse manifest: %w", err)
	}

	if manifest.Version != 1 {
		return fmt.Errorf("unsupported manifest version: %d", manifest.Version)
	}

	fmt.Printf("Manifest: %d files from %s (scanned %s)\n",
		len(manifest.Files), manifest.SourceType, manifest.ScannedAt.Format(time.RFC3339))

	// Build hash→entry map (first occurrence with richest metadata wins)
	hashMap := make(map[string]backfillFileEntry, len(manifest.Files))
	for _, f := range manifest.Files {
		if f.ContentHash == "" {
			continue
		}
		if existing, ok := hashMap[f.ContentHash]; ok {
			// Keep the entry with richer metadata
			if f.Metadata != nil && existing.Metadata == nil {
				hashMap[f.ContentHash] = f
			}
			continue
		}
		hashMap[f.ContentHash] = f
	}

	fmt.Printf("Unique hashes in manifest: %d\n", len(hashMap))

	// Initialize database
	database, err := initDatabase()
	if err != nil {
		return fmt.Errorf("initialize database: %w", err)
	}

	// Collect all hashes for batch query
	hashes := make([]string, 0, len(hashMap))
	for h := range hashMap {
		hashes = append(hashes, h)
	}

	// Query matching media items in batches of 500
	type mediaRow struct {
		ID               string
		StationID        string
		ContentHash      string
		OriginalFilename string
		FileModifiedAt   *time.Time
		Title            string
		Artist           string
		Album            string
		Genre            string
		Year             string
	}

	var allRows []mediaRow
	const batchSize = 500
	for i := 0; i < len(hashes); i += batchSize {
		end := i + batchSize
		if end > len(hashes) {
			end = len(hashes)
		}
		batch := hashes[i:end]

		var rows []mediaRow
		q := database.Model(&models.MediaItem{}).
			Select("id, station_id, content_hash, original_filename, file_modified_at, title, artist, album, genre, year").
			Where("content_hash IN ?", batch)
		if backfillStationID != "" {
			q = q.Where("station_id = ?", backfillStationID)
		}
		if err := q.Find(&rows).Error; err != nil {
			return fmt.Errorf("query media items: %w", err)
		}
		allRows = append(allRows, rows...)
	}

	fmt.Printf("Matching DB records: %d\n\n", len(allRows))

	// Process matches
	var updated, skipped, errors int
	for _, row := range allRows {
		entry, ok := hashMap[row.ContentHash]
		if !ok {
			continue
		}

		updates := make(map[string]interface{})

		// Original filename
		if backfillForce || row.OriginalFilename == "" {
			if entry.Filename != "" && entry.Filename != row.OriginalFilename {
				updates["original_filename"] = entry.Filename
			}
		}

		// File modified at
		if backfillForce || row.FileModifiedAt == nil {
			if !entry.ModifiedAt.IsZero() {
				updates["file_modified_at"] = entry.ModifiedAt
			}
		}

		// Metadata fields (only if --fill-metadata)
		if backfillFillMetadata && entry.Metadata != nil {
			meta := entry.Metadata
			if (backfillForce || row.Title == "") && meta.Title != "" {
				updates["title"] = meta.Title
			}
			if (backfillForce || row.Artist == "") && meta.Artist != "" {
				updates["artist"] = meta.Artist
			}
			if (backfillForce || row.Album == "") && meta.Album != "" {
				updates["album"] = meta.Album
			}
			if (backfillForce || row.Genre == "") && meta.Genre != "" {
				updates["genre"] = meta.Genre
			}
			if (backfillForce || row.Year == "") && meta.Year != "" {
				updates["year"] = meta.Year
			}
		}

		if len(updates) == 0 {
			skipped++
			continue
		}

		if backfillDryRun {
			fmt.Printf("  [dry-run] %s: would update %v\n", row.ID[:8], mapKeys(updates))
			updated++
			continue
		}

		if err := database.Model(&models.MediaItem{}).Where("id = ?", row.ID).Updates(updates).Error; err != nil {
			fmt.Fprintf(os.Stderr, "  error updating %s: %v\n", row.ID[:8], err)
			errors++
			continue
		}
		updated++
	}

	// Summary
	unmatched := len(hashMap) - len(allRows)
	if unmatched < 0 {
		unmatched = 0
	}

	fmt.Printf("\nBackfill %s:\n", modeLabel(backfillDryRun))
	fmt.Printf("  Updated:          %d\n", updated)
	fmt.Printf("  Already populated: %d\n", skipped)
	fmt.Printf("  Errors:           %d\n", errors)
	fmt.Printf("  Unmatched (manifest only): %d\n", unmatched)

	return nil
}

func mapKeys(m map[string]interface{}) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	return keys
}

func modeLabel(dryRun bool) string {
	if dryRun {
		return "Complete (dry run)"
	}
	return "Complete"
}

// initDatabaseForBackfill opens a DB connection using the loaded config.
// We reuse the existing initDatabase function from main.go.
var _ = initDatabase // ensure initDatabase is available (defined in main.go)

// Ensure gorm is used (the import is needed for database.Model calls above).
var _ *gorm.DB
