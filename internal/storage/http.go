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

// BackupMeta contiene metadata del backup que se envía como campos extra
// en el multipart form para que el servidor pueda organizar carpetas.
type BackupMeta struct {
	Client   string // nombre del cliente (JobConfig.Client)
	Database string // nombre de la base de datos
	JobName  string // nombre del job
}

// HTTPStorage uploads backup files via HTTP multipart POST to a remote server.
// The upload streams directly from the source reader without buffering the
// full content in memory, making it safe for databases up to 100 GB.
type HTTPStorage struct {
	cfg    *config.HTTPConfig
	meta   BackupMeta
	client *http.Client
}

// NewHTTP creates an HTTPStorage from the given config (sin metadata).
func NewHTTP(cfg *config.HTTPConfig) *HTTPStorage {
	return newHTTPStorage(cfg, BackupMeta{})
}

// NewHTTPWithMeta creates an HTTPStorage that incluirá campos de metadata
// (client, database, job_name) como campos extra en el multipart form.
// El servidor puede usar estos campos para crear carpetas por cliente/BBDD.
func NewHTTPWithMeta(cfg *config.HTTPConfig, meta BackupMeta) *HTTPStorage {
	return newHTTPStorage(cfg, meta)
}

func newHTTPStorage(cfg *config.HTTPConfig, meta BackupMeta) *HTTPStorage {
	timeout := time.Duration(cfg.TimeoutSeconds) * time.Second
	if timeout == 0 {
		timeout = time.Hour // default: 1 hour for large databases
	}
	return &HTTPStorage{
		cfg:    cfg,
		meta:   meta,
		client: &http.Client{Timeout: timeout},
	}
}

// Store uploads src as a multipart file field to the configured HTTP endpoint.
// Si hay metadata configurada, también escribe campos extra (client, database,
// job_name) antes del archivo para que el servidor pueda organizar carpetas.
//
// Pipeline:
//
//	src (compressed bytes)
//	  -> goroutine: multipart.Writer escribe campos meta + archivo a bodyPw
//	  -> io.Pipe (bodyPw -> bodyPr)
//	  -> HTTP request body leído por el HTTP client
//
// Los bytes fluyen desde src a través del encoding multipart directamente
// al stream HTTP sin ningún buffer intermedio del archivo completo.
func (s *HTTPStorage) Store(ctx context.Context, filename string, src io.Reader) error {
	bodyPr, bodyPw := io.Pipe()
	mw := multipart.NewWriter(bodyPw)

	uploadErrCh := make(chan error, 1)
	go func() {
		// Escribir campos de metadata extra si están configurados
		if s.meta.Client != "" {
			if err := mw.WriteField("client", s.meta.Client); err != nil {
				mw.Close()
				bodyPw.CloseWithError(fmt.Errorf("write client field: %w", err))
				uploadErrCh <- err
				return
			}
		}
		if s.meta.Database != "" {
			if err := mw.WriteField("database", s.meta.Database); err != nil {
				mw.Close()
				bodyPw.CloseWithError(fmt.Errorf("write database field: %w", err))
				uploadErrCh <- err
				return
			}
		}
		if s.meta.JobName != "" {
			if err := mw.WriteField("job_name", s.meta.JobName); err != nil {
				mw.Close()
				bodyPw.CloseWithError(fmt.Errorf("write job_name field: %w", err))
				uploadErrCh <- err
				return
			}
		}

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
