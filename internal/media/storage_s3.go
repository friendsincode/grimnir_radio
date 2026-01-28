/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/

package media

import (
	"context"
	"errors"
	"fmt"
	"io"
	"path/filepath"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"
	"github.com/rs/zerolog"
)

// S3Storage implements Storage using S3-compatible object storage.
type S3Storage struct {
	client        *s3.Client
	bucket        string
	region        string
	endpoint      string
	publicBaseURL string
	usePathStyle  bool
	logger        zerolog.Logger
}

// S3Config contains S3 storage configuration.
type S3Config struct {
	// AWS credentials
	AccessKeyID     string
	SecretAccessKey string
	SessionToken    string // Optional

	// S3 configuration
	Region   string
	Bucket   string
	Endpoint string // Optional: for S3-compatible services (MinIO, Spaces, etc.)

	// URL configuration
	PublicBaseURL string // Optional: custom CDN/public URL
	UsePathStyle  bool   // Use path-style URLs (required for MinIO)

	// Performance
	PartSize          int64 // Multipart upload part size (default: 5MB)
	Concurrency       int   // Upload concurrency (default: 5)
	MaxUploadParts    int32 // Max parts for multipart (default: 10000)
	PresignedExpiry   time.Duration
	DisableSSL        bool
	ForcePathStyle    bool
}

// DefaultS3Config returns default S3 configuration.
func DefaultS3Config() S3Config {
	return S3Config{
		Region:            "us-east-1",
		PartSize:          5 * 1024 * 1024, // 5MB
		Concurrency:       5,
		MaxUploadParts:    10000,
		PresignedExpiry:   15 * time.Minute,
		UsePathStyle:      false,
		DisableSSL:        false,
		ForcePathStyle:    false,
	}
}

// NewS3Storage creates an S3-based storage backend.
// Supports AWS S3 and S3-compatible services (MinIO, DigitalOcean Spaces, Backblaze B2, etc.)
func NewS3Storage(ctx context.Context, cfg S3Config, logger zerolog.Logger) (*S3Storage, error) {
	var awsCfg aws.Config
	var err error

	// If endpoint is provided (S3-compatible service), use custom resolver
	if cfg.Endpoint != "" {
		// Custom endpoint for MinIO, DigitalOcean Spaces, etc.
		customResolver := aws.EndpointResolverWithOptionsFunc(func(service, region string, options ...interface{}) (aws.Endpoint, error) {
			if service == s3.ServiceID {
				return aws.Endpoint{
					URL:               cfg.Endpoint,
					HostnameImmutable: true,
					SigningRegion:     cfg.Region,
				}, nil
			}
			return aws.Endpoint{}, fmt.Errorf("unknown endpoint requested")
		})

		awsCfg, err = config.LoadDefaultConfig(ctx,
			config.WithRegion(cfg.Region),
			config.WithEndpointResolverWithOptions(customResolver),
			config.WithCredentialsProvider(credentials.NewStaticCredentialsProvider(
				cfg.AccessKeyID,
				cfg.SecretAccessKey,
				cfg.SessionToken,
			)),
		)
	} else {
		// Standard AWS S3
		awsCfg, err = config.LoadDefaultConfig(ctx,
			config.WithRegion(cfg.Region),
			config.WithCredentialsProvider(credentials.NewStaticCredentialsProvider(
				cfg.AccessKeyID,
				cfg.SecretAccessKey,
				cfg.SessionToken,
			)),
		)
	}

	if err != nil {
		return nil, fmt.Errorf("load AWS config: %w", err)
	}

	// Create S3 client
	client := s3.NewFromConfig(awsCfg, func(o *s3.Options) {
		if cfg.ForcePathStyle || cfg.UsePathStyle {
			o.UsePathStyle = true
		}
	})

	// Test bucket access
	_, err = client.HeadBucket(ctx, &s3.HeadBucketInput{
		Bucket: aws.String(cfg.Bucket),
	})
	if err != nil {
		logger.Warn().Err(err).Str("bucket", cfg.Bucket).Msg("S3 bucket not accessible (may not exist or no permissions)")
		// Don't fail here - bucket might be created later
	} else {
		logger.Info().Str("bucket", cfg.Bucket).Str("region", cfg.Region).Msg("S3 storage initialized")
	}

	return &S3Storage{
		client:        client,
		bucket:        cfg.Bucket,
		region:        cfg.Region,
		endpoint:      cfg.Endpoint,
		publicBaseURL: cfg.PublicBaseURL,
		usePathStyle:  cfg.UsePathStyle || cfg.ForcePathStyle,
		logger:        logger,
	}, nil
}

