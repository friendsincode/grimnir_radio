# Expand/Contract Migration Discipline + CI Lint — Implementation Plan

> **For agentic workers:** REQUIRED: Use superpowers:subagent-driven-development (if subagents available) or superpowers:executing-plans to implement this plan. Steps use checkbox (`- [ ]`) syntax for tracking.

> **Status:** Complete. 14 tasks across 5 chunks. Plan written 2026-06-03 incrementally per `feedback_brainstorming_incremental_save.md` — every chunk saved before the next was drafted.

**Goal:** Add a CI-enforced migration discipline so v(N) and v(N+1) of the control plane can run side-by-side during rolling updates without one of them breaking on schema drift.

**Architecture:** Three deliverables, none depend on each other in implementation order but ALL must ship together to make the discipline real: (1) a small `cmd/migration-lint/` Go binary that scans new/modified `migrations/*.sql` files (either in a git diff range or the whole directory) for destructive operations and fails unless an explicit `-- migration-contract: <reason>` annotation is present; (2) `make ci` integration so the lint runs on every build; (3) documentation (`CLAUDE.md` section + `docs/MIGRATIONS.md` long-form + `migrations/TEMPLATE.sql` scaffold) so authors know what the rule means and how to comply.

**Tech Stack:** Go 1.24, standard library only (no SQL-parser dependency — line-based regex is sufficient for the patterns we detect and far more debuggable than a full parser; we explicitly accept that this is conservative and may false-positive on contrived SQL). `git diff --name-only` invoked via `exec.Command` for the diff-scan mode. Makefile changes. Markdown docs.

