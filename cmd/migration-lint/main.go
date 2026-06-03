/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/

// Command migration-lint enforces the expand/contract discipline for SQL
// migrations: destructive operations (DROP/RENAME/ALTER TYPE/SET NOT NULL/
// TRUNCATE) must be accompanied by a "-- migration-contract: <reason>"
// annotation explaining why the operation is safe.
//
// Usage:
//
//	migration-lint                            lint everything in ./migrations
//	migration-lint --dir=./db/migrations      lint a different directory
//	migration-lint --diff-base=origin/main    lint only PR-changed files
package main

import (
	"flag"
	"fmt"
	"io"
	"os"
)

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

// run is main, but testable: takes args + writers, returns the exit code.
func run(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("migration-lint", flag.ContinueOnError)
	fs.SetOutput(stderr)
	dir := fs.String("dir", "migrations", "directory containing migration SQL files")
	diffBase := fs.String("diff-base", "", "git ref; if set, lint only files added/modified since this ref")
	if err := fs.Parse(args); err != nil {
		return 2
	}

	var files []string
	var err error
	if *diffBase != "" {
		files, err = DiscoverDiff(*diffBase)
	} else {
		files, err = DiscoverAll(*dir)
	}
	if err != nil {
		fmt.Fprintf(stderr, "migration-lint: %v\n", err)
		return 2
	}

	var findings []FileFinding
	for _, path := range files {
		content, readErr := os.ReadFile(path)
		if readErr != nil {
			fmt.Fprintf(stderr, "migration-lint: read %s: %v\n", path, readErr)
			return 2
		}
		findings = append(findings, LintFile(path, string(content))...)
	}

	if len(findings) == 0 {
		return 0
	}

	fmt.Fprintln(stdout, "Migration lint failures:")
	for _, f := range findings {
		fmt.Fprintln(stdout, FormatFinding(f))
	}
	fmt.Fprintln(stdout, "")
	fmt.Fprintln(stdout, "Add a `-- migration-contract: <reason>` comment to each file above,")
	fmt.Fprintln(stdout, "or split the destructive change into a later release per the")
	fmt.Fprintln(stdout, "expand/contract discipline (see docs/MIGRATIONS.md).")
	return 1
}
