/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/

package edgeencoder

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/fsnotify/fsnotify"
)

// S3PutClient is the minimal interface HLSUploader needs from an S3 backend.
// Implemented by an adapter around internal/media's S3Storage in production &
// by mocks in tests. Implementations must be safe for concurrent calls.
type S3PutClient interface {
	PutObject(ctx context.Context, bucket, key string, body io.Reader) error
}

// HLSUploader watches a local segment directory for new .ts segments &
// .m3u8 manifest updates, then uploads them to S3 under <bucket>/hls/.
//
// Errors during upload are counted but never stop the watch loop; a
// transient S3 failure shouldn't kill the encoder.
type HLSUploader struct {
	dir       string
	bucket    string
	keyPrefix string // default "hls"; configurable for multi-mount in future
	s3        S3PutClient

	// settle is how long a file must stay quiet before it uploads. GStreamer
	// creates a segment & then writes into it over the following seconds;
	// uploading on the Create event shipped empty/truncated segments to S3,
	// & per-Write re-uploads could finish out of order so a truncated body
	// won. Debouncing uploads the finished file exactly once.
	settle time.Duration

	uploaded atomic.Int64
	failed   atomic.Int64
}

// NewHLSUploader constructs an uploader. The directory must already exist
// (typically created by the GStreamer pipeline before hlssink2 starts).
func NewHLSUploader(dir, bucket string, s3 S3PutClient) (*HLSUploader, error) {
	if _, err := os.Stat(dir); err != nil {
		return nil, fmt.Errorf("hls dir: %w", err)
	}
	if bucket == "" {
		return nil, fmt.Errorf("hls bucket is required")
	}
	if s3 == nil {
		return nil, fmt.Errorf("S3 client is required")
	}
	return &HLSUploader{
		dir:       dir,
		bucket:    bucket,
		keyPrefix: "hls",
		s3:        s3,
		settle:    500 * time.Millisecond,
	}, nil
}

// Run blocks until ctx is cancelled. Watches the segment directory & uploads
// each new/modified .ts and .m3u8 file.
func (u *HLSUploader) Run(ctx context.Context) error {
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		return fmt.Errorf("fsnotify: %w", err)
	}
	defer watcher.Close()
	if err := watcher.Add(u.dir); err != nil {
		return fmt.Errorf("watch %s: %w", u.dir, err)
	}

	// Per-file debounce timers: Create + the stream of Write events coalesce
	// into one upload once the file has been quiet for the settle window.
	var mu sync.Mutex
	timers := make(map[string]*time.Timer)
	defer func() {
		mu.Lock()
		for _, t := range timers {
			t.Stop()
		}
		mu.Unlock()
	}()

	for {
		select {
		case <-ctx.Done():
			return nil
		case event, ok := <-watcher.Events:
			if !ok {
				return nil
			}
			if event.Op&(fsnotify.Create|fsnotify.Write) == 0 {
				continue
			}
			base := filepath.Base(event.Name)
			ext := strings.ToLower(filepath.Ext(base))
			if ext != ".ts" && ext != ".m3u8" {
				continue
			}
			name := event.Name
			mu.Lock()
			if t, exists := timers[name]; exists {
				t.Reset(u.settle)
			} else {
				timers[name] = time.AfterFunc(u.settle, func() {
					mu.Lock()
					delete(timers, name)
					mu.Unlock()
					if ctx.Err() == nil {
						u.uploadFile(ctx, name)
					}
				})
			}
			mu.Unlock()
		case _, ok := <-watcher.Errors:
			if !ok {
				return nil
			}
			u.failed.Add(1)
		}
	}
}

func (u *HLSUploader) uploadFile(ctx context.Context, path string) {
	f, err := os.Open(path)
	if err != nil {
		u.failed.Add(1)
		return
	}
	defer f.Close()
	key := u.keyPrefix + "/" + filepath.Base(path)
	uploadCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	if err := u.s3.PutObject(uploadCtx, u.bucket, key, f); err != nil {
		u.failed.Add(1)
		return
	}
	u.uploaded.Add(1)
}

// UploadedCount returns the total successful uploads.
func (u *HLSUploader) UploadedCount() int64 { return u.uploaded.Load() }

// FailedCount returns the total failed uploads.
func (u *HLSUploader) FailedCount() int64 { return u.failed.Load() }
