package compress

import (
	"io"

	"github.com/klauspost/compress/zstd"
)

// ZstdCompressor compresses data using Zstandard.
type ZstdCompressor struct {
	level zstd.EncoderLevel
}

// NewZstd creates a ZstdCompressor. The level maps roughly as follows:
//   - 0 or negative: SpeedDefault (level 3 equivalent, good balance)
//   - 1: SpeedFastest (level 1, best for interactive/large backups)
//   - 2-4: SpeedDefault
//   - 5-7: SpeedBetterCompression
//   - 8+: SpeedBestCompression
func NewZstd(level int) *ZstdCompressor {
	var encLevel zstd.EncoderLevel
	switch {
	case level <= 0:
		encLevel = zstd.SpeedDefault
	case level == 1:
		encLevel = zstd.SpeedFastest
	case level <= 4:
		encLevel = zstd.SpeedDefault
	case level <= 7:
		encLevel = zstd.SpeedBetterCompression
	default:
		encLevel = zstd.SpeedBestCompression
	}
	return &ZstdCompressor{level: encLevel}
}

// FileExtension returns ".zst".
func (c *ZstdCompressor) FileExtension() string {
	return ".zst"
}

// Wrap returns a WriteCloser that zstd-compresses writes to dst.
// The caller must call Close() to flush the zstd frame footer.
func (c *ZstdCompressor) Wrap(dst io.Writer) (io.WriteCloser, error) {
	return zstd.NewWriter(dst, zstd.WithEncoderLevel(c.level))
}
