package database

import (
	"context"
	"io"
)

// Dumper produces a raw SQL byte stream from a database.
// Implementations must not lock tables for reads (e.g., pass
// --single-transaction for MySQL InnoDB).
// The caller must close the returned ReadCloser to release resources.
type Dumper interface {
	// Dump starts the dump process and returns a reader over the SQL stream.
	// The dump runs concurrently; drain the reader to drive it.
	// Cancelling ctx aborts the underlying subprocess.
	Dump(ctx context.Context) (io.ReadCloser, error)

	// FileExtension returns the file extension without compression suffix, e.g. ".sql".
	FileExtension() string
}
