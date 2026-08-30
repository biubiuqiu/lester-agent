package blob

import (
	"context"
	"io"
)

// Store is the object-storage boundary used by the skill catalog. Implementations
// can target MinIO, AWS S3, or any other durable blob service.
type Store interface {
	Ensure(context.Context) error
	Put(context.Context, string, io.Reader, int64, string) error
	Get(context.Context, string) (io.ReadCloser, error)
	Delete(context.Context, string) error
}