// Store uploads a file to S3-compatible storage.
// Path format: media/{station_id}/{media_id}/{filename}
func (s3s *S3Storage) Store(ctx context.Context, stationID, mediaID string, file io.Reader) (string, error) {
	// Generate S3 key
	key := fmt.Sprintf("media/%s/%s", stationID, mediaID)

	s3s.logger.Debug().
		Str("bucket", s3s.bucket).
		Str("key", key).
		Msg("uploading file to S3")

	// Upload file
	_, err := s3s.client.PutObject(ctx, &s3.PutObjectInput{
		Bucket:      aws.String(s3s.bucket),
		Key:         aws.String(key),
		Body:        file,
		ContentType: aws.String(detectContentType(key)),
		// Metadata for tracking
		Metadata: map[string]string{
			"station-id": stationID,
			"media-id":   mediaID,
			"uploaded":   time.Now().Format(time.RFC3339),
		},
	})

	if err != nil {
		s3s.logger.Error().Err(err).Str("key", key).Msg("failed to upload to S3")
		return "", fmt.Errorf("upload to S3: %w", err)
	}

	s3s.logger.Info().
		Str("bucket", s3s.bucket).
		Str("key", key).
		Msg("file uploaded to S3 successfully")

	return key, nil
}

// Delete removes a file from S3 storage.
func (s3s *S3Storage) Delete(ctx context.Context, path string) error {
	s3s.logger.Debug().
		Str("bucket", s3s.bucket).
		Str("key", path).
		Msg("deleting file from S3")

	_, err := s3s.client.DeleteObject(ctx, &s3.DeleteObjectInput{
		Bucket: aws.String(s3s.bucket),
		Key:    aws.String(path),
	})

	if err != nil {
		s3s.logger.Error().Err(err).Str("key", path).Msg("failed to delete from S3")
		return fmt.Errorf("delete from S3: %w", err)
	}

	s3s.logger.Info().
		Str("bucket", s3s.bucket).
		Str("key", path).
		Msg("file deleted from S3 successfully")

	return nil
}

// URL returns the public URL for an S3 object.
// If PublicBaseURL is set, uses that (for CDN/custom domains).
// Otherwise, constructs standard S3 URL.
func (s3s *S3Storage) URL(path string) string {
	// Use custom public base URL if configured (CDN, CloudFront, etc.)
	if s3s.publicBaseURL != "" {
		return fmt.Sprintf("%s/%s", s3s.publicBaseURL, path)
	}

	// Use custom endpoint if configured (MinIO, Spaces, etc.)
	if s3s.endpoint != "" {
		if s3s.usePathStyle {
			return fmt.Sprintf("%s/%s/%s", s3s.endpoint, s3s.bucket, path)
		}
		return fmt.Sprintf("%s/%s", s3s.endpoint, path)
	}

	// Standard AWS S3 URL
	if s3s.usePathStyle {
		return fmt.Sprintf("https://s3.%s.amazonaws.com/%s/%s", s3s.region, s3s.bucket, path)
	}
	return fmt.Sprintf("https://%s.s3.%s.amazonaws.com/%s", s3s.bucket, s3s.region, path)
}

// CheckAccess verifies the S3 bucket exists and is accessible.
func (s3s *S3Storage) CheckAccess(ctx context.Context) error {
	_, err := s3s.client.HeadBucket(ctx, &s3.HeadBucketInput{
		Bucket: aws.String(s3s.bucket),
	})
	if err != nil {
		return fmt.Errorf("cannot access S3 bucket %q: %w", s3s.bucket, err)
	}
	return nil
}

