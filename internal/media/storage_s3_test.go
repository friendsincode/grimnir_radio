/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/

package media

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/rs/zerolog"
)

// fakeS3Server creates a minimal HTTP server that mimics an S3/MinIO endpoint.
// It handles the common S3 API calls used by the SDK.
func fakeS3Server(t *testing.T) *httptest.Server {
	t.Helper()

	mux := http.NewServeMux()

	// HeadBucket – bucket exists
	mux.HandleFunc("/test-bucket", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodHead {
			w.WriteHeader(http.StatusOK)
			return
		}
		// ListObjectsV2
		if r.Method == http.MethodGet {
			w.Header().Set("Content-Type", "application/xml")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`<?xml version="1.0" encoding="UTF-8"?>
<ListBucketResult xmlns="http://s3.amazonaws.com/doc/2006-03-01/">
  <Name>test-bucket</Name>
  <Prefix></Prefix>
  <MaxKeys>1000</MaxKeys>
  <IsTruncated>false</IsTruncated>
  <Contents>
    <Key>media/station1/file1.mp3</Key>
    <Size>100</Size>
  </Contents>
</ListBucketResult>`))
			return
		}
		w.WriteHeader(http.StatusMethodNotAllowed)
	})

	// HeadObject, PutObject, DeleteObject, CopyObject for any key
	mux.HandleFunc("/test-bucket/", func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodHead:
			w.Header().Set("x-amz-meta-station-id", "station1")
			w.WriteHeader(http.StatusOK)
		case http.MethodPut:
			// CopyObject uses x-amz-copy-source header
			if r.Header.Get("x-amz-copy-source") != "" {
				w.Header().Set("Content-Type", "application/xml")
				w.WriteHeader(http.StatusOK)
				_, _ = w.Write([]byte(`<?xml version="1.0" encoding="UTF-8"?>
<CopyObjectResult xmlns="http://s3.amazonaws.com/doc/2006-03-01/">
  <LastModified>2026-01-01T00:00:00.000Z</LastModified>
  <ETag>"abc123"</ETag>
</CopyObjectResult>`))
				return
			}
			w.WriteHeader(http.StatusOK)
		case http.MethodDelete:
			w.WriteHeader(http.StatusNoContent)
		default:
			w.WriteHeader(http.StatusMethodNotAllowed)
		}
	})

	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

// newFakeS3Storage creates an S3Storage backed by the fake HTTP server.
func newFakeS3Storage(t *testing.T, srv *httptest.Server) *S3Storage {
	t.Helper()
	ctx := context.Background()
	cfg := S3Config{
		AccessKeyID:     "fakekey",
		SecretAccessKey: "fakesecret",
		Region:          "us-east-1",
		Bucket:          "test-bucket",
		Endpoint:        srv.URL,
		UsePathStyle:    true,
		ForcePathStyle:  true,
		PresignedExpiry: 15 * time.Minute,
	}
	s3s, err := NewS3Storage(ctx, cfg, zerolog.Nop())
	if err != nil {
		t.Fatalf("NewS3Storage() error: %v", err)
	}
	return s3s
}

// ── NewS3Storage ──────────────────────────────────────────────────────────────

func TestNewS3Storage_WithFakeEndpoint(t *testing.T) {
	srv := fakeS3Server(t)
	s3s := newFakeS3Storage(t, srv)
	if s3s == nil {
		t.Fatal("NewS3Storage() returned nil")
	}
	if s3s.bucket != "test-bucket" {
		t.Errorf("bucket = %q, want %q", s3s.bucket, "test-bucket")
	}
}

func TestNewS3Storage_WithoutEndpoint(t *testing.T) {
	// No endpoint — standard AWS path. Just test that it constructs without error
	// using fake credentials (won't try to contact S3 during creation if bucket
	// is empty, but HeadBucket will fail and we log the warning).
	ctx := context.Background()
	cfg := S3Config{
		AccessKeyID:     "fakekey",
		SecretAccessKey: "fakesecret",
		Region:          "us-east-1",
		Bucket:          "", // empty bucket → HeadBucket skipped
	}
	s3s, err := NewS3Storage(ctx, cfg, zerolog.Nop())
	if err != nil {
		t.Fatalf("NewS3Storage() error: %v", err)
	}
	if s3s == nil {
		t.Fatal("NewS3Storage() returned nil")
	}
}

// ── S3Storage.CheckAccess ─────────────────────────────────────────────────────

func TestS3Storage_CheckAccess_Success(t *testing.T) {
	srv := fakeS3Server(t)
	s3s := newFakeS3Storage(t, srv)
	ctx := context.Background()

	if err := s3s.CheckAccess(ctx); err != nil {
		t.Errorf("CheckAccess() unexpected error: %v", err)
	}
}

// ── S3Storage.Store ───────────────────────────────────────────────────────────

