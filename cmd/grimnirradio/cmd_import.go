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
	Short: "Import from AzuraCast (backup or live API)",
	Long: `Import stations, media, and playlists from AzuraCast.

Two import modes are supported:

1. Backup file import:
   grimnirradio import azuracast --backup /path/to/backup.tar.gz

2. Live API import (connects to running AzuraCast instance):
   grimnirradio import azuracast --url https://azuracast.example.com --api-key YOUR_API_KEY

The API import downloads media files directly from AzuraCast and imports all
stations, mounts, media, playlists, and streamers/DJs that the API key has
access to.`,
	RunE: runImportAzuracast,
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
	azuracastAPIURL     string
	azuracastAPIKey     string
	azuracastUsername   string
	azuracastPassword   string
	azuracastSkipMedia  bool
	azuracastSkipUsers  bool
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
	importAzuracastCmd.Flags().StringVar(&azuracastBackupPath, "backup", "", "Path to AzuraCast backup tarball (.tar.gz)")
	importAzuracastCmd.Flags().StringVar(&azuracastAPIURL, "url", "", "AzuraCast instance URL for live API import (e.g., https://azuracast.example.com)")
	importAzuracastCmd.Flags().StringVar(&azuracastAPIKey, "api-key", "", "AzuraCast API key for live API import")
	importAzuracastCmd.Flags().StringVar(&azuracastUsername, "username", "", "AzuraCast username (alternative to API key)")
	importAzuracastCmd.Flags().StringVar(&azuracastPassword, "password", "", "AzuraCast password (alternative to API key)")
	importAzuracastCmd.Flags().BoolVar(&azuracastSkipMedia, "skip-media", false, "Skip media file import")
	importAzuracastCmd.Flags().BoolVar(&azuracastSkipUsers, "skip-users", false, "Skip streamer/DJ import (API mode only)")
	importAzuracastCmd.Flags().BoolVar(&azuracastDryRun, "dry-run", false, "Analyze without importing")

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

	// Validate flags - need either backup or API credentials
	hasBackup := azuracastBackupPath != ""
	hasAPIKey := azuracastAPIURL != "" && azuracastAPIKey != ""
	hasCredentials := azuracastAPIURL != "" && azuracastUsername != "" && azuracastPassword != ""

	if !hasBackup && !hasAPIKey && !hasCredentials {
		return fmt.Errorf("either --backup, or --url with --api-key, or --url with --username and --password is required")
	}
	if hasBackup && (hasAPIKey || hasCredentials) {
		return fmt.Errorf("cannot use both --backup and API credentials; choose one import method")
	}
	if hasAPIKey && hasCredentials {
		return fmt.Errorf("cannot use both --api-key and --username/--password; choose one authentication method")
	}
	if azuracastAPIURL != "" && !hasAPIKey && !hasCredentials {
		return fmt.Errorf("--url requires either --api-key or both --username and --password")
	}

	importMode := "backup"
	if hasAPIKey || hasCredentials {
		importMode = "api"
	}

	authMethod := "none"
	if hasAPIKey {
		authMethod = "api_key"
	} else if hasCredentials {
		authMethod = "credentials"
	}

	logger.Info().
		Str("mode", importMode).
		Str("auth_method", authMethod).
		Str("backup_path", azuracastBackupPath).
		Str("api_url", azuracastAPIURL).
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
		AzuraCastAPIURL:     azuracastAPIURL,
		AzuraCastAPIKey:     azuracastAPIKey,
		AzuraCastUsername:   azuracastUsername,
		AzuraCastPassword:   azuracastPassword,
		SkipMedia:           azuracastSkipMedia,
		SkipUsers:           azuracastSkipUsers,
	}

	ctx := context.Background()

	// Dry run: just analyze
	if azuracastDryRun {
		logger.Info().Msg("performing dry run analysis...")
		importer := migration.NewAzuraCastImporter(db, mediaService, logger)

		if err := importer.Validate(ctx, options); err != nil {
			return fmt.Errorf("validation failed: %w", err)
		}

		// Use detailed analysis for API mode
		if importMode == "api" {
			report, err := importer.AnalyzeDetailed(ctx, options)
			if err != nil {
				return fmt.Errorf("analysis failed: %w", err)
			}

			printAzuraCastReport(report)
		} else {
			result, err := importer.Analyze(ctx, options)
			if err != nil {
				return fmt.Errorf("analysis failed: %w", err)
			}

			logger.Info().Msg("dry run analysis complete")
			fmt.Printf("\nImport Preview (%s mode):\n", importMode)
			fmt.Printf("  Stations:   %d\n", result.StationsCreated)
			fmt.Printf("  Media:      %d\n", result.MediaItemsImported)
			fmt.Printf("  Playlists:  %d\n", result.PlaylistsCreated)

			if len(result.Warnings) > 0 {
				fmt.Printf("\nWarnings:\n")
				for _, warning := range result.Warnings {
					fmt.Printf("  - %s\n", warning)
				}
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
		status := fmt.Sprintf("\r%s [%.0f%%] %s", progress.Phase, progress.Percentage, progress.CurrentStep)
		if progress.MediaImported > 0 && progress.MediaTotal > 0 {
			status += fmt.Sprintf(" (%d/%d)", progress.MediaImported, progress.MediaTotal)
		}
		fmt.Print(status)
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
	fmt.Printf("  Stations:   %d created\n", result.StationsCreated)
	fmt.Printf("  Media:      %d imported\n", result.MediaItemsImported)
	fmt.Printf("  Playlists:  %d created\n", result.PlaylistsCreated)
	if result.UsersCreated > 0 {
		fmt.Printf("  Users/DJs:  %d created\n", result.UsersCreated)
	}
	fmt.Printf("  Duration:   %.1f seconds\n", result.DurationSeconds)

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

// printAzuraCastReport prints a detailed AzuraCast analysis report to stdout.
func printAzuraCastReport(report *migration.AnalysisReport) {
	fmt.Println()
	fmt.Println("╔══════════════════════════════════════════════════════════════════╗")
	fmt.Println("║             AzuraCast Import Preview (Dry Run)                   ║")
	fmt.Println("╚══════════════════════════════════════════════════════════════════╝")
	fmt.Println()

	// Summary table
	fmt.Println("┌─────────────────────────────────────────────────────────────────┐")
	fmt.Println("│ Summary                                                          │")
	fmt.Println("├─────────────────────────────────────────────────────────────────┤")
	fmt.Printf("│  Stations:            %-10d                                  │\n", report.TotalStations)
	fmt.Printf("│  Media Files:         %-10d                                  │\n", report.TotalMedia)
	fmt.Printf("│  Playlists:           %-10d                                  │\n", report.TotalPlaylists)
	fmt.Printf("│  Schedules:           %-10d                                  │\n", report.TotalSchedules)
	fmt.Printf("│  Streamers/DJs:       %-10d                                  │\n", report.TotalStreamers)
	fmt.Printf("│  Estimated Storage:   %-20s                        │\n", report.EstimatedStorageHuman)
	fmt.Println("└─────────────────────────────────────────────────────────────────┘")
	fmt.Println()

	// Detailed breakdown per station
	for i, station := range report.Stations {
		fmt.Printf("┌─── Station %d: %s ", i+1, station.Name)
		padding := 49 - len(station.Name)
		if padding > 0 {
			fmt.Print(repeatChar('─', padding))
		}
		fmt.Println("┐")

		if station.Description != "" {
			desc := station.Description
			if len(desc) > 60 {
				desc = desc[:57] + "..."
			}
			fmt.Printf("│  Description: %-50s │\n", desc)
		}

		fmt.Printf("│  Media Files: %-50d │\n", station.MediaCount)

		if station.StorageBytes > 0 {
			fmt.Printf("│  Storage:     %-50s │\n", formatBytesSimple(station.StorageBytes))
		}

		// Playlists
		if len(station.Playlists) > 0 {
			fmt.Println("│                                                                  │")
			fmt.Printf("│  Playlists (%d):                                                 │\n", len(station.Playlists))
			for _, pl := range station.Playlists {
				name := pl.Name
				if len(name) > 30 {
					name = name[:27] + "..."
				}
				fmt.Printf("│    • %-30s  (%s, %d items)         │\n", name, pl.Type, pl.ItemCount)
			}
		}

		// Mounts
		if len(station.Mounts) > 0 {
			fmt.Println("│                                                                  │")
			fmt.Printf("│  Mounts (%d):                                                    │\n", len(station.Mounts))
			for _, mt := range station.Mounts {
				fmt.Printf("│    • %-30s  (%s, %d kbps)           │\n", mt.Name, mt.Format, mt.Bitrate)
			}
		}

		// Streamers
		if len(station.Streamers) > 0 {
			fmt.Println("│                                                                  │")
			fmt.Printf("│  Streamers/DJs (%d):                                             │\n", len(station.Streamers))
			for _, st := range station.Streamers {
				name := st.DisplayName
				if name == "" {
					name = st.Username
				}
				if len(name) > 50 {
					name = name[:47] + "..."
				}
				fmt.Printf("│    • %-57s  │\n", name)
			}
		}

		fmt.Println("└──────────────────────────────────────────────────────────────────┘")
		fmt.Println()
	}

	// Warnings
	if len(report.Warnings) > 0 {
		fmt.Println("⚠ Warnings:")
		for _, warning := range report.Warnings {
			fmt.Printf("  • %s\n", warning)
		}
		fmt.Println()
	}
}

// repeatChar repeats a character n times.
func repeatChar(c rune, n int) string {
	if n <= 0 {
		return ""
	}
	result := make([]rune, n)
	for i := range result {
		result[i] = c
	}
	return string(result)
}

// formatBytesSimple converts bytes to human-readable format.
func formatBytesSimple(bytes int64) string {
	const (
		KB = 1024
		MB = KB * 1024
		GB = MB * 1024
	)

	switch {
	case bytes >= GB:
		return fmt.Sprintf("%.2f GB", float64(bytes)/GB)
	case bytes >= MB:
		return fmt.Sprintf("%.2f MB", float64(bytes)/MB)
	case bytes >= KB:
		return fmt.Sprintf("%.2f KB", float64(bytes)/KB)
	default:
		return fmt.Sprintf("%d bytes", bytes)
	}
}
