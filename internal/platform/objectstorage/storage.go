package objectstorage

import (
	"context"
	"io"
	"time"
)

type ObjectInfo struct {
	SizeBytes int64
	SHA256    []byte
	MIMEType  string
	ETag      string
}

type UploadSession struct {
	Provider  string
	ObjectKey string
	Method    string
	URL       string
	Headers   map[string]string
}

type Storage interface {
	Provider() string
	PrepareUpload(context.Context, string, string, time.Duration) (UploadSession, error)
	CompleteUpload(context.Context, string) (ObjectInfo, error)
	Put(context.Context, string, io.Reader, int64, string) (ObjectInfo, error)
	Open(context.Context, string) (io.ReadCloser, ObjectInfo, error)
	PresignGet(context.Context, string, time.Duration) (string, error)
	Delete(context.Context, string) error
}
