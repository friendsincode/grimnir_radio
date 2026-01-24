/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/


package main

import (
	"context"
	"fmt"

	"github.com/friendsincode/grimnir_radio/internal/events"
	"github.com/friendsincode/grimnir_radio/internal/media"
	"github.com/friendsincode/grimnir_radio/internal/migration"
	"github.com/spf13/cobra"
)

var importCmd = &cobra.Command{
	Use:   "import",
	Short: "Import data from other radio automation systems",
	Long:  "Import stations, media, playlists, and schedules from AzuraCast, LibreTime, and other systems",
}

var importAzuracastCmd = &cobra.Command{
	Use:   "azuracast",
	Short: "Import from AzuraCast backup",
	Long:  "Import stations, media, and playlists from an AzuraCast backup tarball (.tar.gz)",
	RunE:  runImportAzuracast,
}

var importLibretimeCmd = &cobra.Command{
	Use:   "libretime",
	Short: "Import from LibreTime database",
	Long:  "Import stations, media, playlists, and shows from a LibreTime PostgreSQL database",
	RunE:  runImportLibretime,
}

// AzuraCast import flags
var (
	azuracastBackupPath string
	azuracastSkipMedia  bool
	azuracastDryRun     bool
)

// LibreTime import flags
var (
	libretimeDBHost     string
	libretimeDBPort     int
	libretimeDBName     string
	libretimeDBUser     string
	libretimeDBPassword string
	libretimeMediaPath  string
	libretimeSkipMedia  bool
	libretimeSkipPlaylists bool
	libretimeSkipSchedules bool
	libretimeDryRun     bool
)

func init() {
	rootCmd.AddCommand(importCmd)
	importCmd.AddCommand(importAzuracastCmd)
	importCmd.AddCommand(importLibretimeCmd)

	// AzuraCast flags
	importAzuracastCmd.Flags().StringVar(&azuracastBackupPath, "backup", "", "Path to AzuraCast backup tarball (.tar.gz) (required)")
	importAzuracastCmd.Flags().BoolVar(&azuracastSkipMedia, "skip-media", false, "Skip media file import")
	importAzuracastCmd.Flags().BoolVar(&azuracastDryRun, "dry-run", false, "Analyze backup without importing")
	importAzuracastCmd.MarkFlagRequired("backup")

	// LibreTime flags
	importLibretimeCmd.Flags().StringVar(&libretimeDBHost, "db-host", "localhost", "LibreTime database host")
	importLibretimeCmd.Flags().IntVar(&libretimeDBPort, "db-port", 5432, "LibreTime database port")
	importLibretimeCmd.Flags().StringVar(&libretimeDBName, "db-name", "airtime", "LibreTime database name")
	importLibretimeCmd.Flags().StringVar(&libretimeDBUser, "db-user", "", "LibreTime database user (required)")
	importLibretimeCmd.Flags().StringVar(&libretimeDBPassword, "db-password", "", "LibreTime database password")
	importLibretimeCmd.Flags().StringVar(&libretimeMediaPath, "media-path", "", "Path to LibreTime media directory")
	importLibretimeCmd.Flags().BoolVar(&libretimeSkipMedia, "skip-media", false, "Skip media file import")
	importLibretimeCmd.Flags().BoolVar(&libretimeSkipPlaylists, "skip-playlists", false, "Skip playlist import")
	importLibretimeCmd.Flags().BoolVar(&libretimeSkipSchedules, "skip-schedules", false, "Skip schedule/show import")
	importLibretimeCmd.Flags().BoolVar(&libretimeDryRun, "dry-run", false, "Analyze database without importing")
	importLibretimeCmd.MarkFlagRequired("db-user")
}

