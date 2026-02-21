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
	dbName     string // included in filename: jobname_dbname_timestamp.sql.gz
	dumper     database.Dumper
	compressor compress.Compressor
	store      storage.Writer
}

// NewPipeline creates a Pipeline for the given job components.
func NewPipeline(jobName, dbName string, dumper database.Dumper, compressor compress.Compressor, store storage.Writer) *Pipeline {
	return &Pipeline{
		jobName:    jobName,
		dbName:     dbName,
		dumper:     dumper,
		compressor: compressor,
		store:      store,
	}
}

// Run executes the backup pipeline.
// Returns compressed bytes written, filename used, and any error.
func (p *Pipeline) Run(ctx context.Context) (bytesWritten int64, filename string, err error) {
	filename = p.buildFilename()

	dumpReader, err := p.dumper.Dump(ctx)
	if err != nil {
		return 0, "", fmt.Errorf("start dump: %w", err)
	}

	pr, pw := io.Pipe()

	compressErrCh := make(chan error, 1)
	go func() {
		defer dumpReader.Close()

		cw, err := p.compressor.Wrap(pw)
		if err != nil {
			pw.CloseWithError(fmt.Errorf("init compressor: %w", err))
			compressErrCh <- err
			return
		}

		_, copyErr := io.Copy(cw, dumpReader)
		closeErr := cw.Close()

		combined := copyErr
		if combined == nil {
			combined = closeErr
		}
		pw.CloseWithError(combined)
		compressErrCh <- combined
	}()

	cr := &countingReader{r: pr}
	storeErr := p.store.Store(ctx, filename, cr)

	if storeErr != nil {
		pr.CloseWithError(storeErr)
	}

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
	return fmt.Sprintf("%s_%s_%s%s%s",
		p.jobName,
		p.dbName,
		ts,
		p.dumper.FileExtension(),
		p.compressor.FileExtension(),
	)
}

type countingReader struct {
	r io.Reader
	n int64
}

func (c *countingReader) Read(p []byte) (n int, err error) {
	n, err = c.r.Read(p)
	c.n += int64(n)
	return
}
