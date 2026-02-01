/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/

package main

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/friendsincode/grimnir_radio/internal/db"
	"github.com/friendsincode/grimnir_radio/internal/migration"
	"github.com/friendsincode/grimnir_radio/internal/models"
)

var (
	resetForce       bool
	resetDeleteMedia bool
	resetKeepUsers   int
)

var resetCmd = &cobra.Command{
	Use:   "reset",
	Short: "Reset the database and optionally delete all media",
	Long: `Reset Grimnir Radio to a fresh state.

This command will:
- Drop all tables from the database (except optionally preserved users)
- Re-create empty tables
- Optionally delete all uploaded media files

WARNING: This action is irreversible! All data will be lost.

Examples:
  # Interactive reset (will prompt for confirmation)
  grimnirradio reset

  # Force reset without confirmation
  grimnirradio reset --force

  # Reset and delete all media files
  grimnirradio reset --force --delete-media

  # Reset but keep up to 3 admin users
  grimnirradio reset --force --keep-users=3
`,
	RunE: runReset,
}

func init() {
	resetCmd.Flags().BoolVarP(&resetForce, "force", "f", false, "Skip confirmation prompt")
	resetCmd.Flags().BoolVar(&resetDeleteMedia, "delete-media", false, "Also delete all uploaded media files")
	resetCmd.Flags().IntVar(&resetKeepUsers, "keep-users", 0, "Number of admin users to preserve (0 = delete all)")
	rootCmd.AddCommand(resetCmd)
}