func runImportAzuracast(cmd *cobra.Command, args []string) error {
	if err := loadConfig(); err != nil {
		return err
	}

	logger.Info().
		Str("backup_path", azuracastBackupPath).
		Bool("dry_run", azuracastDryRun).
		Msg("starting AzuraCast import")

	// Initialize database
	db, err := initDatabase()
	if err != nil {
		return fmt.Errorf("initialize database: %w", err)
	}

	// Initialize media service
	mediaService, err := media.NewService(cfg, logger)
	if err != nil {
		return fmt.Errorf("initialize media service: %w", err)
	}

	// Create event bus
	bus := events.NewBus()

	// Create migration service
	migrationSvc := migration.NewService(db, bus, logger)
	migrationSvc.RegisterImporter(migration.SourceTypeAzuraCast, migration.NewAzuraCastImporter(db, mediaService, logger))

	// Create job options
	options := migration.Options{
		AzuraCastBackupPath: azuracastBackupPath,
		SkipMedia:           azuracastSkipMedia,
	}

	ctx := context.Background()

	// Dry run: just analyze
	if azuracastDryRun {
		logger.Info().Msg("performing dry run analysis...")
		importer := migration.NewAzuraCastImporter(db, mediaService, logger)

		if err := importer.Validate(ctx, options); err != nil {
			return fmt.Errorf("validation failed: %w", err)
		}

		result, err := importer.Analyze(ctx, options)
		if err != nil {
			return fmt.Errorf("analysis failed: %w", err)
		}

		logger.Info().Msg("dry run analysis complete")
		fmt.Printf("\nImport Preview:\n")
		fmt.Printf("  Stations:  %d\n", result.StationsCreated)
		fmt.Printf("  Media:     %d\n", result.MediaItemsImported)
		fmt.Printf("  Playlists: %d\n", result.PlaylistsCreated)

		if len(result.Warnings) > 0 {
			fmt.Printf("\nWarnings:\n")
			for _, warning := range result.Warnings {
				fmt.Printf("  - %s\n", warning)
			}
		}

		fmt.Printf("\nRun without --dry-run to perform the import.\n")
		return nil
	}

	// Create and start import job
	job, err := migrationSvc.CreateJob(ctx, migration.SourceTypeAzuraCast, options)
	if err != nil {
		return fmt.Errorf("create job: %w", err)
	}

	logger.Info().Str("job_id", job.ID).Msg("import job created")

	// Progress callback
	progressCallback := func(progress migration.Progress) {
		fmt.Printf("\r%s [%.0f%%] %s", progress.Phase, progress.Percentage, progress.CurrentStep)
		if progress.Phase == "completed" {
			fmt.Println()
		}
	}

	// Run import directly (not via service to show progress)
	importer := migration.NewAzuraCastImporter(db, mediaService, logger)
	result, err := importer.Import(ctx, options, progressCallback)
	if err != nil {
		logger.Error().Err(err).Msg("import failed")
		return fmt.Errorf("import failed: %w", err)
	}

	// Display results
	fmt.Printf("\n\nImport Complete!\n")
	fmt.Printf("  Stations:  %d created\n", result.StationsCreated)
	fmt.Printf("  Media:     %d imported\n", result.MediaItemsImported)
	fmt.Printf("  Playlists: %d created\n", result.PlaylistsCreated)
	fmt.Printf("  Duration:  %.1f seconds\n", result.DurationSeconds)

	if len(result.Warnings) > 0 {
		fmt.Printf("\nWarnings:\n")
		for _, warning := range result.Warnings {
			fmt.Printf("  - %s\n", warning)
		}
	}

	if len(result.Skipped) > 0 {
		fmt.Printf("\nSkipped:\n")
		for key, count := range result.Skipped {
			fmt.Printf("  - %s: %d\n", key, count)
		}
	}

	logger.Info().Msg("AzuraCast import completed successfully")
	return nil
}