// PresignedURL generates a presigned URL for private/authenticated access.
// Useful for private buckets where direct URL access is restricted.
func (s3s *S3Storage) PresignedURL(ctx context.Context, path string, expiry time.Duration) (string, error) {
	presignClient := s3.NewPresignClient(s3s.client)

	request, err := presignClient.PresignGetObject(ctx, &s3.GetObjectInput{
		Bucket: aws.String(s3s.bucket),
		Key:    aws.String(path),
	}, func(opts *s3.PresignOptions) {
		opts.Expires = expiry
	})

	if err != nil {
		return "", fmt.Errorf("generate presigned URL: %w", err)
	}

	return request.URL, nil
}

// Exists checks if a file exists in S3.
func (s3s *S3Storage) Exists(ctx context.Context, path string) (bool, error) {
	_, err := s3s.client.HeadObject(ctx, &s3.HeadObjectInput{
		Bucket: aws.String(s3s.bucket),
		Key:    aws.String(path),
	})

	if err != nil {
		// Check if error is "not found"
		var notFound *types.NotFound
		if errors.As(err, &notFound) {
			return false, nil
		}
		// Check for NoSuchKey error (some S3-compatible services use this)
		var noSuchKey *types.NoSuchKey
		if errors.As(err, &noSuchKey) {
			return false, nil
		}
		// Other errors are actual errors
		return false, fmt.Errorf("check object existence: %w", err)
	}

	return true, nil
}

// GetMetadata retrieves metadata for an S3 object.
func (s3s *S3Storage) GetMetadata(ctx context.Context, path string) (map[string]string, error) {
	result, err := s3s.client.HeadObject(ctx, &s3.HeadObjectInput{
		Bucket: aws.String(s3s.bucket),
		Key:    aws.String(path),
	})

	if err != nil {
		return nil, fmt.Errorf("get object metadata: %w", err)
	}

	return result.Metadata, nil
}

// Copy copies an object within S3 (server-side copy, no download/upload).
func (s3s *S3Storage) Copy(ctx context.Context, sourcePath, destPath string) error {
	copySource := fmt.Sprintf("%s/%s", s3s.bucket, sourcePath)

	_, err := s3s.client.CopyObject(ctx, &s3.CopyObjectInput{
		Bucket:     aws.String(s3s.bucket),
		CopySource: aws.String(copySource),
		Key:        aws.String(destPath),
	})

	if err != nil {
		return fmt.Errorf("copy object in S3: %w", err)
	}

	s3s.logger.Info().
		Str("source", sourcePath).
		Str("dest", destPath).
		Msg("object copied in S3")

	return nil
}

// ListObjects lists objects with a given prefix.
func (s3s *S3Storage) ListObjects(ctx context.Context, prefix string, maxKeys int32) ([]string, error) {
	if maxKeys == 0 {
		maxKeys = 1000
	}

	result, err := s3s.client.ListObjectsV2(ctx, &s3.ListObjectsV2Input{
		Bucket:  aws.String(s3s.bucket),
		Prefix:  aws.String(prefix),
		MaxKeys: aws.Int32(maxKeys),
	})

	if err != nil {
		return nil, fmt.Errorf("list objects: %w", err)
	}

	keys := make([]string, 0, len(result.Contents))
	for _, obj := range result.Contents {
		if obj.Key != nil {
			keys = append(keys, *obj.Key)
		}
	}

	return keys, nil
}

// detectContentType returns MIME type based on file extension.
func detectContentType(filename string) string {
	ext := filepath.Ext(filename)

	// Audio formats
	switch ext {
	case ".mp3":
		return "audio/mpeg"
	case ".flac":
		return "audio/flac"
	case ".ogg", ".oga":
		return "audio/ogg"
	case ".m4a":
		return "audio/mp4"
	case ".wav":
		return "audio/wav"
	case ".aac":
		return "audio/aac"
	case ".opus":
		return "audio/opus"
	default:
		return "application/octet-stream"
	}
}
