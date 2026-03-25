package storage

import (
	"context"
	"fmt"
	"io"
	"strings"

	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
)

// MinIOStore implements domain.ObjectStore with MinIO/S3.
type MinIOStore struct {
	client *minio.Client
}

// NewMinIOStore constructs a MinIO-backed object store.
func NewMinIOStore(endpoint, accessKey, secretKey string, useSSL bool) (*MinIOStore, error) {
	if endpoint == "" {
		return nil, fmt.Errorf("S3 endpoint is required")
	}
	clean := strings.TrimPrefix(strings.TrimPrefix(endpoint, "http://"), "https://")
	client, err := minio.New(clean, &minio.Options{
		Creds:  credentials.NewStaticV4(accessKey, secretKey, ""),
		Secure: useSSL,
	})
	if err != nil {
		return nil, fmt.Errorf("create minio client: %w", err)
	}
	return &MinIOStore{client: client}, nil
}

// GetObject retrieves an object by bucket and key.
func (s *MinIOStore) GetObject(ctx context.Context, bucket, key string) (io.ReadCloser, error) {
	obj, err := s.client.GetObject(ctx, bucket, key, minio.GetObjectOptions{})
	if err != nil {
		return nil, err
	}
	return obj, nil
}