func TestS3Storage_Store_Success(t *testing.T) {
	srv := fakeS3Server(t)
	s3s := newFakeS3Storage(t, srv)
	ctx := context.Background()

	key, err := s3s.Store(ctx, "station1", "mediaID123", bytes.NewReader([]byte("audio data")))
	if err != nil {
		t.Fatalf("Store() error: %v", err)
	}
	if key == "" {
		t.Error("Store() returned empty key")
	}
	// Key should be in the expected format.
	if !strings.Contains(key, "station1") {
		t.Errorf("Store() key = %q, should contain station1", key)
	}
}

// ── S3Storage.Delete ──────────────────────────────────────────────────────────

func TestS3Storage_Delete_Success(t *testing.T) {
	srv := fakeS3Server(t)
	s3s := newFakeS3Storage(t, srv)
	ctx := context.Background()

	if err := s3s.Delete(ctx, "media/station1/mediaID123"); err != nil {
		t.Fatalf("Delete() error: %v", err)
	}
}

// ── S3Storage.Exists ──────────────────────────────────────────────────────────

func TestS3Storage_Exists_True(t *testing.T) {
	srv := fakeS3Server(t)
	s3s := newFakeS3Storage(t, srv)
	ctx := context.Background()

	exists, err := s3s.Exists(ctx, "media/station1/existing.mp3")
	if err != nil {
		t.Fatalf("Exists() error: %v", err)
	}
	if !exists {
		t.Error("Exists() = false, want true (HeadObject returns 200)")
	}
}

func TestS3Storage_Exists_False(t *testing.T) {
	// Build a server that returns 404 for HeadObject.
	mux := http.NewServeMux()
	mux.HandleFunc("/test-bucket", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodHead {
			w.WriteHeader(http.StatusOK)
		}
	})
	mux.HandleFunc("/test-bucket/", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodHead {
			w.WriteHeader(http.StatusNotFound)
		}
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	ctx := context.Background()
	cfg := S3Config{
		AccessKeyID:     "k",
		SecretAccessKey: "s",
		Region:          "us-east-1",
		Bucket:          "test-bucket",
		Endpoint:        srv.URL,
		UsePathStyle:    true,
		ForcePathStyle:  true,
	}
	s3s, err := NewS3Storage(ctx, cfg, zerolog.Nop())
	if err != nil {
		t.Fatalf("NewS3Storage() error: %v", err)
	}

	exists, err := s3s.Exists(ctx, "media/station1/missing.mp3")
	if err != nil {
		t.Fatalf("Exists() error: %v", err)
	}
	if exists {
		t.Error("Exists() = true, want false (HeadObject returns 404)")
	}
}

// ── S3Storage.GetMetadata ─────────────────────────────────────────────────────

func TestS3Storage_GetMetadata_Success(t *testing.T) {
	srv := fakeS3Server(t)
	s3s := newFakeS3Storage(t, srv)
	ctx := context.Background()

	meta, err := s3s.GetMetadata(ctx, "media/station1/track.mp3")
	if err != nil {
		t.Fatalf("GetMetadata() error: %v", err)
	}
	// meta may be empty or populated depending on headers — just check no error.
	_ = meta
}

// ── S3Storage.Copy ────────────────────────────────────────────────────────────

func TestS3Storage_Copy_Success(t *testing.T) {
	srv := fakeS3Server(t)
	s3s := newFakeS3Storage(t, srv)
	ctx := context.Background()

	if err := s3s.Copy(ctx, "media/src/file.mp3", "media/dst/file.mp3"); err != nil {
		t.Fatalf("Copy() error: %v", err)
	}
}

// ── S3Storage.ListObjects ─────────────────────────────────────────────────────

func TestS3Storage_ListObjects_Success(t *testing.T) {
	srv := fakeS3Server(t)
	s3s := newFakeS3Storage(t, srv)
	ctx := context.Background()

	keys, err := s3s.ListObjects(ctx, "media/station1/", 100)
	if err != nil {
		t.Fatalf("ListObjects() error: %v", err)
	}
	// The fake server returns one object key.
	if len(keys) == 0 {
		t.Error("ListObjects() returned empty list, expected at least one key from fake server")
	}
}

func TestS3Storage_ListObjects_DefaultMaxKeys(t *testing.T) {
	srv := fakeS3Server(t)
	s3s := newFakeS3Storage(t, srv)
	ctx := context.Background()

	// maxKeys=0 should default to 1000 internally.
	keys, err := s3s.ListObjects(ctx, "media/station1/", 0)
	if err != nil {
		t.Fatalf("ListObjects() error: %v", err)
	}
	_ = keys
}

// ── S3Storage.PresignedURL ────────────────────────────────────────────────────

func TestS3Storage_PresignedURL_ReturnsURL(t *testing.T) {
	srv := fakeS3Server(t)
	s3s := newFakeS3Storage(t, srv)
	ctx := context.Background()

	url, err := s3s.PresignedURL(ctx, "media/station1/track.mp3", 15*time.Minute)
	if err != nil {
		t.Fatalf("PresignedURL() error: %v", err)
	}
	if url == "" {
		t.Error("PresignedURL() returned empty URL")
	}
}
