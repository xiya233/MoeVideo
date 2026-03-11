package storage

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	aws "github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"

	"moevideo/backend/internal/config"
)

type Service struct {
	cfg       config.Config
	s3Client  *s3.Client
	s3Presign *s3.PresignClient
}

func NewService(cfg config.Config) (*Service, error) {
	svc := &Service{cfg: cfg}
	if cfg.StorageDriver != "s3" {
		return svc, nil
	}

	loadOpts := []func(*awsconfig.LoadOptions) error{awsconfig.WithRegion(cfg.S3Region)}
	if cfg.S3AccessKeyID != "" && cfg.S3SecretAccessKey != "" {
		creds := credentials.NewStaticCredentialsProvider(cfg.S3AccessKeyID, cfg.S3SecretAccessKey, cfg.S3SessionToken)
		loadOpts = append(loadOpts, awsconfig.WithCredentialsProvider(creds))
	}

	awsCfg, err := awsconfig.LoadDefaultConfig(context.Background(), loadOpts...)
	if err != nil {
		return nil, fmt.Errorf("load aws config: %w", err)
	}

	opts := func(o *s3.Options) {
		o.UsePathStyle = cfg.S3ForcePathStyle
		if cfg.S3Endpoint != "" {
			o.BaseEndpoint = aws.String(cfg.S3Endpoint)
		}
	}

	client := s3.NewFromConfig(awsCfg, opts)
	svc.s3Client = client
	svc.s3Presign = s3.NewPresignClient(client)
	return svc, nil
}

func (s *Service) PresignS3Put(ctx context.Context, key, contentType string, expires time.Duration) (string, map[string]string, error) {
	if s.cfg.StorageDriver != "s3" || s.s3Presign == nil {
		return "", nil, fmt.Errorf("s3 storage is not configured")
	}

	input := &s3.PutObjectInput{
		Bucket:      aws.String(s.cfg.S3Bucket),
		Key:         aws.String(key),
		ContentType: aws.String(contentType),
	}
	presigned, err := s.s3Presign.PresignPutObject(ctx, input, func(opts *s3.PresignOptions) {
		opts.Expires = expires
	})
	if err != nil {
		return "", nil, fmt.Errorf("presign s3 put: %w", err)
	}

	headers := map[string]string{}
	for key, vals := range presigned.SignedHeader {
		if len(vals) == 0 {
			continue
		}
		headers[key] = vals[0]
	}
	return presigned.URL, headers, nil
}

func (s *Service) ObjectURL(provider, bucket, objectKey string) string {
	switch provider {
	case "s3":
		if s.cfg.S3PublicBaseURL != "" {
			return strings.TrimRight(s.cfg.S3PublicBaseURL, "/") + "/" + objectKey
		}
		if s.cfg.S3Endpoint != "" {
			return strings.TrimRight(s.cfg.S3Endpoint, "/") + "/" + bucket + "/" + objectKey
		}
		region := s.cfg.S3Region
		if region == "" {
			region = "us-east-1"
		}
		return fmt.Sprintf("https://%s.s3.%s.amazonaws.com/%s", bucket, region, objectKey)
	default:
		return strings.TrimRight(s.cfg.PublicBaseURL, "/") + "/media/" + objectKey
	}
}

func (s *Service) LocalObjectPath(objectKey string) string {
	cleaned := strings.TrimPrefix(filepath.Clean("/"+objectKey), "/")
	return filepath.Join(s.cfg.LocalStorageDir, cleaned)
}

func (s *Service) Driver() string {
	return s.cfg.StorageDriver
}

func (s *Service) Bucket() string {
	return s.cfg.S3Bucket
}
