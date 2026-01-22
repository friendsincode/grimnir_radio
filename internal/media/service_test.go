package media

import (
	"testing"

	"github.com/friendsincode/grimnir_radio/internal/config"
	"github.com/rs/zerolog"
)

func TestNewService(t *testing.T) {
	logger := zerolog.Nop()

	tests := []struct {
		name               string
		objectStorageURL   string
		expectedStorageType string
	}{
		{
			name:               "filesystem storage when no object storage URL",
			objectStorageURL:   "",
			expectedStorageType: "filesystem",
		},
		{
			name:               "s3 storage when object storage URL provided",
			objectStorageURL:   "s3://bucket-name",
			expectedStorageType: "s3",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &config.Config{
				MediaRoot:        "/tmp/media",
				ObjectStorageURL: tt.objectStorageURL,
			}

			svc := NewService(cfg, logger)

			if svc == nil {
				t.Fatal("NewService() returned nil")
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
			name:      "standard path",
			stationID: "station1",
			mediaID:   "abcd1234efgh5678",
			extension: ".mp3",
			expected:  "station1/ab/cd/abcd1234efgh5678.mp3",
		},
		{
			name:      "short mediaID",
			stationID: "station2",
			mediaID:   "abc",
			extension: ".flac",
			expected:  "station2/abc.flac",
		},
		{
			name:      "exactly 4 chars",
			stationID: "station3",
			mediaID:   "abcd",
			extension: ".wav",
			expected:  "station3/ab/cd/abcd.wav",
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

func TestStorageURL(t *testing.T) {
	logger := zerolog.Nop()

	t.Run("filesystem storage URL", func(t *testing.T) {
		fs := NewFilesystemStorage("/tmp/media", logger)
		path := "/tmp/media/station1/ab/cd/file.mp3"
		url := fs.URL(path)

		if url != path {
			t.Errorf("FilesystemStorage.URL() = %v, want %v", url, path)
		}
	})

	t.Run("s3 storage URL", func(t *testing.T) {
		s3 := NewS3Storage("https://s3.example.com", "my-bucket", logger)
		path := "station1/ab/cd/file.mp3"
		url := s3.URL(path)

		expected := "https://s3.example.com/my-bucket/station1/ab/cd/file.mp3"
		if url != expected {
			t.Errorf("S3Storage.URL() = %v, want %v", url, expected)
		}
	})
}
