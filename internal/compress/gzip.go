package compress

import (
	"compress/gzip"
	"io"
)

// GzipCompressor compresses data using standard gzip.
type GzipCompressor struct {
	level int
}

// NewGzip creates a GzipCompressor with the given compression level.
// Level 0 uses gzip.DefaultCompression (-1, balanced speed/ratio).
func NewGzip(level int) *GzipCompressor {
	return &GzipCompressor{level: level}
}

// FileExtension returns ".gz".
func (c *GzipCompressor) FileExtension() string {
	return ".gz"
}

// Wrap returns a WriteCloser that gzip-compresses writes to dst.
// The caller must call Close() to flush the gzip footer.
func (c *GzipCompressor) Wrap(dst io.Writer) (io.WriteCloser, error) {
	level := c.level
	if level == 0 {
		level = gzip.DefaultCompression
	}
	return gzip.NewWriterLevel(dst, level)
}
