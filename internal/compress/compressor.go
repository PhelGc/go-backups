package compress

import (
	"fmt"
	"io"

	"gobackups/internal/config"
)

// Compressor wraps a destination writer with a streaming compression layer.
// The caller must call Close on the returned WriteCloser to flush internal
// compressor buffers before the underlying writer is considered complete.
type Compressor interface {
	// Wrap returns a WriteCloser that compresses data written to it
	// and forwards compressed bytes to dst.
	Wrap(dst io.Writer) (io.WriteCloser, error)

	// FileExtension returns the suffix added by this compressor, e.g. ".gz" or ".zst".
	FileExtension() string
}

// New returns a Compressor based on the given config.
func New(cfg config.CompressConfig) (Compressor, error) {
	switch cfg.Kind {
	case "gzip":
		return NewGzip(cfg.Level), nil
	case "zstd":
		return NewZstd(cfg.Level), nil
	default:
		return nil, fmt.Errorf("unknown compression kind: %q (supported: gzip, zstd)", cfg.Kind)
	}
}