**Issue:** [#234](https://github.com/friendsincode/grimnir_radio/issues/234)

**Spec reference:** `docs/superpowers/plans/2026-06-01-ha-zero-loss-failover-design.md` Section 4 (Schema migration discipline) + Section 9.1 Track B-1 (day-1 work).

**Scope decision (Q-PB1, 2026-06-03):** Phase 1 lints `.sql` files only. GORM struct-level removals (`AutoMigrate` destructive cases) are out of scope for this issue; a follow-up is fine if real misses appear in practice. This is intentional YAGNI for phase 1.

---

## File structure

| File | Status | Responsibility |
|---|---|---|
| `cmd/migration-lint/main.go` | Create | CLI entry point, flag parsing, exit code mapping |
| `cmd/migration-lint/lint.go` | Create | Core lint logic: classify SQL lines, detect destructive ops, detect annotation, return findings |
| `cmd/migration-lint/lint_test.go` | Create | Table-driven unit tests for every detection rule |
| `cmd/migration-lint/scan.go` | Create | Discover files to lint: directory mode (`--all`) and git-diff mode (`--diff-base=<ref>`) |
| `cmd/migration-lint/scan_test.go` | Create | Tests for both discovery modes (directory walks against test fixtures; git-diff against `exec.Command` mocked via a test helper script) |
| `cmd/migration-lint/testdata/` | Create | SQL fixture files: clean, destructive-without-annotation, destructive-with-annotation, edge cases (comments, multi-line SQL, etc.) |
| `Makefile` | Modify | Add `migration-lint` target; add it to `ci` |
| `CLAUDE.md` | Modify | New "Database Migrations (expand/contract discipline)" section |
| `docs/MIGRATIONS.md` | Create | Long-form: rule, three phases, worked examples for "add column," "rename column," "drop column," "narrow type" |
| `migrations/TEMPLATE.sql` | Create | Scaffold file authors copy when writing a new migration; contains the phase-comment block prompting expand/contract thought |

**Decomposition principle:** lint logic is pure functions taking SQL text and returning findings (zero git/file deps, easy to unit-test). File discovery is separate (filesystem + git). Main wires them together. This is the smallest reasonable separation: each file < 200 lines, each test file mirrors its source one-to-one.

---

## Chunk 1: Core lint engine

The lint engine is pure: takes SQL text + filename, returns a list of findings. No filesystem, no git, no exit codes. Just regex + line iteration.

### Task 1: Detect destructive SQL patterns

**Files:**
- Create: `cmd/migration-lint/lint.go`
- Create: `cmd/migration-lint/lint_test.go`

**Context:**
The patterns to flag (from spec Section 4 + issue #234) are: `DROP COLUMN`, `DROP TABLE`, `DROP INDEX`, `RENAME COLUMN`, `RENAME TABLE`, `ALTER COLUMN ... TYPE` (any), `ALTER COLUMN ... SET NOT NULL`, `TRUNCATE`. Case-insensitive. Inside `--` line comments doesn't count.

- [ ] **Step 1: Write the failing test**

`cmd/migration-lint/lint_test.go`:

```go
/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/

package main

import (
	"testing"
)

func TestDetectDestructive_TableOfPatterns(t *testing.T) {
	tests := []struct {
		name    string
		sql     string
		wantOp  string // empty if no destructive op should be found
	}{
		{"drop column", "ALTER TABLE foo DROP COLUMN bar;", "DROP COLUMN"},
		{"drop column lowercase", "alter table foo drop column bar;", "DROP COLUMN"},
		{"drop table", "DROP TABLE foo;", "DROP TABLE"},
		{"drop index", "DROP INDEX idx_foo;", "DROP INDEX"},
		{"rename column", "ALTER TABLE foo RENAME COLUMN a TO b;", "RENAME COLUMN"},
		{"rename table", "ALTER TABLE foo RENAME TO bar;", "RENAME TABLE"},
		{"alter type", "ALTER TABLE foo ALTER COLUMN bar TYPE integer;", "ALTER COLUMN TYPE"},
		{"set not null", "ALTER TABLE foo ALTER COLUMN bar SET NOT NULL;", "ALTER COLUMN SET NOT NULL"},
		{"truncate", "TRUNCATE foo;", "TRUNCATE"},
		{"truncate table", "TRUNCATE TABLE foo;", "TRUNCATE"},
		{"in comment, ignored", "-- DROP COLUMN bar -- this is a comment", ""},
		{"add column, safe", "ALTER TABLE foo ADD COLUMN bar text;", ""},
		{"create table, safe", "CREATE TABLE foo (id uuid PRIMARY KEY);", ""},
		{"create index, safe", "CREATE INDEX idx_foo ON foo(bar);", ""},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			ops := detectDestructive(tc.sql)
			if tc.wantOp == "" {
				if len(ops) != 0 {
					t.Errorf("detectDestructive(%q) = %v, want no findings", tc.sql, ops)
				}
				return
			}
			if len(ops) != 1 {
				t.Fatalf("detectDestructive(%q) = %v, want 1 finding (%q)", tc.sql, ops, tc.wantOp)
			}
			if ops[0].Op != tc.wantOp {
				t.Errorf("detectDestructive(%q) op = %q, want %q", tc.sql, ops[0].Op, tc.wantOp)
			}
		})
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

```bash
go test -v -run "TestDetectDestructive_TableOfPatterns" ./cmd/migration-lint/
```

Expected: FAIL — `detectDestructive` undefined.

- [ ] **Step 3: Implement `detectDestructive`**

`cmd/migration-lint/lint.go`:

```go
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
	{"ALTER COLUMN SET NOT NULL", regexp.MustCompile(`(?i)\balter\s+column\s+\w+\s+set\s+not\s+null\b`)},
	{"ALTER COLUMN TYPE", regexp.MustCompile(`(?i)\balter\s+column\s+\w+\s+type\b`)},
	{"RENAME COLUMN", regexp.MustCompile(`(?i)\brename\s+column\b`)},
	{"RENAME TABLE", regexp.MustCompile(`(?i)\balter\s+table\s+\w+\s+rename\s+to\b`)},
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
```

- [ ] **Step 4: Run test to verify it passes**

```bash
go test -v -run "TestDetectDestructive_TableOfPatterns" ./cmd/migration-lint/
```

Expected: PASS, all 14 subtests green.

- [ ] **Step 5: Commit**

```bash
git add cmd/migration-lint/lint.go cmd/migration-lint/lint_test.go
git commit -m "migration-lint: detect destructive SQL patterns"
```

---

### Task 2: Detect `-- migration-contract:` annotation

**Files:**
- Modify: `cmd/migration-lint/lint.go` (add `hasContractAnnotation`)
- Modify: `cmd/migration-lint/lint_test.go` (add tests)

**Context:**
A `-- migration-contract: <reason>` annotation anywhere in a migration file marks it as a knowingly-destructive migration. The `<reason>` must be non-empty; an empty annotation is treated as missing.

- [ ] **Step 1: Write the failing test**

Append to `cmd/migration-lint/lint_test.go`:

```go
func TestHasContractAnnotation(t *testing.T) {
	tests := []struct {
		name string
		sql  string
		want bool
	}{
		{"explicit annotation", "-- migration-contract: dropping foo, replaced by bar in v1.40\nDROP COLUMN foo;", true},
		{"annotation with surrounding whitespace", "  --   migration-contract:   reason here\n", true},
		{"missing annotation", "DROP COLUMN foo;", false},
		{"empty annotation", "-- migration-contract:\nDROP COLUMN foo;", false},
		{"annotation with only whitespace reason", "-- migration-contract:    \nDROP COLUMN foo;", false},
		{"annotation case-insensitive marker", "-- MIGRATION-CONTRACT: reason\nDROP COLUMN foo;", true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := hasContractAnnotation(tc.sql)
			if got != tc.want {
				t.Errorf("hasContractAnnotation(%q) = %v, want %v", tc.sql, got, tc.want)
			}
		})
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

```bash
go test -v -run "TestHasContractAnnotation" ./cmd/migration-lint/
```

Expected: FAIL — `hasContractAnnotation` undefined.

- [ ] **Step 3: Implement `hasContractAnnotation`**

Append to `cmd/migration-lint/lint.go`:

```go
// contractAnnotation matches a "-- migration-contract: <non-empty reason>" comment.
// The marker is case-insensitive; the reason must contain at least one non-space character.
var contractAnnotation = regexp.MustCompile(`(?im)^\s*--\s*migration-contract\s*:\s*(\S.*)$`)

// hasContractAnnotation reports whether the SQL contains a non-empty
// "-- migration-contract: <reason>" annotation.
func hasContractAnnotation(sql string) bool {
	return contractAnnotation.MatchString(sql)
}
```

- [ ] **Step 4: Run test to verify it passes**

```bash
go test -v -run "TestHasContractAnnotation" ./cmd/migration-lint/
```

Expected: PASS, all 6 subtests green.

- [ ] **Step 5: Commit**

```bash
git add cmd/migration-lint/lint.go cmd/migration-lint/lint_test.go
git commit -m "migration-lint: detect migration-contract annotation"
```

---

### Task 3: Combine into per-file lint

**Files:**
- Modify: `cmd/migration-lint/lint.go` (add `LintFile`)
- Modify: `cmd/migration-lint/lint_test.go`

**Context:**
`LintFile` is the public-facing function: take a path + SQL content, return a list of `FileFinding` (a `Finding` plus the path) only when the file is destructive AND lacks the annotation. Files with no destructive ops, or destructive ops with the annotation, return no findings.

- [ ] **Step 1: Write the failing test**

Append to `cmd/migration-lint/lint_test.go`:

```go
func TestLintFile(t *testing.T) {
	tests := []struct {
		name         string
		path         string
		sql          string
		wantFindings int
	}{
		{
			name:         "clean migration",
			path:         "migrations/100_add_column.sql",
			sql:          "ALTER TABLE foo ADD COLUMN bar text;",
			wantFindings: 0,
		},
		{
			name:         "destructive without annotation",
			path:         "migrations/101_drop_column.sql",
			sql:          "ALTER TABLE foo DROP COLUMN bar;",
			wantFindings: 1,
		},
		{
			name:         "destructive with annotation",
			path:         "migrations/102_drop_column_annotated.sql",
			sql:          "-- migration-contract: foo deprecated in v1.40, contracted in v1.43\nALTER TABLE foo DROP COLUMN bar;",
			wantFindings: 0,
		},
		{
			name:         "two destructive ops without annotation",
			path:         "migrations/103_two_drops.sql",
			sql:          "DROP TABLE foo;\nDROP TABLE bar;",
			wantFindings: 2,
		},
		{
			name:         "two destructive ops with annotation",
			path:         "migrations/104_two_drops_annotated.sql",
			sql:          "-- migration-contract: deprecated tables\nDROP TABLE foo;\nDROP TABLE bar;",
			wantFindings: 0,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := LintFile(tc.path, tc.sql)
			if len(got) != tc.wantFindings {
				t.Errorf("LintFile findings = %d, want %d (%+v)", len(got), tc.wantFindings, got)
			}
			for _, f := range got {
				if f.Path != tc.path {
					t.Errorf("finding path = %q, want %q", f.Path, tc.path)
				}
			}
		})
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

```bash
go test -v -run "TestLintFile" ./cmd/migration-lint/
```

Expected: FAIL — `LintFile`, `FileFinding` undefined.

- [ ] **Step 3: Implement `LintFile`**

Append to `cmd/migration-lint/lint.go`:

```go
// FileFinding is a destructive operation found in a specific file that
// lacked a contract annotation.
type FileFinding struct {
	Path string
	Finding
}

// LintFile reports destructive operations in `sql` that are not covered by a
// migration-contract annotation. A clean file returns nil; a fully-annotated
// destructive file returns nil; a destructive file without annotation returns
// one FileFinding per destructive operation.
func LintFile(path, sql string) []FileFinding {
	ops := detectDestructive(sql)
	if len(ops) == 0 {
		return nil
	}
	if hasContractAnnotation(sql) {
		return nil
	}
	out := make([]FileFinding, 0, len(ops))
	for _, op := range ops {
		out = append(out, FileFinding{Path: path, Finding: op})
	}
	return out
}
```

- [ ] **Step 4: Run test to verify it passes**

```bash
go test -v ./cmd/migration-lint/
```

Expected: PASS — all of Task 1, 2, and 3 green.

- [ ] **Step 5: Commit**

```bash
git add cmd/migration-lint/lint.go cmd/migration-lint/lint_test.go
git commit -m "migration-lint: per-file lint combining detection and annotation"
```

---

### Task 4: Human-readable error messages

**Files:**
- Modify: `cmd/migration-lint/lint.go` (add `FormatFinding`)
- Modify: `cmd/migration-lint/lint_test.go`

**Context:**
At 3am the operator wants to read the lint output and know exactly what to do. Each finding should print as one line: `<path>:<line>: destructive operation <op> without -- migration-contract annotation: <snippet>`.

- [ ] **Step 1: Write the failing test**

```go
func TestFormatFinding(t *testing.T) {
	f := FileFinding{
		Path: "migrations/099_drop.sql",
		Finding: Finding{
			Op:      "DROP COLUMN",
			Line:    7,
			Snippet: "ALTER TABLE foo DROP COLUMN bar;",
		},
	}
	got := FormatFinding(f)
	want := "migrations/099_drop.sql:7: destructive operation DROP COLUMN without -- migration-contract annotation: ALTER TABLE foo DROP COLUMN bar;"
	if got != want {
		t.Errorf("FormatFinding =\n  %q\nwant\n  %q", got, want)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

```bash
go test -v -run "TestFormatFinding" ./cmd/migration-lint/
```

Expected: FAIL — `FormatFinding` undefined.

- [ ] **Step 3: Implement `FormatFinding`**

```go
import "fmt"

// FormatFinding renders a FileFinding as a single human-readable line.
func FormatFinding(f FileFinding) string {
	return fmt.Sprintf("%s:%d: destructive operation %s without -- migration-contract annotation: %s",
		f.Path, f.Line, f.Op, f.Snippet)
}
```

- [ ] **Step 4: Run test to verify it passes**

```bash
go test -v ./cmd/migration-lint/
```

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add cmd/migration-lint/lint.go cmd/migration-lint/lint_test.go
git commit -m "migration-lint: human-readable finding format"
```

---

## Chunk 2: File discovery

Two modes:

- **Directory mode** (`--all`): walk a directory (default `migrations/`), return every `*.sql` file. Used for "lint everything" sanity checks in CI when there's no diff base.
- **Diff mode** (`--diff-base=<ref>`): run `git diff --name-only --diff-filter=AM <ref>...HEAD -- migrations/*.sql` and return the list of changed files. Used in PR-style CI.

Both modes return a flat `[]string` of paths relative to the repo root.

### Task 5: Directory mode discovery

**Files:**
- Create: `cmd/migration-lint/scan.go`
- Create: `cmd/migration-lint/scan_test.go`
- Create: `cmd/migration-lint/testdata/migrations_clean/100_add.sql`
- Create: `cmd/migration-lint/testdata/migrations_clean/101_index.sql`
- Create: `cmd/migration-lint/testdata/migrations_clean/notes.txt` (a non-SQL file to verify the filter works)

**Context:**
The function `DiscoverAll(dir string) ([]string, error)` returns every `*.sql` file under `dir` in deterministic (lexicographic) order. Non-`.sql` files are ignored. Subdirectories are NOT recursed (the project keeps migrations flat).

- [ ] **Step 1: Create test fixtures**

```bash
mkdir -p cmd/migration-lint/testdata/migrations_clean
cat > cmd/migration-lint/testdata/migrations_clean/100_add.sql <<'SQL'
ALTER TABLE foo ADD COLUMN bar text;
SQL
cat > cmd/migration-lint/testdata/migrations_clean/101_index.sql <<'SQL'
CREATE INDEX idx_foo ON foo(bar);
SQL
cat > cmd/migration-lint/testdata/migrations_clean/notes.txt <<'TXT'
not a migration, must be ignored
TXT
```

- [ ] **Step 2: Write the failing test**

`cmd/migration-lint/scan_test.go`:

```go
/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/

package main

import (
	"reflect"
	"testing"
)

func TestDiscoverAll(t *testing.T) {
	got, err := DiscoverAll("testdata/migrations_clean")
	if err != nil {
		t.Fatalf("DiscoverAll error: %v", err)
	}
	want := []string{
		"testdata/migrations_clean/100_add.sql",
		"testdata/migrations_clean/101_index.sql",
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("DiscoverAll = %v\nwant %v", got, want)
	}
}

func TestDiscoverAll_MissingDir(t *testing.T) {
	_, err := DiscoverAll("testdata/does_not_exist")
	if err == nil {
		t.Error("DiscoverAll on missing dir: want error, got nil")
	}
}
```

- [ ] **Step 3: Run test to verify it fails**

```bash
go test -v -run "TestDiscoverAll" ./cmd/migration-lint/
```

Expected: FAIL — `DiscoverAll` undefined.

- [ ] **Step 4: Implement `DiscoverAll`**

`cmd/migration-lint/scan.go`:

```go
/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/

package main

import (
	"os"
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
```

- [ ] **Step 5: Run test to verify it passes**

```bash
go test -v -run "TestDiscoverAll" ./cmd/migration-lint/
```

Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add cmd/migration-lint/scan.go cmd/migration-lint/scan_test.go cmd/migration-lint/testdata/
git commit -m "migration-lint: directory-mode file discovery"
```

---

### Task 6: Git diff mode discovery

**Files:**
- Modify: `cmd/migration-lint/scan.go` (add `DiscoverDiff`)
- Modify: `cmd/migration-lint/scan_test.go`

**Context:**
`DiscoverDiff(baseRef string)` runs `git diff --name-only --diff-filter=AM <baseRef>...HEAD -- 'migrations/*.sql'` and returns the list of files added or modified since `baseRef`. The `--diff-filter=AM` means added or modified (we don't lint deletions — deleting a migration file is itself suspicious but a different problem). For testability, the git command is invoked through a variable function so tests can stub it.

- [ ] **Step 1: Write the failing test**

Append to `cmd/migration-lint/scan_test.go`:

```go
func TestDiscoverDiff(t *testing.T) {
	// Stub out the git runner.
	original := gitDiffNames
	defer func() { gitDiffNames = original }()

	gitDiffNames = func(baseRef string) ([]string, error) {
		if baseRef != "origin/main" {
			t.Errorf("git called with baseRef = %q, want %q", baseRef, "origin/main")
		}
		return []string{
			"migrations/099_drop.sql",
			"migrations/100_add.sql",
		}, nil
	}

	got, err := DiscoverDiff("origin/main")
	if err != nil {
		t.Fatalf("DiscoverDiff error: %v", err)
	}
	want := []string{
		"migrations/099_drop.sql",
		"migrations/100_add.sql",
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("DiscoverDiff = %v\nwant %v", got, want)
	}
}

func TestDiscoverDiff_EmptyDiff(t *testing.T) {
	original := gitDiffNames
	defer func() { gitDiffNames = original }()
	gitDiffNames = func(string) ([]string, error) { return nil, nil }

	got, err := DiscoverDiff("origin/main")
	if err != nil {
		t.Fatalf("DiscoverDiff error: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("DiscoverDiff on empty diff = %v, want empty", got)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

```bash
go test -v -run "TestDiscoverDiff" ./cmd/migration-lint/
```

Expected: FAIL — `DiscoverDiff`, `gitDiffNames` undefined.

- [ ] **Step 3: Implement `DiscoverDiff`**

Append to `cmd/migration-lint/scan.go`:

```go
import (
	"bufio"
	"bytes"
	"fmt"
	"os/exec"
)

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
```

(Note: the existing scan.go already imports `strings`; the new imports are `bufio`, `bytes`, `fmt`, and `os/exec`. Merge into the existing import block — don't add a duplicate `import` statement.)

- [ ] **Step 4: Run test to verify it passes**

```bash
go test -v ./cmd/migration-lint/
```

Expected: PASS, all Chunk 1 + Chunk 2 tests green.

- [ ] **Step 5: Commit**

```bash
git add cmd/migration-lint/scan.go cmd/migration-lint/scan_test.go
git commit -m "migration-lint: git-diff-mode file discovery"
```

---

## Chunk 3: CLI wiring

`main.go` is the entry point. Two flags:

- `--dir=<path>` (default `migrations`): the directory to scan when no `--diff-base` is given.
- `--diff-base=<ref>` (optional): if set, lint only files changed between this ref and HEAD; otherwise lint everything in `--dir`.

Exit codes:

- `0` — no findings (clean).
- `1` — findings printed to stdout, build should fail.
- `2` — internal error (cannot read file, git failure, etc.), printed to stderr.

### Task 7: Main entry point

**Files:**
- Create: `cmd/migration-lint/main.go`

**Context:**
Main wires `DiscoverAll`/`DiscoverDiff` to `LintFile` and prints findings. Reads file content with `os.ReadFile`. Keep it short; logic stays in the testable functions.

- [ ] **Step 1: Write the integration test**

Create `cmd/migration-lint/main_test.go`:

```go
/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/

package main

import (
	"bytes"
	"strings"
	"testing"
)

func TestRun_CleanDir(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := run([]string{"--dir=testdata/migrations_clean"}, &stdout, &stderr)
	if code != 0 {
		t.Errorf("clean dir exit code = %d, want 0; stderr=%q", code, stderr.String())
	}
	if stdout.Len() != 0 {
		t.Errorf("clean dir stdout = %q, want empty", stdout.String())
	}
}

func TestRun_DirtyDir(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := run([]string{"--dir=testdata/migrations_dirty"}, &stdout, &stderr)
	if code != 1 {
		t.Errorf("dirty dir exit code = %d, want 1; stderr=%q", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "DROP COLUMN") {
		t.Errorf("dirty dir stdout = %q, want to contain 'DROP COLUMN'", stdout.String())
	}
}

func TestRun_MissingDir(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := run([]string{"--dir=testdata/does_not_exist"}, &stdout, &stderr)
	if code != 2 {
		t.Errorf("missing dir exit code = %d, want 2", code)
	}
}
```

- [ ] **Step 2: Create the dirty fixture**

```bash
mkdir -p cmd/migration-lint/testdata/migrations_dirty
cat > cmd/migration-lint/testdata/migrations_dirty/200_drop.sql <<'SQL'
ALTER TABLE foo DROP COLUMN bar;
SQL
```

- [ ] **Step 3: Run the test to verify it fails**

```bash
go test -v -run "TestRun" ./cmd/migration-lint/
```

Expected: FAIL — `run` undefined.

- [ ] **Step 4: Implement `main.go`**

`cmd/migration-lint/main.go`:

```go
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
```

- [ ] **Step 5: Run test to verify it passes**

```bash
go test -v ./cmd/migration-lint/
```

Expected: PASS, all of Chunk 1, 2, and 3 green.

- [ ] **Step 6: Smoke-test the binary**

```bash
go run ./cmd/migration-lint --dir=cmd/migration-lint/testdata/migrations_clean ; echo "exit=$?"
go run ./cmd/migration-lint --dir=cmd/migration-lint/testdata/migrations_dirty ; echo "exit=$?"
```

Expected: first run exits `0`; second run prints `DROP COLUMN` finding and exits `1`.

- [ ] **Step 7: Commit**

```bash
git add cmd/migration-lint/main.go cmd/migration-lint/main_test.go cmd/migration-lint/testdata/migrations_dirty/
git commit -m "migration-lint: CLI entry point with dir and diff-base modes"
```

---

### Task 8: End-to-end test against the real `migrations/` directory

**Files:**
- No new files. Just runs the binary against actual project state and confirms a clean exit.

**Context:**
The project's two existing migrations (`001_add_performance_indexes.sql`, `002_fix_media_paths.sql`) are both expand-only. The lint must report zero findings against them. If it doesn't, either the migrations are misclassified or the lint is wrong; both cases require fixing before the lint can ship.

- [ ] **Step 1: Run lint against real migrations**

```bash
go run ./cmd/migration-lint --dir=migrations ; echo "exit=$?"
```

Expected: exit `0`, no output.

- [ ] **Step 2: If exit ≠ 0**

Inspect the output. Two possibilities:

- The lint is over-flagging a benign migration (e.g., flagging `DROP INDEX IF EXISTS` when the index is being immediately recreated). If so, examine which rule fires and decide whether to (a) refine the regex, (b) accept the false positive and add a `-- migration-contract: <reason>` annotation to the existing migration with a clear explanation, or (c) leave it as a known limitation documented in `docs/MIGRATIONS.md`.
- The migration genuinely is destructive and was never annotated. If so, add the annotation retroactively per the rule. Each retroactive annotation is its own commit with the reason explained.

- [ ] **Step 3: Re-run lint until clean**

```bash
go run ./cmd/migration-lint --dir=migrations ; echo "exit=$?"
```

Expected: exit `0`.

- [ ] **Step 4: Commit any annotations added in Step 2**

```bash
git add migrations/
git commit -m "migrations: add migration-contract annotations to legacy migrations"
```

(Skip this step if no annotations were needed.)

---

## Chunk 4: Makefile integration

The lint becomes part of `make ci` so every push (and every CI run) enforces the discipline. Two flavors:

- **Local dev (`make migration-lint`)**: runs in `--dir=migrations` mode (lint everything; predictable, no git dependency).
- **CI (`make migration-lint-ci`)**: runs in `--diff-base=$BASE_REF` mode if `BASE_REF` is set, otherwise falls back to `--dir` mode. This lets the GitHub Actions workflow pass the base ref of the PR for a fast targeted lint.

### Task 9: Add the Makefile target

**Files:**
- Modify: `Makefile`

**Context:**
The existing `Makefile` lists phony targets on a single line near the top. Add `migration-lint` and `migration-lint-ci` to that list. The target builds the binary once and runs it.

- [ ] **Step 1: Read the current `.PHONY` line and `ci` target**

```bash
grep -n "^\.PHONY:\|^ci:\|^verify:" Makefile
```

- [ ] **Step 2: Add `migration-lint` to `.PHONY` and to `ci`**

Edit `Makefile`:

1. Append `migration-lint migration-lint-ci` to the `.PHONY:` line near the top.
2. Add the targets near `lint`:

```makefile
migration-lint:
	@$(GO) run ./cmd/migration-lint --dir=migrations

migration-lint-ci:
	@if [ -n "$$BASE_REF" ]; then \
		$(GO) run ./cmd/migration-lint --diff-base=$$BASE_REF; \
	else \
		$(GO) run ./cmd/migration-lint --dir=migrations; \
	fi
```

3. Modify the `ci` line to call the lint:

```makefile
ci: verify fmt-check migration-lint-ci
```

- [ ] **Step 3: Run `make migration-lint`**

```bash
make migration-lint
```

Expected: silent success (exit 0). If anything fails, fix per Task 8 Step 2.

- [ ] **Step 4: Run the full `make ci`**

```bash
make ci
```

Expected: full CI gate passes, including the new lint step. If a real existing test fails for unrelated reasons, fix or document; don't ship migration-lint while the build is broken.

- [ ] **Step 5: Commit**

```bash
git add Makefile
git commit -m "Makefile: integrate migration-lint into ci"
```

---

### Task 10: Negative test — confirm CI catches a destructive migration

**Files:**
- Temporary: a deliberate destructive migration to verify the lint fires, then deleted.

**Context:**
The lint must actually break the build when something destructive ships without annotation. This task verifies that end-to-end against the real `make ci` gate, then reverts.

- [ ] **Step 1: Create a deliberately destructive migration (temporary)**

```bash
cat > migrations/099_DELETE_ME_test_lint.sql <<'SQL'
-- Test fixture: should fail migration-lint
ALTER TABLE foo DROP COLUMN bar;
SQL
```

- [ ] **Step 2: Run `make migration-lint`**

```bash
make migration-lint ; echo "exit=$?"
```

Expected: exit `1`. Output names `migrations/099_DELETE_ME_test_lint.sql`.

- [ ] **Step 3: Add the annotation, verify the lint passes**

```bash
cat > migrations/099_DELETE_ME_test_lint.sql <<'SQL'
-- Test fixture: should pass migration-lint with annotation
-- migration-contract: temporary test fixture; remove before merging.
ALTER TABLE foo DROP COLUMN bar;
SQL
make migration-lint ; echo "exit=$?"
```

Expected: exit `0`.

- [ ] **Step 4: Delete the test fixture**

```bash
rm migrations/099_DELETE_ME_test_lint.sql
make migration-lint
```

Expected: exit `0` again, no findings.

- [ ] **Step 5: Do NOT commit anything from this task**

This task verifies behavior; the fixture is throwaway. `git status` should be clean.

```bash
git status
```

Expected: working tree clean.

---

## Chunk 5: Documentation, template, version bump

The lint by itself is mechanical; it only delivers value if authors know what it's enforcing and how to comply. Three docs deliverables: a short section in `CLAUDE.md` for the rule, a long-form `docs/MIGRATIONS.md` with worked examples, and a `migrations/TEMPLATE.sql` scaffold that authors copy.

### Task 11: New section in `CLAUDE.md`

**Files:**
- Modify: `CLAUDE.md`

**Context:**
`CLAUDE.md` is the project-level guidance loaded into every Claude session. The expand/contract rule needs a short summary here (long-form details live in `docs/MIGRATIONS.md`). Insert near the existing "Versioning" section since it's also a release-discipline rule.

- [ ] **Step 1: Identify insertion point**

Read `CLAUDE.md` and find the `## Versioning` section. The new section sits just above it.

- [ ] **Step 2: Add the section**

Insert into `CLAUDE.md`:

```markdown
## Database Migrations (expand/contract discipline)

Rolling updates require v(N) and v(N+1) of the control plane to run side-by-side
against the same database during a deploy. Schema changes that work for one
version but break the other cause silent corruption or hard errors at the worst
moment. Every schema change must be split into three releases minimum:

1. **Expand**: ADD columns/tables/indexes only. Old code keeps working.
2. **Backfill + dual-write**: app writes to old + new shape; backfill populates new shape.
3. **Contract**: a later release (after every region is on the dual-write code) drops the old shape.

A "rename column" becomes 3 releases minimum.

**Enforced by `make ci`**: `cmd/migration-lint/` scans `migrations/*.sql` for
destructive operations (`DROP COLUMN`, `DROP TABLE`, `DROP INDEX`,
`RENAME COLUMN`, `RENAME TABLE`, `ALTER COLUMN ... TYPE`,
`ALTER COLUMN ... SET NOT NULL`, `TRUNCATE`) and fails the build unless the
migration includes a `-- migration-contract: <reason>` annotation. CI runs in
diff mode against `$BASE_REF` (set in the GitHub Actions workflow) so only
PR-changed files get linted; local `make migration-lint` lints everything.

**When to add the `-- migration-contract:` annotation**: when the destructive
operation is a legitimate contract phase of a multi-release sequence and the
release that wrote dual-format is already live in every region. The annotation
must name the original expand release and explain why it's safe now.

**See:** `docs/MIGRATIONS.md` for worked examples (add column, rename column,
drop column, narrow type). Use `migrations/TEMPLATE.sql` as the starting point
for any new migration.
```

- [ ] **Step 3: Verify it reads correctly in context**

```bash
grep -A 25 "## Database Migrations" CLAUDE.md
```

- [ ] **Step 4: Commit**

```bash
git add CLAUDE.md
git commit -m "CLAUDE.md: document expand/contract migration discipline"
```

---

### Task 12: Long-form `docs/MIGRATIONS.md`

**Files:**
- Create: `docs/MIGRATIONS.md`

**Context:**
The long-form doc carries worked examples that the CLAUDE.md summary references. Cover four common scenarios end-to-end: add column, rename column (3 releases), drop column (2 releases), narrow type (3 releases).

- [ ] **Step 1: Write the document**

`docs/MIGRATIONS.md`:

````markdown
# Database Migrations

This project uses **expand/contract** migration discipline so v(N) and v(N+1)
of the control plane can run side-by-side during a rolling update without one
of them tripping over the other's schema assumptions. The rule is enforced by
`cmd/migration-lint/` in `make ci`.

## The rule

Every schema change goes through up to three releases:

1. **Expand**: ADD columns / tables / indexes only. Old code keeps working.
2. **Dual-write + backfill**: app writes both old and new shape; backfill
   populates new shape from existing rows.
3. **Contract**: a later release (after every region is on the dual-write
   code) drops the old shape. Requires `-- migration-contract: <reason>` in
   the SQL file.

For pure expand changes (most additive work), one release is enough; the
discipline only forces extra ceremony for destructive operations.

## Operations the lint flags

| Operation | Why it's destructive |
|---|---|
| `DROP COLUMN` | Old code reading the column gets a runtime error |
| `DROP TABLE` | Old code reading the table gets a runtime error |
| `DROP INDEX` | Performance regression for old code; sometimes deliberate, hence annotation |
| `RENAME COLUMN` | Old code can't find the new name |
| `RENAME TABLE` | Old code can't find the new name |
| `ALTER COLUMN ... TYPE` | Type-narrowing breaks old writes; widening is usually safe but flagged for review |
| `ALTER COLUMN ... SET NOT NULL` | Old code inserting `NULL` fails |
| `TRUNCATE` | Data loss is always destructive |

If you genuinely need one of these and the multi-release sequence is correctly
sequenced, add an annotation:

```sql
-- migration-contract: column "foo" was deprecated in v1.41 (expand release added
-- "bar"); v1.42 wrote both columns; this release drops "foo" because every
-- region is now confirmed on v1.42+ per deploy_history.
ALTER TABLE example DROP COLUMN foo;
```

The annotation must name the original expand release and explain why dropping
now is safe.

## Worked examples

### Example 1: Add a column (one release)

Pure expand. No discipline overhead.

```sql
-- 042_add_listener_country.sql
ALTER TABLE listener_events ADD COLUMN country_code text;
CREATE INDEX idx_listener_events_country
    ON listener_events(country_code)
    WHERE country_code IS NOT NULL;
```

App code starts populating `country_code` in the same release. Old rows have
`NULL`; readers handle that.

### Example 2: Rename a column (three releases)

Renaming `listener_events.country` → `listener_events.country_code`.

**Release N (expand):**

```sql
-- 050_add_country_code_column.sql
ALTER TABLE listener_events ADD COLUMN country_code text;
-- copy existing values asynchronously via the next release; do not backfill
-- in this migration because CONCURRENT large UPDATEs are themselves disruptive.
```

App code: still reads/writes `country`.

**Release N+1 (dual-write + backfill):**

App code: writes to both `country` and `country_code`; reads prefer
`country_code` and fall back to `country`. A background backfill job populates
`country_code` from `country` for old rows.

No migration file needed; this is pure app-layer work.

**Release N+2 (contract):**

```sql
-- 060_drop_legacy_country_column.sql
-- migration-contract: `country` was the legacy column; `country_code` was added
-- in v1.41 (file 050) and v1.42 wrote both. Every region is now on v1.43+ per
-- deploy_history; dropping the legacy column is safe.
ALTER TABLE listener_events DROP COLUMN country;
```

### Example 3: Narrow a column type (three releases)

Narrowing `media_items.duration_ms` from `bigint` → `integer` (because
durations never exceed 2.1 billion ms and the int saves storage).

**Release N (expand):**

```sql
-- 070_add_duration_ms_int.sql
ALTER TABLE media_items ADD COLUMN duration_ms_int integer;
```

**Release N+1 (dual-write + backfill):**

App writes both columns; backfill copies `duration_ms::integer` into
`duration_ms_int` for old rows.

**Release N+2 (contract):**

```sql
-- 080_swap_to_int_duration.sql
-- migration-contract: duration_ms was bigint, narrowed to integer because
-- durations never exceed 2.1B ms. duration_ms_int has been populated since
-- v1.41 (file 070); every region is on v1.43+ per deploy_history.
ALTER TABLE media_items DROP COLUMN duration_ms;
ALTER TABLE media_items RENAME COLUMN duration_ms_int TO duration_ms;
```

### Example 4: Add a NOT NULL constraint (three releases)

The simplest "destructive" case is widening a column constraint.

**Release N (expand):**

```sql
-- 090_add_station_active_default.sql
ALTER TABLE stations ADD COLUMN active boolean DEFAULT true;
UPDATE stations SET active = true WHERE active IS NULL;
```

**Release N+1 (dual-write):** app code always writes `active`; backfill ran in
release N.

**Release N+2 (contract):**

```sql
-- 100_station_active_not_null.sql
-- migration-contract: `active` was added in v1.41 (file 090) with default
-- true and all existing rows backfilled. v1.42 enforced non-NULL writes in
-- app code. Every region is on v1.43+; the constraint is safe.
ALTER TABLE stations ALTER COLUMN active SET NOT NULL;
```

## Tooling

- New migration: copy `migrations/TEMPLATE.sql` and rename to the next
  sequential number. Fill in the phase comment block at the top so reviewers
  can see at a glance whether this is expand, dual-write, or contract.
- Run lint locally: `make migration-lint`.
- CI runs `make migration-lint-ci` which lints only PR-changed files.

## When the discipline does NOT apply

Single-instance deployments without HA (the always-supported fallback shape)
don't need expand/contract because there's only one instance — a brief
downtime during a destructive migration is acceptable. However, the lint
applies uniformly because the codebase ships one migration set for all
deployment shapes. If a single-instance-only deployment wants to take a
destructive shortcut, the annotation `-- migration-contract: single-instance
deployment, no rolling update applies` is acceptable and self-documenting.

## Edge cases

- **`CREATE INDEX CONCURRENTLY`**: safe; pure expand.
- **`DROP INDEX CONCURRENTLY IF EXISTS`** followed by a recreate in the same
  file: still flagged. Annotate with the reason
  ("rebuilding the index with new sort order; CONCURRENTLY so no listener
  impact; readers tolerate the brief window of no index").
- **Renaming a constraint**: not currently flagged because constraints don't
  directly break old code. Audit case-by-case.
- **`pg_repack` or similar online-migration tools**: out of scope; if used,
  add an annotation explaining why the apparent destructiveness is illusory.
````

- [ ] **Step 2: Commit**

```bash
git add docs/MIGRATIONS.md
git commit -m "docs: long-form expand/contract migration guide with worked examples"
```

---

### Task 13: Migration scaffold file

**Files:**
- Create: `migrations/TEMPLATE.sql`

**Context:**
A template authors copy when starting a new migration. The phase-comment block at the top prompts thinking about expand/contract before any SQL gets written.

- [ ] **Step 1: Write the template**

`migrations/TEMPLATE.sql`:

```sql
-- Migration NNN: <short description>
-- Created: YYYY-MM-DD
-- Phase: [expand | dual-write | contract]
--
-- Description: <what this migration changes and why>
--
-- For destructive operations (DROP, RENAME, ALTER TYPE, SET NOT NULL, TRUNCATE):
-- this migration MUST include the following annotation explaining why dropping
-- this shape now is safe (referencing the expand release that added the new shape,
-- the dual-write release that populated it, and confirming every region is on
-- a version that doesn't read the old shape):
--
-- -- migration-contract: <reason>
--
-- See docs/MIGRATIONS.md for worked examples.

-- ============================================================================
-- SQL BELOW
-- ============================================================================
```

- [ ] **Step 2: Verify the lint accepts the template as-is**

The template is empty of SQL so it should pass the lint trivially.

```bash
cp migrations/TEMPLATE.sql migrations/099_TEMPLATE_TEST.sql
make migration-lint ; echo "exit=$?"
rm migrations/099_TEMPLATE_TEST.sql
```

Expected: exit `0` (no SQL, no destructive ops, no findings).

- [ ] **Step 3: Commit**

```bash
git add migrations/TEMPLATE.sql
git commit -m "migrations: scaffold template for new migrations"
```

---

### Task 14: Final CI gate + version bump + push

**Files:**
- Modify: `internal/version/version.go`

**Context:**
Per memory rule, every push to GitHub gets a patch version bump. This work is non-functional from the listener's perspective but is real engineering scaffolding for the HA rollout. Current version: check `internal/version/version.go`.

- [ ] **Step 1: Run the full CI gate one more time**

```bash
make ci
```

Expected: all green, including the new `migration-lint-ci` step.

- [ ] **Step 2: Read current version**

```bash
grep -n 'Version' internal/version/version.go
```

- [ ] **Step 3: Bump patch version**

Edit `internal/version/version.go`, incrementing the patch component (e.g., `1.40.8` → `1.40.9`).

- [ ] **Step 4: Final commit + tag + push**

```bash
NEW_VER=$(grep -oP '"\K[0-9]+\.[0-9]+\.[0-9]+' internal/version/version.go)
git add internal/version/version.go
git commit -m "Expand/contract migration discipline + CI lint (v${NEW_VER}, closes #234)"
git tag -a "v${NEW_VER}" -m "Version ${NEW_VER}"
git push origin main
git push origin "v${NEW_VER}"
```

- [ ] **Step 5: Close the issue**

```bash
gh issue close 234 --repo friendsincode/grimnir_radio \
  --comment "Shipped in v${NEW_VER}. cmd/migration-lint/ now runs in make ci (diff mode in PR builds, full-dir mode locally). CLAUDE.md updated, docs/MIGRATIONS.md added with worked examples, migrations/TEMPLATE.sql scaffold in place."
```

---

## Implementation notes

- **Why line-based regex over a SQL parser?** Postgres SQL is non-trivial; a full parser would be either a heavy dependency or many weeks of work. The patterns we flag (`DROP COLUMN`, etc.) are reliably detectable with regex against `--`-comment-stripped lines. Conservative false-positives are acceptable (you add an annotation explaining why) and far cheaper than a parser dependency. If we hit real false-positive pain we can revisit; not before.
- **Why per-line iteration over the whole file?** Multi-statement SQL files are common; line-by-line means each finding gets a useful line number, and a destructive op buried in a 200-line migration still gets caught.
- **Why a package-variable `gitDiffNames` for the git call?** Standard Go testability pattern when shelling out. Cleaner than wiring an `io/fs`-shaped abstraction; the only operation we mock is "run git diff and parse the output."
- **Why exit codes 0/1/2?** Standard Unix convention: 0 success, 1 lint failures, 2 internal error. `make ci` cares only that the exit is nonzero on either failure case.
- **Why no `golangci-lint` integration?** The existing project skips lint gracefully if `golangci-lint` isn't installed; we follow the same pattern (lint is its own binary, run unconditionally). Avoids one more tool to install on CI hosts.

## Out of scope (filed as future work if needed)

- GORM struct-field deletion detection (per Q-PB1 = A). Not in this issue.
- Multi-statement transactional safety (e.g., flagging migrations that aren't wrapped in a transaction). Different concern.
- Cross-migration consistency checks (e.g., "migration 070 ADDs `foo`; does any later migration drop it without annotation?"). Useful but a different tool.
- Automatic migration generation from GORM models. Out of scope.

## Acceptance criteria for this plan as a whole

- `cmd/migration-lint/` exists; `go test ./cmd/migration-lint/` is green.
- `make migration-lint` returns exit 0 against the current `migrations/` directory.
- `make ci` includes the migration-lint step and passes.
- A deliberate destructive migration without annotation causes `make ci` to fail (Task 10 verification).
- A deliberate destructive migration WITH annotation passes (Task 10 verification).
- `CLAUDE.md` has the new section.
- `docs/MIGRATIONS.md` exists with all four worked examples.
- `migrations/TEMPLATE.sql` exists.
- Issue #234 closed with the version reference.

