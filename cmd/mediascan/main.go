/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/

package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/spf13/cobra"
)

var (
	dirs       []string
	outputFile string
	workers    int
	noMetadata bool
	sourceType string
)

var rootCmd = &cobra.Command{
	Use:   "mediascan",
	Short: "Scan media directories and produce a JSON manifest",
	Long: `mediascan walks media directories, computes SHA-256 hashes, and optionally
extracts ID3 metadata via ffprobe. The output manifest is used by
"grimnirradio backfill" to update media item records with original filenames,
modification dates, and metadata.

Examples:
  mediascan --dir /var/azuracast/stations/*/media --type azuracast -o manifest.json
  mediascan --dir /srv/airtime/stor --type libretime -o manifest.json
  mediascan --dir /path/to/media  # output to stdout`,
	RunE: runScan,
}

func init() {
	rootCmd.Flags().StringArrayVar(&dirs, "dir", nil, "Media directory to scan (required, repeatable)")
	rootCmd.Flags().StringVarP(&outputFile, "output", "o", "", "Output file (default: stdout)")
	rootCmd.Flags().IntVarP(&workers, "workers", "w", 4, "Parallel hash workers")
	rootCmd.Flags().BoolVar(&noMetadata, "no-metadata", false, "Skip ffprobe metadata extraction")
	rootCmd.Flags().StringVar(&sourceType, "type", "", "Source type hint: azuracast or libretime")
	rootCmd.MarkFlagRequired("dir")
}

func main() {
	if err := rootCmd.Execute(); err != nil {
		os.Exit(1)
	}
}

func runScan(cmd *cobra.Command, args []string) error {
	if workers < 1 {
		workers = 1
	}

	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	s := &scanner{
		dirs:       dirs,
		workers:    workers,
		noMetadata: noMetadata,
		sourceType: sourceType,
	}

	fmt.Fprintf(os.Stderr, "Scanning %d director(y/ies) with %d workers...\n", len(dirs), workers)

	manifest, err := s.scan(ctx)
	if err != nil {
		return fmt.Errorf("scan failed: %w", err)
	}

	fmt.Fprintf(os.Stderr, "Scan complete: %d files, %d errors, %.1fs\n",
		manifest.Stats.TotalFiles, manifest.Stats.Errors, manifest.Stats.DurationSeconds)

	// Write output
	var out *os.File
	if outputFile != "" {
		out, err = os.Create(outputFile)
		if err != nil {
			return fmt.Errorf("create output file: %w", err)
		}
		defer out.Close()
	} else {
		out = os.Stdout
	}

	enc := json.NewEncoder(out)
	enc.SetIndent("", "  ")
	if err := enc.Encode(manifest); err != nil {
		return fmt.Errorf("encode manifest: %w", err)
	}

	if outputFile != "" {
		fmt.Fprintf(os.Stderr, "Manifest written to %s\n", outputFile)
	}

	return nil
}
