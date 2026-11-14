package media

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
)

// LocalStorage keeps objects under a directory; http serves them at /media/.
type LocalStorage struct{ dir string }

// NewLocalStorage creates dir if needed.
func NewLocalStorage(dir string) (*LocalStorage, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("media: %w", err)
	}
	return &LocalStorage{dir: dir}, nil
}

// safeKey rejects keys that would escape the directory.
func safeKey(key string) (string, error) {
	clean := path.Clean("/" + key)[1:]
	if clean == "" || clean != key || strings.Contains(key, "..") || strings.ContainsRune(key, '\\') {
		return "", errors.New("media: invalid key")
	}
	return clean, nil
}

func (l *LocalStorage) Put(_ context.Context, key string, data []byte, _ string) error {
	k, err := safeKey(key)
	if err != nil {
		return err
	}
	p := filepath.Join(l.dir, filepath.FromSlash(k))
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return err
	}
	tmp := p + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, p)
}

func (l *LocalStorage) Delete(_ context.Context, key string) error {
	k, err := safeKey(key)
	if err != nil {
		return err
	}
	err = os.Remove(filepath.Join(l.dir, filepath.FromSlash(k)))
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	return err
}

// FS serves the stored files.
func (l *LocalStorage) FS() fs.FS { return os.DirFS(l.dir) }

// S3Storage is an S3-compatible bucket (AWS, R2, MinIO).
type S3Storage struct {
	client *s3.Client
	bucket string
}

func NewS3Storage(endpoint, region, bucket, accessKey, secretKey string) *S3Storage {
	opts := s3.Options{
		Region:       region,
		Credentials:  credentials.NewStaticCredentialsProvider(accessKey, secretKey, ""),
		UsePathStyle: true,
	}
	if endpoint != "" {
		opts.BaseEndpoint = aws.String(endpoint)
	}
	return &S3Storage{client: s3.New(opts), bucket: bucket}
}

func (s *S3Storage) Put(ctx context.Context, key string, data []byte, contentType string) error {
	_, err := s.client.PutObject(ctx, &s3.PutObjectInput{
		Bucket:       aws.String(s.bucket),
		Key:          aws.String(key),
		Body:         bytes.NewReader(data),
		ContentType:  aws.String(contentType),
		CacheControl: aws.String("public, max-age=31536000, immutable"),
	})
	return err
}

func (s *S3Storage) Delete(ctx context.Context, key string) error {
	_, err := s.client.DeleteObject(ctx, &s3.DeleteObjectInput{Bucket: aws.String(s.bucket), Key: aws.String(key)})
	return err
}

// MemStorage is an in-memory Storage for tests.
type MemStorage struct {
	Objects map[string][]byte
}

func NewMemStorage() *MemStorage { return &MemStorage{Objects: map[string][]byte{}} }

func (m *MemStorage) Put(_ context.Context, key string, data []byte, _ string) error {
	m.Objects[key] = append([]byte(nil), data...)
	return nil
}

func (m *MemStorage) Delete(_ context.Context, key string) error {
	delete(m.Objects, key)
	return nil
}
