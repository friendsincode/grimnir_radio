/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/

package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestScanFile_CatchesUpdateSingleColumn(t *testing.T) {
	src := `package x
func f() {
	db.Model(&y).Update("media_id", "").Error
}
`
	hits := scanFile("/x.go", src)
	if len(hits) != 1 {
		t.Fatalf("want 1 hit, got %d: %+v", len(hits), hits)
	}
	if hits[0].line != 3 {
		t.Errorf("want line 3, got %d", hits[0].line)
	}
	if !strings.Contains(hits[0].pattern, "Update single-column") {
		t.Errorf("want Update single-column pattern, got %q", hits[0].pattern)
	}
}

func TestScanFile_CatchesMapLiteral(t *testing.T) {
	src := `package x
func f() {
	db.Updates(map[string]any{"host_user_id": "", "name": "joe"})
}
`
	hits := scanFile("/x.go", src)
	if len(hits) != 1 {
		t.Fatalf("want 1 hit, got %d: %+v", len(hits), hits)
	}
	if !strings.Contains(hits[0].pattern, "map-literal") {
		t.Errorf("want map-literal pattern, got %q", hits[0].pattern)
	}
}

func TestScanFile_CatchesMapIndexAssignment(t *testing.T) {
	src := `package x
func f() {
	updates["mount_id"] = ""
}
`
	hits := scanFile("/x.go", src)
	if len(hits) != 1 {
		t.Fatalf("want 1 hit, got %d: %+v", len(hits), hits)
	}
}

func TestScanFile_AllowsNilBind(t *testing.T) {
	src := `package x
func f() {
	db.Model(&y).Update("media_id", nil)
	db.Updates(map[string]any{"host_user_id": nil})
	updates["mount_id"] = nil
}
`
	hits := scanFile("/x.go", src)
	if len(hits) != 0 {
		t.Fatalf("nil binds should pass; got %d hits: %+v", len(hits), hits)
	}
}

func TestScanFile_AllowsNonIDColumns(t *testing.T) {
	// "" on non-id columns is fine (text/varchar columns accept it).
	src := `package x
func f() {
	db.Model(&y).Update("name", "")
	db.Updates(map[string]any{"description": "", "color": ""})
}
`
	hits := scanFile("/x.go", src)
	if len(hits) != 0 {
		t.Fatalf("non-id columns should pass; got %d hits: %+v", len(hits), hits)
	}
}

func TestRun_ExitsZeroOnCleanTree(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "ok.go"), []byte(`package x
func f() { db.Update("media_id", nil) }
`), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	var stdout, stderr bytes.Buffer
	code := run([]string{"--root", root}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("want exit 0, got %d. stdout=%s stderr=%s", code, stdout.String(), stderr.String())
	}
}

func TestRun_ExitsNonZeroOnTrap(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "bad.go"), []byte(`package x
func f() { db.Update("media_id", "") }
`), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	var stdout, stderr bytes.Buffer
	code := run([]string{"--root", root}, &stdout, &stderr)
	if code != 1 {
		t.Fatalf("want exit 1, got %d. stdout=%s stderr=%s", code, stdout.String(), stderr.String())
	}
	if !strings.Contains(stdout.String(), "SQLSTATE 22P02") {
		t.Errorf("want explanation in output, got: %s", stdout.String())
	}
}

func TestRun_SkipsTestFiles(t *testing.T) {
	// Test files often have JSON request bodies that look like
	// `"foo_id": ""` inside string literals; never reach GORM.
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "x_test.go"), []byte(`package x
func TestY() {
	body := []byte(`+"`{\"mount_id\":\"\"}`"+`)
	_ = body
}
`), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	var stdout, stderr bytes.Buffer
	code := run([]string{"--root", root}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("test files should be skipped; got exit %d. stdout=%s", code, stdout.String())
	}
}
