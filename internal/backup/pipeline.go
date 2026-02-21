package backup

import (
	"context"
	"fmt"
	"io"
	"time"

	"gobackups/internal/compress"
	"gobackups/internal/database"
	"gobackups/internal/storage"
)

// Pipeline wires a dump source through a compressor to a storage destination.
// Data flows as a stream: mysqldump stdout -> compressor -> storage.
// No full-file buffering occurs at any stage.
type Pipeline struct {
	jobName    string
	dumper     database.Dumper
	compressor compress.Compressor
	store      storage.Writer
}

// NewPipeline creates a Pipeline for the given job components.
func NewPipeline(jobName string, dumper database.Dumper, compressor compress.Compressor, store storage.Writer) *Pipeline {
	return &Pipeline{
		jobName:    jobName,
		dumper:     dumper,
		compressor: compressor,
		store:      store,
	}
}

// Run executes the backup pipeline.
// Returns the number of compressed bytes written to storage, the filename
// used, and any error.
//
// Concurrency model:
//
//	goroutine A: reads from mysqldump stdout, compresses, writes to pw (io.PipeWriter)
//	main goroutine: storage.Store drains pr (io.PipeReader) and persists bytes
//
// Back-pressure is automatic via io.Pipe: if storage is slow, the pipe blocks
// the compressor which blocks mysqldump. Memory usage stays constant regardless
// of database size.
func (p *Pipeline) Run(ctx context.Context) (bytesWritten int64, filename string, err error) {
	filename = p.buildFilename()

	dumpReader, err := p.dumper.Dump(ctx)
	if err != nil {
		return 0, "", fmt.Errorf("start dump: %w", err)
	}

	// pr/pw bridge the compressor (writer side) and storage (reader side).
	pr, pw := io.Pipe()

	// Goroutine A: read from dump -> compress -> write to pw.
	compressErrCh := make(chan error, 1)
	go func() {
		// Always close the subprocess reader when done.
		defer dumpReader.Close()

		cw, err := p.compressor.Wrap(pw)
		if err != nil {
			pw.CloseWithError(fmt.Errorf("init compressor: %w", err))
			compressErrCh <- err
			return
		}

		_, copyErr := io.Copy(cw, dumpReader)
		// Always attempt to flush the compressor's internal buffers.
		closeErr := cw.Close()

		combined := copyErr
		if combined == nil {
			combined = closeErr
		}
		// Closing pw with nil causes a clean EOF on pr; an error propagates.
		pw.CloseWithError(combined)
		compressErrCh <- combined
	}()

	// Main goroutine: storage drains pr.
	cr := &countingReader{r: pr}
	storeErr := p.store.Store(ctx, filename, cr)

	if storeErr != nil {
		// Signal goroutine A to stop writing (next pw.Write returns storeErr).
		pr.CloseWithError(storeErr)
	}

	// Always wait for goroutine A to finish before returning.
	compressErr := <-compressErrCh

	if storeErr != nil {
		return 0, "", fmt.Errorf("store: %w", storeErr)
	}
	if compressErr != nil {
		return 0, "", fmt.Errorf("compress: %w", compressErr)
	}

	return cr.n, filename, nil
}

func (p *Pipeline) buildFilename() string {
	ts := time.Now().UTC().Format("20060102T150405Z")
	return fmt.Sprintf("%s_%s%s%s",
		p.jobName,
		ts,
		p.dumper.FileExtension(),
		p.compressor.FileExtension(),
	)
}

// countingReader wraps a reader and counts the bytes passing through.
type countingReader struct {
	r io.Reader
	n int64
}

func (c *countingReader) Read(p []byte) (n int, err error) {
	n, err = c.r.Read(p)
	c.n += int64(n)
	return
}
