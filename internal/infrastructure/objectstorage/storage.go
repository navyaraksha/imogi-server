package objectstorage

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
	platformstorage "github.com/navyaraksha/imogi/internal/platform/objectstorage"
)

type ObjectInfo = platformstorage.ObjectInfo
type UploadSession = platformstorage.UploadSession
type Storage = platformstorage.Storage

type Config struct {
	Driver        string
	Endpoint      string
	Region        string
	Bucket        string
	AccessKey     string
	SecretKey     string
	UsePathStyle  bool
	RootDirectory string
	Secure        bool
}

func New(config Config) (Storage, error) {
	switch strings.ToLower(strings.TrimSpace(config.Driver)) {
	case "", "filesystem":
		return NewFilesystem(config.RootDirectory)
	case "s3":
		return NewS3(config)
	default:
		return nil, fmt.Errorf("unsupported object storage driver %q", config.Driver)
	}
}

type Filesystem struct{ root string }

func (*Filesystem) Provider() string { return "filesystem" }

func NewFilesystem(root string) (*Filesystem, error) {
	if strings.TrimSpace(root) == "" {
		return nil, errors.New("filesystem storage root is required")
	}
	if err := os.MkdirAll(root, 0o700); err != nil {
		return nil, fmt.Errorf("create filesystem storage root: %w", err)
	}
	return &Filesystem{root: root}, nil
}

func (storage *Filesystem) PrepareUpload(_ context.Context, key, _ string, _ time.Duration) (UploadSession, error) {
	if _, err := storage.path(key); err != nil {
		return UploadSession{}, err
	}
	return UploadSession{Provider: "filesystem", ObjectKey: key, Method: "PUT", Headers: map[string]string{}}, nil
}

func (storage *Filesystem) CompleteUpload(ctx context.Context, key string) (ObjectInfo, error) {
	return storage.objectInfo(ctx, key)
}

func (storage *Filesystem) Put(ctx context.Context, key string, input io.Reader, size int64, mimeType string) (ObjectInfo, error) {
	path, err := storage.path(key)
	if err != nil {
		return ObjectInfo{}, err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return ObjectInfo{}, err
	}
	temporary, err := os.CreateTemp(filepath.Dir(path), ".upload-*")
	if err != nil {
		return ObjectInfo{}, err
	}
	temporaryPath := temporary.Name()
	defer os.Remove(temporaryPath)
	defer temporary.Close()
	hash := sha256.New()
	written, err := io.Copy(io.MultiWriter(temporary, hash), io.LimitReader(input, size+1))
	if err != nil {
		return ObjectInfo{}, err
	}
	if written != size {
		return ObjectInfo{}, fmt.Errorf("uploaded size %d does not match expected size %d", written, size)
	}
	if err := temporary.Close(); err != nil {
		return ObjectInfo{}, err
	}
	if err := os.Chmod(temporaryPath, 0o600); err != nil {
		return ObjectInfo{}, err
	}
	if err := os.Rename(temporaryPath, path); err != nil {
		return ObjectInfo{}, err
	}
	return ObjectInfo{SizeBytes: written, SHA256: hash.Sum(nil), MIMEType: mimeType}, nil
}

func (storage *Filesystem) Open(_ context.Context, key string) (io.ReadCloser, ObjectInfo, error) {
	path, err := storage.path(key)
	if err != nil {
		return nil, ObjectInfo{}, err
	}
	file, err := os.Open(path)
	if err != nil {
		return nil, ObjectInfo{}, err
	}
	stat, err := file.Stat()
	if err != nil {
		file.Close()
		return nil, ObjectInfo{}, err
	}
	return file, ObjectInfo{SizeBytes: stat.Size()}, nil
}

func (storage *Filesystem) PresignGet(_ context.Context, key string, _ time.Duration) (string, error) {
	path, err := storage.path(key)
	if err != nil {
		return "", err
	}
	return (&url.URL{Scheme: "file", Path: path}).String(), nil
}

func (storage *Filesystem) Delete(_ context.Context, key string) error {
	path, err := storage.path(key)
	if err != nil {
		return err
	}
	return os.Remove(path)
}

