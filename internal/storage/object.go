package storage

import "context"

// ObjectStore abstracts object storage operations.
type ObjectStore interface {
	Put(ctx context.Context, key string, data []byte) error
	Get(ctx context.Context, key string) ([]byte, error)
}
