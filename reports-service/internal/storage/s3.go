package storage

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"

	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
	"github.com/minio/minio-go/v7/pkg/lifecycle"
)

const cacheControlImmutable = "public, max-age=31536000, immutable"

type Store struct {
	client *minio.Client
	bucket string
}

func New(ctx context.Context, endpoint, accessKey, secretKey, bucket string, useSSL bool, retentionDays int) (*Store, error) {
	client, err := minio.New(endpoint, &minio.Options{
		Creds:  credentials.NewStaticV4(accessKey, secretKey, ""),
		Secure: useSSL,
	})
	if err != nil {
		return nil, fmt.Errorf("minio client: %w", err)
	}

	store := &Store{client: client, bucket: bucket}
	if err := store.bootstrap(ctx, retentionDays); err != nil {
		return nil, err
	}
	return store, nil
}

func (s *Store) Bucket() string { return s.bucket }

func (s *Store) Exists(ctx context.Context, key string) (bool, error) {
	_, err := s.client.StatObject(ctx, s.bucket, key, minio.StatObjectOptions{})
	if err == nil {
		return true, nil
	}

	var response minio.ErrorResponse
	if errors.As(err, &response) && (response.StatusCode == http.StatusNotFound || response.Code == "NoSuchKey") {
		return false, nil
	}
	return false, fmt.Errorf("stat object %s: %w", key, err)
}

func (s *Store) PutJSON(ctx context.Context, key string, payload any) error {
	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("marshal report: %w", err)
	}

	_, err = s.client.PutObject(ctx, s.bucket, key, bytes.NewReader(body), int64(len(body)),
		minio.PutObjectOptions{
			ContentType:  "application/json; charset=utf-8",
			CacheControl: cacheControlImmutable,
		})
	if err != nil {
		return fmt.Errorf("put object %s: %w", key, err)
	}
	return nil
}

func (s *Store) bootstrap(ctx context.Context, retentionDays int) error {
	exists, err := s.client.BucketExists(ctx, s.bucket)
	if err != nil {
		return fmt.Errorf("bucket exists: %w", err)
	}
	if !exists {
		if err := s.client.MakeBucket(ctx, s.bucket, minio.MakeBucketOptions{}); err != nil {
			return fmt.Errorf("make bucket: %w", err)
		}
	}

	policy := fmt.Sprintf(`{
      "Version": "2012-10-17",
      "Statement": [
        {
          "Effect": "Allow",
          "Principal": {"AWS": ["*"]},
          "Action": ["s3:GetObject"],
          "Resource": ["arn:aws:s3:::%s/*"]
        }
      ]
    }`, s.bucket)
	if err := s.client.SetBucketPolicy(ctx, s.bucket, policy); err != nil {
		return fmt.Errorf("set bucket policy: %w", err)
	}

	config := lifecycle.NewConfiguration()
	config.Rules = []lifecycle.Rule{
		{
			ID:         "expire-superseded-reports",
			Status:     "Enabled",
			RuleFilter: lifecycle.Filter{Prefix: ""},
			Expiration: lifecycle.Expiration{Days: lifecycle.ExpirationDays(retentionDays)},
		},
	}
	if err := s.client.SetBucketLifecycle(ctx, s.bucket, config); err != nil {
		return fmt.Errorf("set bucket lifecycle: %w", err)
	}
	return nil
}
