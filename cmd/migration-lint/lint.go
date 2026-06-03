/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/

package main

import (
	"regexp"
	"strings"
)

// Finding represents one destructive SQL operation found in a migration file.
type Finding struct {
	Op      string // canonical name, e.g. "DROP COLUMN"
	Line    int    // 1-based line number where the operation was found
	Snippet string // the matched line (trimmed)
}

// destructiveRule maps a regex (case-insensitive) to a canonical op name.
// Order matters only for stability of error messages: most-specific rules first.
var destructiveRules = []struct {
	canonical string
	pattern   *regexp.Regexp
}{
	{"ALTER COLUMN SET NOT NULL", regexp.MustCompile(`(?i)\balter\s+column\s+[\w."]+\s+set\s+not\s+null\b`)},
	{"ALTER COLUMN TYPE", regexp.MustCompile(`(?i)\balter\s+column\s+[\w."]+\s+(set\s+data\s+)?type\b`)},
	{"RENAME COLUMN", regexp.MustCompile(`(?i)\brename\s+column\b`)},
	{"RENAME TABLE", regexp.MustCompile(`(?i)\balter\s+table\s+[\w."]+\s+rename\s+to\b`)},
	{"DROP COLUMN", regexp.MustCompile(`(?i)\bdrop\s+column\b`)},
	{"DROP TABLE", regexp.MustCompile(`(?i)\bdrop\s+table\b`)},
	{"DROP INDEX", regexp.MustCompile(`(?i)\bdrop\s+index\b`)},
	{"TRUNCATE", regexp.MustCompile(`(?i)\btruncate(\s+table)?\b`)},
}

// detectDestructive scans SQL text and returns one Finding per destructive operation.
// Lines that are entirely inside SQL line comments ("--") are skipped.
// A line that ends with a comment is examined only up to the comment marker.
func detectDestructive(sql string) []Finding {
	var findings []Finding
	for i, raw := range strings.Split(sql, "\n") {
		line := raw
		if idx := strings.Index(line, "--"); idx >= 0 {
			line = line[:idx]
		}
		if strings.TrimSpace(line) == "" {
			continue
		}
		for _, rule := range destructiveRules {
			if rule.pattern.MatchString(line) {
				findings = append(findings, Finding{
					Op:      rule.canonical,
					Line:    i + 1,
					Snippet: strings.TrimSpace(raw),
				})
				break // one finding per line is enough; we don't double-flag
			}
		}
	}
	return findings
}

// contractAnnotation matches a "-- migration-contract: <non-empty reason>" comment.
// The marker is case-insensitive; the reason must contain at least one non-space character.
var contractAnnotation = regexp.MustCompile(`(?im)^[^\S\n]*--[^\S\n]*migration-contract[^\S\n]*:[^\S\n]*(\S.*)$`)

// hasContractAnnotation reports whether the SQL contains a non-empty
// "-- migration-contract: <reason>" annotation.
func hasContractAnnotation(sql string) bool {
	return contractAnnotation.MatchString(sql)
}
