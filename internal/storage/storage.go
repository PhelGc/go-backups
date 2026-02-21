package storage

import (
	"context"
	"fmt"
	"io"

	"gobackups/internal/config"
)

// Writer is the target destination for a backup stream.
// Implementations handle atomicity internally:
//   - local: temp file then os.Rename
//   - http: multipart upload finalization
type Writer interface {
	// Store reads all bytes from src and persists them as filename.
	// filename is the final desired name (e.g. "mydb_20260220T020000Z.sql.gz").
	// Cancelling ctx aborts an in-progress write/upload.
	Store(ctx context.Context, filename string, src io.Reader) error
}

// New returns a storage Writer based on the given config.
func New(cfg config.StorageConfig) (Writer, error) {
	switch cfg.Kind {
	case "local":
		return NewLocal(cfg.Local), nil
	case "http":
		return NewHTTP(cfg.HTTP), nil
	default:
		return nil, fmt.Errorf("unknown storage kind: %q (supported: local, http)", cfg.Kind)
	}
}
