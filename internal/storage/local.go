package storage

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"gobackups/internal/config"
)

// LocalStorage writes backup files to the local filesystem.
// Writes are atomic: data goes to a temp file which is then renamed
// to the final path. A crash mid-write leaves a .tmp file, never a
// corrupt final file.
type LocalStorage struct {
	path string
}

// NewLocal creates a LocalStorage that writes files into dir.
func NewLocal(cfg *config.LocalConfig) *LocalStorage {
	return &LocalStorage{path: cfg.Path}
}

// Store reads all bytes from src and writes them atomically to <path>/<filename>.
func (s *LocalStorage) Store(_ context.Context, filename string, src io.Reader) error {
	if err := os.MkdirAll(s.path, 0750); err != nil {
		return fmt.Errorf("create storage directory %q: %w", s.path, err)
	}

	finalPath := filepath.Join(s.path, filename)
	tmpPath := fmt.Sprintf("%s.tmp.%d", finalPath, os.Getpid())

	f, err := os.OpenFile(tmpPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0640)
	if err != nil {
		return fmt.Errorf("create temp file: %w", err)
	}

	if _, err := io.Copy(f, src); err != nil {
		f.Close()
		os.Remove(tmpPath)
		return fmt.Errorf("write backup data: %w", err)
	}

	if err := f.Close(); err != nil {
		os.Remove(tmpPath)
		return fmt.Errorf("close temp file: %w", err)
	}

	if err := os.Rename(tmpPath, finalPath); err != nil {
		os.Remove(tmpPath)
		return fmt.Errorf("rename to final path: %w", err)
	}

	return nil
}
