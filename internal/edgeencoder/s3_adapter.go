/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/

package edgeencoder

import (
	"context"
	"fmt"
	"io"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/s3"
)

// s3Adapter wraps an aws-sdk-go-v2 s3.Client so it satisfies S3PutClient.
// Credentials come from the standard AWS SDK chain (env vars, shared config,
// IAM role); no new credential env vars are added at this layer.
type s3Adapter struct {
	client *s3.Client
}

// NewS3Adapter builds an S3PutClient from the edge-encoder Config. Region
// defaults to "us-east-1" in LoadConfigFromEnv; endpoint & path-style are
// optional for MinIO & other S3-compatible services.
func NewS3Adapter(ctx context.Context, cfg *Config) (S3PutClient, error) {
	loadOpts := []func(*config.LoadOptions) error{
		config.WithRegion(cfg.HLSS3Region),
	}
	if cfg.HLSS3Endpoint != "" {
		resolver := aws.EndpointResolverWithOptionsFunc(func(service, region string, options ...interface{}) (aws.Endpoint, error) {
			if service == s3.ServiceID {
				return aws.Endpoint{
					URL:               cfg.HLSS3Endpoint,
					HostnameImmutable: true,
					SigningRegion:     cfg.HLSS3Region,
				}, nil
			}
			return aws.Endpoint{}, fmt.Errorf("unknown endpoint requested")
		})
		loadOpts = append(loadOpts, config.WithEndpointResolverWithOptions(resolver))
	}

	awsCfg, err := config.LoadDefaultConfig(ctx, loadOpts...)
	if err != nil {
		return nil, fmt.Errorf("load AWS config: %w", err)
	}

	client := s3.NewFromConfig(awsCfg, func(o *s3.Options) {
		if cfg.HLSS3UsePathStyle {
			o.UsePathStyle = true
		}
	})
	return &s3Adapter{client: client}, nil
}

func (a *s3Adapter) PutObject(ctx context.Context, bucket, key string, body io.Reader) error {
	_, err := a.client.PutObject(ctx, &s3.PutObjectInput{
		Bucket: aws.String(bucket),
		Key:    aws.String(key),
		Body:   body,
	})
	if err != nil {
		return fmt.Errorf("s3 put %s/%s: %w", bucket, key, err)
	}
	return nil
}