func runReset(cmd *cobra.Command, args []string) error {
	if err := loadConfig(); err != nil {
		return err
	}

	// Confirmation prompt
	if !resetForce {
		fmt.Println("\n╔══════════════════════════════════════════════════════════════╗")
		fmt.Println("║                        WARNING                               ║")
		fmt.Println("╠══════════════════════════════════════════════════════════════╣")
		fmt.Println("║  This will DELETE ALL DATA from Grimnir Radio:               ║")
		fmt.Println("║                                                              ║")
		if resetKeepUsers > 0 {
			fmt.Printf("║  • All users EXCEPT the first %d admin user(s)               ║\n", resetKeepUsers)
		} else {
			fmt.Println("║  • All users and accounts                                    ║")
		}
		fmt.Println("║  • All stations and configurations                           ║")
		fmt.Println("║  • All playlists, smart blocks, and clocks                   ║")
		fmt.Println("║  • All schedules and play history                            ║")
		if resetDeleteMedia {
			fmt.Println("║  • ALL UPLOADED MEDIA FILES                                  ║")
		}
		fmt.Println("║                                                              ║")
		fmt.Println("║  This action CANNOT be undone!                               ║")
		fmt.Println("╚══════════════════════════════════════════════════════════════╝")
		fmt.Println()

		fmt.Print("Type 'yes' to confirm reset: ")
		reader := bufio.NewReader(os.Stdin)
		response, err := reader.ReadString('\n')
		if err != nil {
			return fmt.Errorf("failed to read input: %w", err)
		}

		response = strings.TrimSpace(strings.ToLower(response))
		if response != "yes" {
			fmt.Println("Reset cancelled.")
			return nil
		}
	}

	logger.Info().
		Bool("delete_media", resetDeleteMedia).
		Int("keep_users", resetKeepUsers).
		Msg("Starting database reset")

	// Connect to database
	database, err := db.Connect(cfg)
	if err != nil {
		return fmt.Errorf("connect to database: %w", err)
	}

	// Get the underlying SQL DB to close it later
	sqlDB, err := database.DB()
	if err != nil {
		return fmt.Errorf("get sql db: %w", err)
	}
	defer sqlDB.Close()

	// If keeping users, preserve them first
	var preservedUsers []models.User
	if resetKeepUsers > 0 {
		logger.Info().Int("count", resetKeepUsers).Msg("Preserving admin users")

		// Get platform admins first, then any other users if needed
		database.Where("platform_role = ?", models.PlatformRoleAdmin).
			Order("created_at ASC").
			Limit(resetKeepUsers).
			Find(&preservedUsers)

		// If we don't have enough admins, get other users
		if len(preservedUsers) < resetKeepUsers {
			remaining := resetKeepUsers - len(preservedUsers)
			var ids []string
			for _, u := range preservedUsers {
				ids = append(ids, u.ID)
			}

			var moreUsers []models.User
			query := database.Order("created_at ASC").Limit(remaining)
			if len(ids) > 0 {
				query = query.Where("id NOT IN ?", ids)
			}
			query.Find(&moreUsers)
			preservedUsers = append(preservedUsers, moreUsers...)
		}

		for _, u := range preservedUsers {
			logger.Info().
				Str("user_id", u.ID).
				Str("email", u.Email).
				Str("role", string(u.PlatformRole)).
				Msg("Preserving user")
		}
	}

	// Drop all tables in reverse order of dependencies
	tables := []interface{}{
		// Migration jobs first
		&migration.Job{},

		// Station resources (dependent on stations)
		&models.PlaylistItem{},
		&models.Playlist{},
		&models.ClockSlot{},
		&models.Clock{},
		&models.ClockHour{},
		&models.ScheduleEntry{},
		&models.PlayHistory{},
		&models.AnalysisJob{},
		&models.PrioritySource{},
		&models.ExecutorState{},
		&models.LiveSession{},
		&models.Webstream{},
		&models.SmartBlock{},
		&models.MediaTagLink{},
		&models.Tag{},
		&models.MediaItem{},
		&models.EncoderPreset{},
		&models.Mount{},

		// Station groups
		&models.StationGroupMember{},
		&models.StationGroup{},
		&models.StationUser{},
		&models.Station{},

		// Platform groups
		&models.PlatformGroupMember{},
		&models.PlatformGroup{},
		&models.User{},
	}

	logger.Info().Msg("Dropping all tables")
	for _, table := range tables {
		if err := database.Migrator().DropTable(table); err != nil {
			// Log but continue - table might not exist
			logger.Debug().Err(err).Msgf("drop table (may not exist)")
		}
	}

	// Delete media files if requested
	if resetDeleteMedia && cfg.MediaRoot != "" {
		logger.Info().Str("path", cfg.MediaRoot).Msg("Deleting media files...")

		// Walk through and delete all files in media root
		err := filepath.Walk(cfg.MediaRoot, func(path string, info os.FileInfo, err error) error {
			if err != nil {
				return err
			}
			// Don't delete the root directory itself
			if path == cfg.MediaRoot {
				return nil
			}
			// Delete files and empty directories
			if !info.IsDir() {
				if err := os.Remove(path); err != nil {
					logger.Warn().Err(err).Str("path", path).Msg("failed to delete file")
				}
			}
			return nil
		})
		if err != nil {
			logger.Warn().Err(err).Msg("error walking media directory")
		}

		// Clean up empty directories
		cleanEmptyDirs(cfg.MediaRoot)
		logger.Info().Msg("Media files deleted")
	}

	// Re-create tables
	logger.Info().Msg("Creating fresh database schema")
	if err := db.Migrate(database); err != nil {
		return fmt.Errorf("migrate database: %w", err)
	}

	// Restore preserved users
	if len(preservedUsers) > 0 {
		logger.Info().Int("count", len(preservedUsers)).Msg("Restoring preserved users")
		for _, u := range preservedUsers {
			// Keep original CreatedAt, set UpdatedAt to match
			u.UpdatedAt = u.CreatedAt

			if err := database.Create(&u).Error; err != nil {
				logger.Error().Err(err).Str("email", u.Email).Msg("failed to restore user")
			} else {
				logger.Info().
					Str("user_id", u.ID).
					Str("email", u.Email).
					Msg("User restored")
			}
		}
	}

	logger.Info().Msg("Reset complete")
	fmt.Println()
	fmt.Println("╔══════════════════════════════════════════════════════════════╗")
	fmt.Println("║                    Reset Complete!                           ║")
	fmt.Println("╠══════════════════════════════════════════════════════════════╣")
	fmt.Println("║  Grimnir Radio has been reset to a fresh state.              ║")
	fmt.Println("║                                                              ║")
	fmt.Println("║  Next steps:                                                 ║")
	fmt.Println("║  1. Start the server: grimnirradio serve                     ║")
	fmt.Println("║  2. Visit the web UI to run the setup wizard                 ║")
	fmt.Println("╚══════════════════════════════════════════════════════════════╝")
	fmt.Println()

	return nil
}

// cleanEmptyDirs removes empty directories recursively
func cleanEmptyDirs(root string) {
	filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil || !info.IsDir() || path == root {
			return nil
		}

		// Check if directory is empty
		entries, err := os.ReadDir(path)
		if err != nil {
			return nil
		}

		if len(entries) == 0 {
			os.Remove(path)
		}
		return nil
	})
}
