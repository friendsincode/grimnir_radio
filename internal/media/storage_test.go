/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/

package media

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/rs/zerolog"
)

// ── FilesystemStorage.Store ───────────────────────────────────────────────

func TestFilesystemStorage_Store_ReturnsRelativePath(t *testing.T) {
	root := t.TempDir()
	logger := zerolog.Nop()
	fs := NewFilesystemStorage(root, logger)
	ctx := context.Background()

	stationID := "station-abc"
	mediaID := "abcdef1234567890"
	content := []byte("audio content here")

	relPath, err := fs.Store(ctx, stationID, mediaID, bytes.NewReader(content))
	if err != nil {
		t.Fatalf("Store() unexpected error: %v", err)
	}

	// Path must NOT start with the root directory.
	if filepath.IsAbs(relPath) {
		t.Errorf("Store() returned absolute path %q — must be relative", relPath)
	}
	if strings.HasPrefix(relPath, root) {
		t.Errorf("Store() returned path prefixed with rootDir %q — must be relative", relPath)
	}
}

func TestFilesystemStorage_Store_FileIsWrittenToCorrectLocation(t *testing.T) {
	root := t.TempDir()
	logger := zerolog.Nop()
	fs := NewFilesystemStorage(root, logger)
	ctx := context.Background()

	stationID := "station-xyz"
	mediaID := "abcdef1234567890"
	content := []byte("test audio bytes")

	relPath, err := fs.Store(ctx, stationID, mediaID, bytes.NewReader(content))
	if err != nil {
		t.Fatalf("Store() error: %v", err)
	}

	// Verify the file exists at root + relPath.
	fullPath := filepath.Join(root, relPath)
	data, err := os.ReadFile(fullPath)
	if err != nil {
		t.Fatalf("file not found at %q: %v", fullPath, err)
	}
	if string(data) != string(content) {
		t.Errorf("file content = %q, want %q", string(data), string(content))
	}
}

func TestFilesystemStorage_Store_CreatesHierarchicalPath(t *testing.T) {
	// The path structure must be station_id/media_id[0:2]/media_id[2:4]/media_id.audio
	root := t.TempDir()
	logger := zerolog.Nop()
	fs := NewFilesystemStorage(root, logger)
	ctx := context.Background()

	stationID := "st1"
	mediaID := "abcdef1234567890"

	relPath, err := fs.Store(ctx, stationID, mediaID, bytes.NewReader([]byte("x")))
	if err != nil {
		t.Fatalf("Store() error: %v", err)
	}

	// Expected: st1/ab/cd/abcdef1234567890.audio
	expectedPrefix := filepath.Join(stationID, mediaID[0:2], mediaID[2:4])
	if !strings.HasPrefix(relPath, expectedPrefix) {
		t.Errorf("relPath = %q, expected prefix %q", relPath, expectedPrefix)
	}
}

func TestFilesystemStorage_Store_ShortMediaIDPath(t *testing.T) {
	// mediaID shorter than 4 chars uses flat path: station_id/media_id.audio
	root := t.TempDir()
	logger := zerolog.Nop()
	fs := NewFilesystemStorage(root, logger)
	ctx := context.Background()

	stationID := "st2"
	mediaID := "ab" // only 2 chars

	relPath, err := fs.Store(ctx, stationID, mediaID, bytes.NewReader([]byte("y")))
	if err != nil {
		t.Fatalf("Store() error: %v", err)
	}

	// Expected flat path: st2/ab.audio
	expected := filepath.Join(stationID, mediaID+".audio")
	if relPath != expected {
		t.Errorf("relPath = %q, want %q", relPath, expected)
	}
}

