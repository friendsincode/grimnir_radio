package media

import (
	"context"
	"fmt"
	"io"

	"github.com/rs/zerolog"
)

// S3Storage implements Storage using S3-compatible object storage.
type S3Storage struct {
	endpoint string
	bucket   string
	logger   zerolog.Logger
}

// NewS3Storage creates an S3-based storage backend.
// For now, this is a placeholder implementation that will be completed in later phases.
func NewS3Storage(endpoint, bucket string, logger zerolog.Logger) *S3Storage {
	return &S3Storage{
		endpoint: endpoint,
		bucket:   bucket,
		logger:   logger,
	}
}

// Store uploads a file to S3-compatible storage.
func (s3 *S3Storage) Store(ctx context.Context, stationID, mediaID string, file io.Reader) (string, error) {
	// TODO: Implement S3 upload using AWS SDK or MinIO client
	// This is a placeholder that will be implemented when S3 support is needed
	s3.logger.Warn().Msg("S3 storage not yet implemented, falling back would be required")
	return "", fmt.Errorf("S3 storage not yet implemented")
}

// Delete removes a file from S3 storage.
func (s3 *S3Storage) Delete(ctx context.Context, path string) error {
	// TODO: Implement S3 delete
	return fmt.Errorf("S3 storage not yet implemented")
}

// URL returns the public URL for an S3 object.
func (s3 *S3Storage) URL(path string) string {
	// TODO: Construct S3 URL
	return fmt.Sprintf("%s/%s/%s", s3.endpoint, s3.bucket, path)
}
