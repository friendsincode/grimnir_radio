/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/

package media

import (
	"context"
	"testing"

	"github.com/friendsincode/grimnir_radio/internal/config"
	"github.com/rs/zerolog"
)

func TestNewService(t *testing.T) {
	logger := zerolog.Nop()

	tests := []struct {
		name                string
		cfg                 *config.Config
		expectedStorageType string
		expectError         bool
	}{
		{
			name: "filesystem storage when no S3 bucket configured",
			cfg: &config.Config{
				MediaRoot: "/tmp/media",
				S3Bucket:  "",
			},
			expectedStorageType: "filesystem",
			expectError:         false,
		},
		// Note: S3 storage test would require valid AWS credentials or mocking
		// We test S3Storage separately with proper mocking
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc, err := NewService(tt.cfg, logger)

			if tt.expectError {
				if err == nil {
					t.Fatal("NewService() expected error, got nil")
				}
				return
			}

			if err != nil {
				t.Fatalf("NewService() unexpected error: %v", err)
			}

			if svc == nil {
				t.Fatal("NewService() returned nil service")
			}

			if svc.storage == nil {
				t.Fatal("NewService() storage is nil")
			}

			// Check type assertion
			switch tt.expectedStorageType {
			case "filesystem":
				if _, ok := svc.storage.(*FilesystemStorage); !ok {
					t.Errorf("NewService() storage type = %T, want *FilesystemStorage", svc.storage)
				}
			case "s3":
				if _, ok := svc.storage.(*S3Storage); !ok {
					t.Errorf("NewService() storage type = %T, want *S3Storage", svc.storage)
				}
			}
		})
	}
}

func TestBuildMediaPath(t *testing.T) {
	tests := []struct {
		name      string
		stationID string
		mediaID   string
		extension string
		expected  string
	}{
		{
			name:      "standard path with long mediaID",
			stationID: "station1",
			mediaID:   "abcd1234efgh5678",
			extension: ".mp3",
			expected:  "station1/ab/cd/abcd1234efgh5678.mp3",
		},
		{
			name:      "short mediaID (less than 4 chars)",
			stationID: "station2",
			mediaID:   "abc",
			extension: ".flac",
			expected:  "station2/abc.flac",
		},
		{
			name:      "exactly 4 chars mediaID",
			stationID: "station3",
			mediaID:   "abcd",
			extension: ".wav",
			expected:  "station3/ab/cd/abcd.wav",
		},
		{
			name:      "uppercase mediaID",
			stationID: "station4",
			mediaID:   "ABCD1234",
			extension: ".ogg",
			expected:  "station4/AB/CD/ABCD1234.ogg",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := buildMediaPath(tt.stationID, tt.mediaID, tt.extension)
			if result != tt.expected {
				t.Errorf("buildMediaPath() = %v, want %v", result, tt.expected)
			}
		})
	}
}

func TestFilesystemStorageURL(t *testing.T) {
	logger := zerolog.Nop()

	fs := NewFilesystemStorage("/tmp/media", logger)

	tests := []struct {
		name     string
		path     string
		expected string
	}{
		{
			name:     "returns path unchanged",
			path:     "/tmp/media/station1/ab/cd/file.mp3",
			expected: "/tmp/media/station1/ab/cd/file.mp3",
		},
		{
			name:     "relative path",
			path:     "station1/ab/cd/file.mp3",
			expected: "station1/ab/cd/file.mp3",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			url := fs.URL(tt.path)
			if url != tt.expected {
				t.Errorf("FilesystemStorage.URL() = %v, want %v", url, tt.expected)
			}
		})
	}
}

func TestS3StorageURL(t *testing.T) {
	tests := []struct {
		name          string
		bucket        string
		endpoint      string
		publicBaseURL string
		usePathStyle  bool
		region        string
		path          string
		expected      string
	}{
		{
			name:          "endpoint with path style includes bucket",
			bucket:        "my-bucket",
			endpoint:      "https://minio.example.com",
			publicBaseURL: "",
			usePathStyle:  true,
			path:          "station1/ab/cd/file.mp3",
			expected:      "https://minio.example.com/my-bucket/station1/ab/cd/file.mp3",
		},
		{
			name:          "endpoint without path style omits bucket (virtual-hosted)",
			bucket:        "my-bucket",
			endpoint:      "https://s3.example.com",
			publicBaseURL: "",
			usePathStyle:  false,
			path:          "station1/ab/cd/file.mp3",
			expected:      "https://s3.example.com/station1/ab/cd/file.mp3",
		},
		{
			name:          "public base URL overrides everything",
			bucket:        "my-bucket",
			endpoint:      "https://s3.example.com",
			publicBaseURL: "https://cdn.example.com",
			usePathStyle:  false,
			path:          "station1/ab/cd/file.mp3",
			expected:      "https://cdn.example.com/station1/ab/cd/file.mp3",
		},
		{
			name:          "standard AWS S3 URL with virtual-hosted style",
			bucket:        "my-bucket",
			endpoint:      "",
			publicBaseURL: "",
			usePathStyle:  false,
			region:        "us-east-1",
			path:          "station1/ab/cd/file.mp3",
			expected:      "https://my-bucket.s3.us-east-1.amazonaws.com/station1/ab/cd/file.mp3",
		},
		{
			name:          "standard AWS S3 URL with path style",
			bucket:        "my-bucket",
			endpoint:      "",
			publicBaseURL: "",
			usePathStyle:  true,
			region:        "us-west-2",
			path:          "station1/ab/cd/file.mp3",
			expected:      "https://s3.us-west-2.amazonaws.com/my-bucket/station1/ab/cd/file.mp3",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s3 := &S3Storage{
				bucket:        tt.bucket,
				endpoint:      tt.endpoint,
				publicBaseURL: tt.publicBaseURL,
				usePathStyle:  tt.usePathStyle,
				region:        tt.region,
				logger:        zerolog.Nop(),
			}
			url := s3.URL(tt.path)
			if url != tt.expected {
				t.Errorf("S3Storage.URL() = %v, want %v", url, tt.expected)
			}
		})
	}
}

func TestS3Config(t *testing.T) {
	t.Run("default config has sensible values", func(t *testing.T) {
		cfg := DefaultS3Config()

		if cfg.Region == "" {
			t.Error("DefaultS3Config() Region should not be empty")
		}
		if cfg.PartSize <= 0 {
			t.Error("DefaultS3Config() PartSize should be positive")
		}
		if cfg.Concurrency <= 0 {
			t.Error("DefaultS3Config() Concurrency should be positive")
		}
		if cfg.MaxUploadParts <= 0 {
			t.Error("DefaultS3Config() MaxUploadParts should be positive")
		}
		if cfg.PresignedExpiry <= 0 {
			t.Error("DefaultS3Config() PresignedExpiry should be positive")
		}
	})
}

func TestFilesystemStorageCheckAccess(t *testing.T) {
	logger := zerolog.Nop()
	ctx := context.Background()

	t.Run("valid directory", func(t *testing.T) {
		fs := NewFilesystemStorage("/tmp", logger)
		err := fs.CheckAccess(ctx)
		if err != nil {
			t.Errorf("CheckAccess() for /tmp should succeed, got: %v", err)
		}
	})

	t.Run("non-existent directory", func(t *testing.T) {
		fs := NewFilesystemStorage("/nonexistent/path/that/does/not/exist", logger)
		err := fs.CheckAccess(ctx)
		if err == nil {
			t.Error("CheckAccess() for non-existent path should fail")
		}
	})
}