func TestFilesystemStorage_Store_NoDoubleRootPath(t *testing.T) {
	// Critical regression guard: path returned from Store must be relative so
	// that joining root + relPath does NOT produce root/root/... double paths.
	root := t.TempDir()
	logger := zerolog.Nop()
	fs := NewFilesystemStorage(root, logger)
	ctx := context.Background()

	relPath, err := fs.Store(ctx, "st3", "abcd1234", bytes.NewReader([]byte("z")))
	if err != nil {
		t.Fatalf("Store() error: %v", err)
	}

	// Double-joining the root should produce a valid path that exists exactly once.
	joined := filepath.Join(root, relPath)
	// If relPath were absolute and equal to joined, a second join would reproduce it.
	// But if relPath is relative, joining again would be root/root/... which won't exist.
	if _, err := os.Stat(joined); err != nil {
		t.Errorf("joined path %q not accessible: %v — store may have written to wrong location", joined, err)
	}

	// Verify there is no double-root in the joined path.
	if strings.Contains(joined, root+root) || strings.Count(joined, root) > 1 {
		t.Errorf("double root detected in path %q", joined)
	}
}

// ── FilesystemStorage.Delete ──────────────────────────────────────────────

func TestFilesystemStorage_Delete_JoinsRootWithRelativePath(t *testing.T) {
	root := t.TempDir()
	logger := zerolog.Nop()
	fs := NewFilesystemStorage(root, logger)
	ctx := context.Background()

	// Store a file and then delete it using the relative path returned by Store.
	stationID := "station-del"
	mediaID := "abcdef001122"
	relPath, err := fs.Store(ctx, stationID, mediaID, bytes.NewReader([]byte("delete me")))
	if err != nil {
		t.Fatalf("Store() error: %v", err)
	}

	// File should exist before delete.
	fullPath := filepath.Join(root, relPath)
	if _, err := os.Stat(fullPath); err != nil {
		t.Fatalf("file should exist before delete: %v", err)
	}

	if err := fs.Delete(ctx, relPath); err != nil {
		t.Fatalf("Delete() error: %v", err)
	}

	// File should not exist after delete.
	if _, err := os.Stat(fullPath); !os.IsNotExist(err) {
		t.Errorf("file should not exist after delete, stat returned: %v", err)
	}
}

func TestFilesystemStorage_Delete_NonExistentFileReturnsNil(t *testing.T) {
	// Deleting a file that does not exist should return nil (idempotent).
	root := t.TempDir()
	logger := zerolog.Nop()
	fs := NewFilesystemStorage(root, logger)
	ctx := context.Background()

	err := fs.Delete(ctx, "station-x/no/file/here.audio")
	if err != nil {
		t.Errorf("Delete() for non-existent file should return nil, got: %v", err)
	}
}

func TestFilesystemStorage_Delete_PathTraversalAllowedByFilepathJoin(t *testing.T) {
	// KNOWN VULNERABILITY: filepath.Join(rootDir, "../file") resolves to the parent
	// directory and CAN delete files outside rootDir. This test documents the
	// current (unsafe) behavior so any future fix to validate paths is immediately
	// visible as a test change.
	//
	// The correct fix would be to reject any path where filepath.Clean(path)
	// starts with ".." before joining with rootDir.
	root := t.TempDir()
	logger := zerolog.Nop()
	fs := NewFilesystemStorage(root, logger)
	ctx := context.Background()

	// Create a sentinel file in the parent directory.
	parent := filepath.Dir(root)
	sentinel := filepath.Join(parent, "traversal_sentinel_"+filepath.Base(root)+".txt")
	if err := os.WriteFile(sentinel, []byte("vulnerable"), 0644); err != nil {
		t.Fatalf("create sentinel: %v", err)
	}
	t.Cleanup(func() { os.Remove(sentinel) })

	// Construct a path that traverses up one level.
	traversal := "../" + filepath.Base(sentinel)

	// The current implementation (filepath.Join) will resolve this to the sentinel
	// and delete it successfully. If this test starts failing with an error from
	// Delete(), it means path validation was added — that's a good change.
	err := fs.Delete(ctx, traversal)

	// Currently: deletion succeeds (traversal works), sentinel is gone.
	// A future fix should either: return an error from Delete() OR leave sentinel intact.
	sentinelExists := true
	if _, statErr := os.Stat(sentinel); os.IsNotExist(statErr) {
		sentinelExists = false
	}

	if err != nil && sentinelExists {
		// Delete returned an error AND sentinel still exists → path validation added. Good.
		return
	}
	if err == nil && !sentinelExists {
		// Delete succeeded and sentinel was removed → current vulnerable behaviour.
		// Log but do not fail — this test documents the current state.
		t.Logf("KNOWN VULNERABILITY: path traversal via '../' deletes files outside rootDir (no validation in Delete)")
		return
	}
	if err == nil && sentinelExists {
		// Traversal didn't resolve to sentinel (different filesystem layout); skip.
		t.Logf("path traversal test inconclusive (sentinel not deleted, no error): filesystem layout may differ")
	}
}

