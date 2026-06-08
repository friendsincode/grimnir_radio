/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/

// Command uuid-trap-lint fails the build if any non-test Go file binds the
// empty string "" to a column whose name ends in _id. Postgres rejects "" on
// uuid-typed columns with SQLSTATE 22P02 ("invalid input syntax for type
// uuid"). SQLite accepts the same value, so the bug survives every test on
// the sqlite-backed test driver & detonates the first time a deploy hits a
// real Postgres instance.
//
// Issues #223, #228, & #242 all traced back to this trap; the 2026-06-08
// audit added this lint so future regressions get caught at PR time rather
// than in prod.
//
// Fix: bind nil instead of "". GORM serializes a Go nil to SQL NULL, which
// every Postgres uuid column (nullable or not) accepts as a parameter. For
// non-nullable FKs, supply a real id or remove the assignment.
//
// Usage:
//
//	uuid-trap-lint                # lint all non-test Go files in the repo
//	uuid-trap-lint --root=./other # lint a different repo root
package main

import (
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
)

// Patterns that the lint refuses to allow. Each one represents a way to bind
// "" to a *_id column via GORM. Names are mentioned in the failure output so
// the dev knows which family of trap they tripped.
var patterns = []struct {
	name string
	re   *regexp.Regexp
}{
	// Update("foo_id", "")
	{"Update single-column", regexp.MustCompile(`\.Update\("[a-zA-Z_]+_id"\s*,\s*""\s*\)`)},
	// "foo_id": "" inside any map literal (covers Updates(map[string]any{...}))
	{"map-literal value", regexp.MustCompile(`"[a-zA-Z_]+_id"\s*:\s*""`)},
	// updates["foo_id"] = ""
	{"map-index assignment", regexp.MustCompile(`updates\["[a-zA-Z_]+_id"\]\s*=\s*""`)},
}

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

// run is main, but testable: takes args + writers & returns the exit code.
func run(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("uuid-trap-lint", flag.ContinueOnError)
	fs.SetOutput(stderr)
	root := fs.String("root", ".", "repository root to scan")
	if err := fs.Parse(args); err != nil {
		return 2
	}

	files, err := discoverGoFiles(*root)
	if err != nil {
		fmt.Fprintf(stderr, "uuid-trap-lint: %v\n", err)
		return 2
	}

	var hits []finding
	for _, path := range files {
		content, readErr := os.ReadFile(path)
		if readErr != nil {
			fmt.Fprintf(stderr, "uuid-trap-lint: read %s: %v\n", path, readErr)
			return 2
		}
		hits = append(hits, scanFile(path, string(content))...)
	}

	if len(hits) == 0 {
		fmt.Fprintf(stdout, "uuid-trap-lint: clean (%d files scanned)\n", len(files))
		return 0
	}

	fmt.Fprintln(stdout, "uuid-trap-lint: empty-string bound to *_id column.")
	fmt.Fprintln(stdout, "Postgres rejects this on uuid columns with SQLSTATE 22P02; bind nil")
	fmt.Fprintln(stdout, "instead. GORM serializes Go nil to SQL NULL. See issue #242.")
	fmt.Fprintln(stdout, "")
	for _, h := range hits {
		fmt.Fprintf(stdout, "  %s:%d: %s\n    %s\n", h.path, h.line, h.pattern, h.text)
	}
	fmt.Fprintf(stdout, "\n%d trap site(s) found.\n", len(hits))
	return 1
}

// finding is one offending line in one file.
type finding struct {
	path    string
	line    int
	pattern string
	text    string
}

// scanFile returns every offending line in content under any of the lint
// patterns. _test.go files are skipped at the discovery layer.
func scanFile(path, content string) []finding {
	var hits []finding
	for i, line := range strings.Split(content, "\n") {
		for _, p := range patterns {
			if p.re.MatchString(line) {
				hits = append(hits, finding{
					path:    path,
					line:    i + 1,
					pattern: p.name,
					text:    strings.TrimSpace(line),
				})
			}
		}
	}
	return hits
}

// discoverGoFiles lists every tracked non-test .go file in root. Uses git
// ls-files so we ignore vendored deps, the .git tree, & gitignored output.
// Falls back to a filepath.Walk if git isn't available (e.g. in CI shards
// without git history).
func discoverGoFiles(root string) ([]string, error) {
	out, err := exec.Command("git", "-C", root, "ls-files", "*.go").Output()
	if err == nil {
		return filterTracked(root, string(out)), nil
	}
	// Fallback: walk the tree.
	var files []string
	walkErr := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			// Skip vendored & build output directories.
			name := info.Name()
			if name == "vendor" || name == ".git" || name == "node_modules" {
				return filepath.SkipDir
			}
			return nil
		}
		if strings.HasSuffix(path, ".go") && !strings.HasSuffix(path, "_test.go") {
			files = append(files, path)
		}
		return nil
	})
	if walkErr != nil {
		return nil, fmt.Errorf("walk %s: %w", root, walkErr)
	}
	return files, nil
}

// filterTracked turns `git ls-files` output into absolute paths, dropping
// _test.go files. The lint is meant for production code only; test JSON
// request bodies legitimately carry `"foo_id": ""` inside string literals.
func filterTracked(root, out string) []string {
	var files []string
	for _, rel := range strings.Split(out, "\n") {
		rel = strings.TrimSpace(rel)
		if rel == "" || strings.HasSuffix(rel, "_test.go") {
			continue
		}
		files = append(files, filepath.Join(root, rel))
	}
	return files
}