func runImportLibretime(cmd *cobra.Command, args []string) error {
	if err := loadConfig(); err != nil {
		return err
	}

	logger.Info().
		Str("db_host", libretimeDBHost).
		Str("db_name", libretimeDBName).
		Bool("dry_run", libretimeDryRun).
		Msg("starting LibreTime import")

	// Initialize database
	db, err := initDatabase()
	if err != nil {
		return fmt.Errorf("initialize database: %w", err)
	}

	// Initialize media service
	mediaService, err := media.NewService(cfg, logger)
	if err != nil {
		return fmt.Errorf("initialize media service: %w", err)
	}

	// Create event bus
	bus := events.NewBus()

	// Create migration service
	migrationSvc := migration.NewService(db, bus, logger)
	migrationSvc.RegisterImporter(migration.SourceTypeLibreTime, migration.NewLibreTimeImporter(db, mediaService, logger))

	// Create job options
	options := migration.Options{
		LibreTimeDBHost:     libretimeDBHost,
		LibreTimeDBPort:     libretimeDBPort,
		LibreTimeDBName:     libretimeDBName,
		LibreTimeDBUser:     libretimeDBUser,
		LibreTimeDBPassword: libretimeDBPassword,
		LibreTimeMediaPath:  libretimeMediaPath,
		SkipMedia:           libretimeSkipMedia,
		SkipPlaylists:       libretimeSkipPlaylists,
		SkipSchedules:       libretimeSkipSchedules,
	}

	ctx := context.Background()

	// Dry run: just analyze
	if libretimeDryRun {
		logger.Info().Msg("performing dry run analysis...")
		importer := migration.NewLibreTimeImporter(db, mediaService, logger)

		if err := importer.Validate(ctx, options); err != nil {
			return fmt.Errorf("validation failed: %w", err)
		}

		result, err := importer.Analyze(ctx, options)
		if err != nil {
			return fmt.Errorf("analysis failed: %w", err)
		}

		logger.Info().Msg("dry run analysis complete")
		fmt.Printf("\nImport Preview:\n")
		fmt.Printf("  Stations:  %d\n", result.StationsCreated)
		fmt.Printf("  Media:     %d\n", result.MediaItemsImported)
		fmt.Printf("  Playlists: %d\n", result.PlaylistsCreated)
		fmt.Printf("  Shows:     %d\n", result.SchedulesCreated)

		if len(result.Warnings) > 0 {
			fmt.Printf("\nWarnings:\n")
			for _, warning := range result.Warnings {
				fmt.Printf("  - %s\n", warning)
			}
		}

		fmt.Printf("\nRun without --dry-run to perform the import.\n")
		return nil
	}

	// Create and start import job
	job, err := migrationSvc.CreateJob(ctx, migration.SourceTypeLibreTime, options)
	if err != nil {
		return fmt.Errorf("create job: %w", err)
	}

	logger.Info().Str("job_id", job.ID).Msg("import job created")

	// Progress callback
	progressCallback := func(progress migration.Progress) {
		status := fmt.Sprintf("%s [%.0f%%] %s", progress.Phase, progress.Percentage, progress.CurrentStep)

		// Add detailed counts if available
		if progress.MediaImported > 0 {
			status += fmt.Sprintf(" (%d/%d media)", progress.MediaImported, progress.MediaTotal)
		} else if progress.PlaylistsImported > 0 {
			status += fmt.Sprintf(" (%d/%d playlists)", progress.PlaylistsImported, progress.PlaylistsTotal)
		}

		fmt.Printf("\r%-100s", status)
		if progress.Phase == "completed" {
			fmt.Println()
		}
	}

	// Run import directly (not via service to show progress)
	importer := migration.NewLibreTimeImporter(db, mediaService, logger)
	result, err := importer.Import(ctx, options, progressCallback)
	if err != nil {
		logger.Error().Err(err).Msg("import failed")
		return fmt.Errorf("import failed: %w", err)
	}

	// Display results
	fmt.Printf("\n\nImport Complete!\n")
	fmt.Printf("  Stations:  %d created\n", result.StationsCreated)
	fmt.Printf("  Media:     %d imported\n", result.MediaItemsImported)
	fmt.Printf("  Playlists: %d created\n", result.PlaylistsCreated)
	fmt.Printf("  Shows:     %d imported as clocks\n", result.SchedulesCreated)
	fmt.Printf("  Duration:  %.1f seconds\n", result.DurationSeconds)

	if len(result.Warnings) > 0 {
		fmt.Printf("\nWarnings:\n")
		for _, warning := range result.Warnings {
			fmt.Printf("  - %s\n", warning)
		}
	}

	if len(result.Skipped) > 0 {
		fmt.Printf("\nSkipped:\n")
		for key, count := range result.Skipped {
			fmt.Printf("  - %s: %d\n", key, count)
		}
	}

	logger.Info().Msg("LibreTime import completed successfully")
	return nil
}
