package main

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/spf13/cobra"

	"github.com/friendsincode/grimnir_radio/internal/migration"
	"github.com/friendsincode/grimnir_radio/internal/migration/azuracast"
	"github.com/friendsincode/grimnir_radio/internal/migration/libretime"
)

var importCmd = &cobra.Command{
	Use:   "import",
	Short: "Import data from other broadcast automation systems",
	Long:  "Import stations, media, playlists, and schedules from other broadcast automation systems like AzuraCast and LibreTime",
}

var importAzuracastCmd = &cobra.Command{
	Use:   "azuracast [backup-file]",
	Short: "Import from AzuraCast backup",
	Long: `Import stations, mounts, media, playlists, and schedules from an AzuraCast backup.

The backup file should be a .tar.gz archive created by AzuraCast's backup system.
This command will extract the backup, read the database, and import all data into Grimnir Radio.

Example:
  grimnirradio import azuracast /path/to/azuracast-backup.tar.gz
  grimnirradio import azuracast /path/to/azuracast-backup.tar.gz --dry-run
  grimnirradio import azuracast /path/to/azuracast-backup.tar.gz --skip-media
  grimnirradio import azuracast /path/to/azuracast-backup.tar.gz --media-copy-method=symlink`,
	Args: cobra.ExactArgs(1),
	RunE: runImportAzuracast,
}

var importLibreTimeCmd = &cobra.Command{
	Use:   "libretime [database-dsn]",
	Short: "Import from LibreTime database",
	Long: `Import media, playlists, shows, and schedules from a LibreTime PostgreSQL database.

The database DSN should be in the format: postgres://user:password@host:port/database

This command connects directly to a running LibreTime database and imports all data.
LibreTime uses a single-station model, so a default station will be created.

Example:
  grimnirradio import libretime "postgres://airtime:airtime@localhost/airtime"
  grimnirradio import libretime "postgres://airtime:airtime@localhost/airtime" --dry-run
  grimnirradio import libretime "postgres://airtime:airtime@localhost/airtime" --skip-media
  grimnirradio import libretime "postgres://airtime:airtime@localhost/airtime" --media-copy-method=symlink`,
	Args: cobra.ExactArgs(1),
	RunE: runImportLibreTime,
}

var (
	importDryRun          bool
	importSkipMedia       bool
	importMediaCopyMethod string
	importOverwrite       bool
)

func init() {
	rootCmd.AddCommand(importCmd)
	importCmd.AddCommand(importAzuracastCmd)
	importCmd.AddCommand(importLibreTimeCmd)

	// AzuraCast flags
	importAzuracastCmd.Flags().BoolVar(&importDryRun, "dry-run", false, "Preview import without making changes")
	importAzuracastCmd.Flags().BoolVar(&importSkipMedia, "skip-media", false, "Skip importing media files")
	importAzuracastCmd.Flags().StringVar(&importMediaCopyMethod, "media-copy-method", "copy", "How to import media files: copy, symlink, or none")
	importAzuracastCmd.Flags().BoolVar(&importOverwrite, "overwrite", false, "Overwrite existing data")

	// LibreTime flags
	importLibreTimeCmd.Flags().BoolVar(&importDryRun, "dry-run", false, "Preview import without making changes")
	importLibreTimeCmd.Flags().BoolVar(&importSkipMedia, "skip-media", false, "Skip importing media files")
	importLibreTimeCmd.Flags().StringVar(&importMediaCopyMethod, "media-copy-method", "copy", "How to import media files: copy, symlink, or none")
	importLibreTimeCmd.Flags().BoolVar(&importOverwrite, "overwrite", false, "Overwrite existing data")
}

