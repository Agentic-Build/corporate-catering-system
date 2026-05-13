// Package storage wraps the S3-compatible object storage API used by the
// platform. The same client targets MinIO for local/dev (path-style + custom
// endpoint), AWS S3 in production, or GCS via its HMAC-S3 compatibility layer.
package storage

import (
	"context"
	"fmt"
	"io"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
)

// S3Client is a thin wrapper over the AWS SDK S3 client carrying the bucket
// name we always operate against. Methods are deliberately narrow so callers
// (e.g. payroll settler) don't need to import the SDK directly.
type S3Client struct {
	Bucket string
	s3     *s3.Client
}

// S3Config describes the connection parameters for the underlying S3 client.
//
// Endpoint empty → use the AWS regional default.
// UsePathStyle true → required for MinIO; false for real AWS S3 / GCS HMAC.
type S3Config struct {
	Endpoint        string
	Region          string
	AccessKeyID     string
	SecretAccessKey string
	Bucket          string
	UsePathStyle    bool
}

// NewS3 builds an S3Client from S3Config. The function does not contact the
// server; the first network call happens on first Put/Get/EnsureBucket.
func NewS3(ctx context.Context, cfg S3Config) (*S3Client, error) {
	region := cfg.Region
	if region == "" {
		region = "us-east-1"
	}
	creds := credentials.NewStaticCredentialsProvider(cfg.AccessKeyID, cfg.SecretAccessKey, "")
	awsCfg, err := awsconfig.LoadDefaultConfig(ctx,
		awsconfig.WithRegion(region),
		awsconfig.WithCredentialsProvider(creds),
	)
	if err != nil {
		return nil, fmt.Errorf("aws config: %w", err)
	}
	opts := []func(*s3.Options){
		func(o *s3.Options) {
			o.UsePathStyle = cfg.UsePathStyle
			if cfg.Endpoint != "" {
				o.BaseEndpoint = aws.String(cfg.Endpoint)
			}
		},
	}
	client := s3.NewFromConfig(awsCfg, opts...)
	return &S3Client{Bucket: cfg.Bucket, s3: client}, nil
}

// EnsureBucket creates the bucket if it does not already exist. It is
// idempotent: BucketAlreadyOwnedByYou / BucketAlreadyExists are treated as
// success so the worker can re-run safely.
func (c *S3Client) EnsureBucket(ctx context.Context) error {
	_, err := c.s3.HeadBucket(ctx, &s3.HeadBucketInput{Bucket: aws.String(c.Bucket)})
	if err == nil {
		return nil
	}
	_, err = c.s3.CreateBucket(ctx, &s3.CreateBucketInput{Bucket: aws.String(c.Bucket)})
	if err != nil &&
		!strings.Contains(err.Error(), "BucketAlreadyOwnedByYou") &&
		!strings.Contains(err.Error(), "BucketAlreadyExists") {
		return fmt.Errorf("create bucket: %w", err)
	}
	return nil
}

// PutObject uploads body at the given key with the supplied content-type and
// returns a canonical s3://bucket/key URI that callers can persist as a stable
// reference (we record it on payroll_batch.export_uri).
func (c *S3Client) PutObject(ctx context.Context, key string, body io.Reader, contentType string) (string, error) {
	_, err := c.s3.PutObject(ctx, &s3.PutObjectInput{
		Bucket:      aws.String(c.Bucket),
		Key:         aws.String(key),
		Body:        body,
		ContentType: aws.String(contentType),
	})
	if err != nil {
		return "", fmt.Errorf("put %s: %w", key, err)
	}
	return fmt.Sprintf("s3://%s/%s", c.Bucket, key), nil
}

// GetObject opens the object at key for reading. Caller owns Close().
func (c *S3Client) GetObject(ctx context.Context, key string) (io.ReadCloser, error) {
	out, err := c.s3.GetObject(ctx, &s3.GetObjectInput{
		Bucket: aws.String(c.Bucket),
		Key:    aws.String(key),
	})
	if err != nil {
		return nil, fmt.Errorf("get %s: %w", key, err)
	}
	return out.Body, nil
}