func (storage *Filesystem) objectInfo(ctx context.Context, key string) (ObjectInfo, error) {
	file, info, err := storage.Open(ctx, key)
	if err != nil {
		return ObjectInfo{}, err
	}
	defer file.Close()
	hash := sha256.New()
	if _, err := io.Copy(hash, file); err != nil {
		return ObjectInfo{}, err
	}
	info.SHA256 = hash.Sum(nil)
	return info, nil
}

func (storage *Filesystem) path(key string) (string, error) {
	clean := filepath.Clean("/" + key)
	path := filepath.Join(storage.root, clean)
	relative, err := filepath.Rel(storage.root, path)
	if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return "", errors.New("object key escapes storage root")
	}
	return path, nil
}

type S3 struct {
	client *minio.Client
	bucket string
}

func (*S3) Provider() string { return "s3" }

func NewS3(config Config) (*S3, error) {
	if config.Endpoint == "" || config.Bucket == "" || config.AccessKey == "" || config.SecretKey == "" {
		return nil, errors.New("s3 endpoint, bucket, access key, and secret key are required")
	}
	client, err := minio.New(config.Endpoint, &minio.Options{
		Creds:  credentials.NewStaticV4(config.AccessKey, config.SecretKey, ""),
		Secure: config.Secure,
		Region: config.Region,
		BucketLookup: func() minio.BucketLookupType {
			if config.UsePathStyle {
				return minio.BucketLookupPath
			}
			return minio.BucketLookupAuto
		}(),
	})
	if err != nil {
		return nil, err
	}
	client.SetAppInfo("imogi", "v0")
	return &S3{client: client, bucket: config.Bucket}, nil
}

func (storage *S3) PrepareUpload(ctx context.Context, key, _ string, expiry time.Duration) (UploadSession, error) {
	putURL, err := storage.client.PresignedPutObject(ctx, storage.bucket, key, expiry)
	if err != nil {
		return UploadSession{}, err
	}
	return UploadSession{Provider: "s3", ObjectKey: key, Method: "PUT", URL: putURL.String(), Headers: map[string]string{}}, nil
}

func (storage *S3) CompleteUpload(ctx context.Context, key string) (ObjectInfo, error) {
	object, err := storage.client.GetObject(ctx, storage.bucket, key, minio.GetObjectOptions{})
	if err != nil {
		return ObjectInfo{}, err
	}
	info, err := object.Stat()
	if err != nil {
		object.Close()
		return ObjectInfo{}, err
	}
	hash := sha256.New()
	if _, err := io.Copy(hash, object); err != nil {
		object.Close()
		return ObjectInfo{}, err
	}
	if err := object.Close(); err != nil {
		return ObjectInfo{}, err
	}
	return ObjectInfo{SizeBytes: info.Size, SHA256: hash.Sum(nil), ETag: info.ETag, MIMEType: info.ContentType}, nil
}

func (storage *S3) Put(ctx context.Context, key string, input io.Reader, size int64, mimeType string) (ObjectInfo, error) {
	hash := sha256.New()
	info, err := storage.client.PutObject(ctx, storage.bucket, key, io.TeeReader(input, hash), size, minio.PutObjectOptions{ContentType: mimeType})
	if err != nil {
		return ObjectInfo{}, err
	}
	return ObjectInfo{SizeBytes: info.Size, SHA256: hash.Sum(nil), ETag: info.ETag, MIMEType: mimeType}, nil
}

func (storage *S3) Open(ctx context.Context, key string) (io.ReadCloser, ObjectInfo, error) {
	object, err := storage.client.GetObject(ctx, storage.bucket, key, minio.GetObjectOptions{})
	if err != nil {
		return nil, ObjectInfo{}, err
	}
	info, err := object.Stat()
	if err != nil {
		object.Close()
		return nil, ObjectInfo{}, err
	}
	return object, ObjectInfo{SizeBytes: info.Size, ETag: info.ETag, MIMEType: info.ContentType}, nil
}

func (storage *S3) PresignGet(ctx context.Context, key string, expiry time.Duration) (string, error) {
	getURL, err := storage.client.PresignedGetObject(ctx, storage.bucket, key, expiry, nil)
	if err != nil {
		return "", err
	}
	return getURL.String(), nil
}

func (storage *S3) Delete(ctx context.Context, key string) error {
	return storage.client.RemoveObject(ctx, storage.bucket, key, minio.RemoveObjectOptions{})
}
