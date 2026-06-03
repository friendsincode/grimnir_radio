/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/

package main

import (
	"bufio"
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
)

// DiscoverAll returns every *.sql file directly under dir, sorted lexicographically.
// Subdirectories and non-SQL files are ignored. Returns an error if dir cannot be read.
func DiscoverAll(dir string) ([]string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	var out []string
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		if !strings.HasSuffix(strings.ToLower(e.Name()), ".sql") {
			continue
		}
		out = append(out, filepath.Join(dir, e.Name()))
	}
	sort.Strings(out)
	return out, nil
}

// gitDiffNames is the function used by DiscoverDiff to invoke git.
// Exposed as a package variable so tests can stub it.
var gitDiffNames = realGitDiffNames

func realGitDiffNames(baseRef string) ([]string, error) {
	cmd := exec.Command("git", "diff", "--name-only", "--diff-filter=AM",
		baseRef+"...HEAD", "--", "migrations/*.sql")
	var out bytes.Buffer
	var stderr bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("git diff failed: %v: %s", err, stderr.String())
	}
	var names []string
	scanner := bufio.NewScanner(&out)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line != "" {
			names = append(names, line)
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	return names, nil
}

// DiscoverDiff returns SQL migration files added or modified between baseRef
// and HEAD. Returns empty (not error) when nothing changed.
func DiscoverDiff(baseRef string) ([]string, error) {
	return gitDiffNames(baseRef)
}