func runImportAzuracast(cmd *cobra.Command, args []string) error {
	if err := loadConfig(); err != nil {
		return err
	}

	backupPath := args[0]

	// Check if backup file exists
	if _, err := os.Stat(backupPath); err != nil {
		return fmt.Errorf("backup file not found: %w", err)
	}

	// Initialize database connection
	db, err := initDatabase()
	if err != nil {
		return fmt.Errorf("init database: %w", err)
	}

	// Create importer
	options := migration.MigrationOptions{
		DryRun:            importDryRun,
		SkipMedia:         importSkipMedia,
		MediaCopyMethod:   importMediaCopyMethod,
		OverwriteExisting: importOverwrite,
	}

	importer := azuracast.NewImporter(db, logger, options)

	// Set progress callback
	importer.SetProgressCallback(func(step, total int, message string) {
		percent := int(float64(step) / float64(total) * 100)
		fmt.Printf("[%3d%%] %s\n", percent, message)
	})

	// Run import
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
	defer cancel()

	logger.Info().
		Str("backup", backupPath).
		Bool("dry_run", importDryRun).
		Bool("skip_media", importSkipMedia).
		Str("media_copy_method", importMediaCopyMethod).
		Msg("starting AzuraCast import")

	fmt.Println("Starting AzuraCast import...")
	if importDryRun {
		fmt.Println("🔍 DRY RUN MODE - No changes will be made")
	}
	fmt.Println()

	stats, err := importer.Import(ctx, backupPath)
	if err != nil {
		return fmt.Errorf("import failed: %w", err)
	}

	// Print summary
	fmt.Println()
	fmt.Println("✅ Import completed successfully!")
	fmt.Println()
	fmt.Println("Summary:")
	fmt.Printf("  Stations imported:   %d\n", stats.StationsImported)
	fmt.Printf("  Mounts imported:     %d\n", stats.MountsImported)
	fmt.Printf("  Media imported:      %d\n", stats.MediaImported)
	fmt.Printf("  Playlists imported:  %d\n", stats.PlaylistsImported)
	fmt.Printf("  Schedules imported:  %d\n", stats.SchedulesImported)
	fmt.Printf("  Users imported:      %d\n", stats.UsersImported)
	fmt.Printf("  Errors encountered:  %d\n", stats.ErrorsEncountered)
	fmt.Println()

	if stats.UsersImported > 0 {
		fmt.Println("⚠️  Imported users require password reset (passwords cannot be migrated)")
	}

	if importDryRun {
		fmt.Println("🔍 This was a dry run - no changes were made to the database")
	}

	return nil
}

func runImportLibreTime(cmd *cobra.Command, args []string) error {
	if err := loadConfig(); err != nil {
		return err
	}

	dbDSN := args[0]

	// Initialize database connection
	db, err := initDatabase()
	if err != nil {
		return fmt.Errorf("init database: %w", err)
	}

	// Create importer
	options := migration.MigrationOptions{
		DryRun:            importDryRun,
		SkipMedia:         importSkipMedia,
		MediaCopyMethod:   importMediaCopyMethod,
		OverwriteExisting: importOverwrite,
	}

	importer := libretime.NewImporter(db, logger, options)

	// Set progress callback
	importer.SetProgressCallback(func(step, total int, message string) {
		percent := int(float64(step) / float64(total) * 100)
		fmt.Printf("[%3d%%] %s\n", percent, message)
	})

	// Run import
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
	defer cancel()

	logger.Info().
		Bool("dry_run", importDryRun).
		Bool("skip_media", importSkipMedia).
		Str("media_copy_method", importMediaCopyMethod).
		Msg("starting LibreTime import")

	fmt.Println("Starting LibreTime import...")
	if importDryRun {
		fmt.Println("🔍 DRY RUN MODE - No changes will be made")
	}
	fmt.Println()

	stats, err := importer.Import(ctx, dbDSN)
	if err != nil {
		return fmt.Errorf("import failed: %w", err)
	}

	// Print summary
	fmt.Println()
	fmt.Println("✅ Import completed successfully!")
	fmt.Println()
	fmt.Println("Summary:")
	fmt.Printf("  Stations imported:   %d\n", stats.StationsImported)
	fmt.Printf("  Mounts imported:     %d\n", stats.MountsImported)
	fmt.Printf("  Media imported:      %d\n", stats.MediaImported)
	fmt.Printf("  Playlists imported:  %d\n", stats.PlaylistsImported)
	fmt.Printf("  Schedules imported:  %d\n", stats.SchedulesImported)
	fmt.Printf("  Users imported:      %d\n", stats.UsersImported)
	fmt.Printf("  Errors encountered:  %d\n", stats.ErrorsEncountered)
	fmt.Println()

	if stats.UsersImported > 0 {
		fmt.Println("⚠️  Imported users require password reset (passwords cannot be migrated)")
	}

	if importDryRun {
		fmt.Println("🔍 This was a dry run - no changes were made to the database")
	}

	return nil
}
