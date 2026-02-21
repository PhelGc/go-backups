package storage

import (
	"context"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"time"

	"gobackups/internal/config"
)

// HTTPStorage uploads backup files via HTTP multipart POST to a remote server.
// The upload streams directly from the source reader without buffering the
// full content in memory, making it safe for databases up to 100 GB.
type HTTPStorage struct {
	cfg    *config.HTTPConfig
	client *http.Client
}

// NewHTTP creates an HTTPStorage from the given config.
func NewHTTP(cfg *config.HTTPConfig) *HTTPStorage {
	timeout := time.Duration(cfg.TimeoutSeconds) * time.Second
	if timeout == 0 {
		timeout = time.Hour // default: 1 hour for large databases
	}
	return &HTTPStorage{
		cfg:    cfg,
		client: &http.Client{Timeout: timeout},
	}
}

// Store uploads src as a multipart file field to the configured HTTP endpoint.
//
// Pipeline:
//
//	src (compressed bytes)
//	  -> goroutine: multipart.Writer writes to bodyPw
//	  -> io.Pipe (bodyPw -> bodyPr)
//	  -> HTTP request body read by the HTTP client
//
// This means the bytes flow from src through multipart encoding directly
// into the HTTP request stream without any intermediate full-file buffer.
func (s *HTTPStorage) Store(ctx context.Context, filename string, src io.Reader) error {
	bodyPr, bodyPw := io.Pipe()
	mw := multipart.NewWriter(bodyPw)

	uploadErrCh := make(chan error, 1)
	go func() {
		part, err := mw.CreateFormFile(s.cfg.FieldName, filename)
		if err != nil {
			mw.Close()
			bodyPw.CloseWithError(fmt.Errorf("create form file part: %w", err))
			uploadErrCh <- err
			return
		}

		_, copyErr := io.Copy(part, src)
		closeErr := mw.Close()

		combined := copyErr
		if combined == nil {
			combined = closeErr
		}
		bodyPw.CloseWithError(combined) // nil causes a clean EOF on bodyPr
		uploadErrCh <- combined
	}()

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, s.cfg.URL, bodyPr)
	if err != nil {
		bodyPr.CloseWithError(err)
		<-uploadErrCh
		return fmt.Errorf("build upload request: %w", err)
	}
	req.Header.Set("Content-Type", mw.FormDataContentType())
	for k, v := range s.cfg.Headers {
		req.Header.Set(k, v)
	}

	resp, doErr := s.client.Do(req)
	if doErr != nil {
		// Unblock the upload goroutine so it can exit cleanly.
		bodyPr.CloseWithError(doErr)
		<-uploadErrCh
		return fmt.Errorf("http upload: %w", doErr)
	}
	defer resp.Body.Close()
	io.Copy(io.Discard, resp.Body) // drain to allow connection reuse

	if uploadErr := <-uploadErrCh; uploadErr != nil {
		return fmt.Errorf("write multipart body: %w", uploadErr)
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("http upload: server returned status %d", resp.StatusCode)
	}

	return nil
}