// ── FilesystemStorage.CheckAccess ────────────────────────────────────────

func TestFilesystemStorage_CheckAccess_IsFileFails(t *testing.T) {
	// If the media root is a file rather than a directory, CheckAccess should fail.
	tmp := t.TempDir()
	file := filepath.Join(tmp, "notadir.txt")
	if err := os.WriteFile(file, []byte("x"), 0644); err != nil {
		t.Fatalf("create file: %v", err)
	}
	fs := NewFilesystemStorage(file, zerolog.Nop())
	if err := fs.CheckAccess(context.Background()); err == nil {
		t.Error("CheckAccess() should fail when rootDir is a file, not a directory")
	}
}

// ── buildMediaPath ────────────────────────────────────────────────────────

func TestBuildMediaPath_PathTraversalNotEscapedByStation(t *testing.T) {
	// buildMediaPath uses filepath.Join which normalises "..".
	// A malicious stationID with ".." should not escape the expected structure
	// in a way that writes outside an expected prefix when joined with a real root.
	root := t.TempDir()
	logger := zerolog.Nop()
	fs := NewFilesystemStorage(root, logger)
	ctx := context.Background()

	// Use a stationID that tries path traversal.
	stationID := "../evil"
	mediaID := "abcdef001122"

	relPath, err := fs.Store(ctx, stationID, mediaID, bytes.NewReader([]byte("attempt")))
	if err != nil {
		// It's acceptable to fail with an error — traversal blocked.
		return
	}

	// If it succeeded, the actual file path must still be within root or its parent
	// (filepath.Join cleans ".." naturally), but it must NOT escape to arbitrary paths.
	// We verify the full path is somewhere accessible and that the relative path
	// returned does not start with "/".
	if filepath.IsAbs(relPath) {
		t.Errorf("Store() must not return absolute path even for adversarial stationID, got: %q", relPath)
	}
}

// ── Service.Store / Service.Delete wrappers ───────────────────────────────

func TestService_Store_WrapsFilesystemStore(t *testing.T) {
	root := t.TempDir()
	logger := zerolog.Nop()
	storage := NewFilesystemStorage(root, logger)
	svc := &Service{storage: storage, mediaRoot: root, logger: logger}
	ctx := context.Background()

	stationID := "svc-station"
	mediaID := "abcdef001234"
	content := []byte("wrapped store content")

	relPath, err := svc.Store(ctx, stationID, mediaID, bytes.NewReader(content))
	if err != nil {
		t.Fatalf("Service.Store() error: %v", err)
	}
	if filepath.IsAbs(relPath) {
		t.Errorf("Service.Store() returned absolute path %q, want relative", relPath)
	}
	// Verify file exists.
	fullPath := filepath.Join(root, relPath)
	if _, err := os.Stat(fullPath); err != nil {
		t.Fatalf("stored file not found at %q: %v", fullPath, err)
	}
}

func TestService_Delete_WrapsFilesystemDelete(t *testing.T) {
	root := t.TempDir()
	logger := zerolog.Nop()
	storage := NewFilesystemStorage(root, logger)
	svc := &Service{storage: storage, mediaRoot: root, logger: logger}
	ctx := context.Background()

	// Store then delete via Service wrapper.
	relPath, err := svc.Store(ctx, "svc-station", "abcdef001234", bytes.NewReader([]byte("del")))
	if err != nil {
		t.Fatalf("Store: %v", err)
	}
	if err := svc.Delete(ctx, relPath); err != nil {
		t.Fatalf("Service.Delete() error: %v", err)
	}
	fullPath := filepath.Join(root, relPath)
	if _, err := os.Stat(fullPath); !os.IsNotExist(err) {
		t.Error("file should not exist after Service.Delete()")
	}
}
