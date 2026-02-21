package backup

import (
	"context"
	"fmt"
	"log/slog"
	"math"
	"os"
	"path/filepath"
	"time"

	"gobackups/internal/compress"
	"gobackups/internal/config"
	"gobackups/internal/database"
	"gobackups/internal/notify"
	"gobackups/internal/storage"
)

// Runner orchestrates a single backup job: builds the pipeline, handles
// retries with exponential backoff, and sends webhook notifications.
type Runner struct {
	job    config.JobConfig
	logger *slog.Logger
}

// NewRunner creates a Runner for the given job configuration.
func NewRunner(job config.JobConfig, logger *slog.Logger) *Runner {
	return &Runner{job: job, logger: logger}
}

// Run elige automaticamente el modo segun la config:
//
//   - HTTP con stage_path: fase 1 = dump a archivo local (una vez),
//     fase 2 = upload con reintentos. Si la conexion falla NO vuelve
//     a correr mysqldump.
//
//   - Cualquier otro caso: streaming directo con reintentos completos.
func (r *Runner) Run(ctx context.Context) error {
	if r.job.Storage.Kind == "http" &&
		r.job.Storage.HTTP != nil &&
		r.job.Storage.HTTP.StagePath != "" {
		return r.runStaged(ctx)
	}
	return r.runStreaming(ctx)
}

// runStreaming: pipeline directo dump -> compress -> destino.
// Cada reintento vuelve a correr mysqldump desde cero.
func (r *Runner) runStreaming(ctx context.Context) error {
	startedAt := time.Now()
	result := notify.Result{JobName: r.job.Name, StartedAt: startedAt}

	maxAttempts := r.job.Retry.MaxAttempts
	if maxAttempts < 1 {
		maxAttempts = 1
	}

	var lastErr error
	for attempt := 1; attempt <= maxAttempts; attempt++ {
		if attempt > 1 {
			if err := r.waitBackoff(ctx, attempt); err != nil {
				lastErr = err
				break
			}
		}
		r.logger.Info("starting backup", "job", r.job.Name, "attempt", attempt)
		bytes, dest, err := r.runOnce(ctx)
		if err == nil {
			return r.success(ctx, &result, startedAt, bytes, dest)
		}
		lastErr = err
		r.logger.Error("backup attempt failed",
			"job", r.job.Name, "attempt", attempt,
			"max_attempts", maxAttempts, "error", err)
	}
	return r.failure(ctx, &result, startedAt, maxAttempts, lastErr)
}

// runStaged: dos fases independientes.
//
//	Fase 1 (una vez): mysqldump -> compress -> archivo local en stage_path
//	Fase 2 (con reintentos): leer archivo local -> HTTP upload
//
// El archivo staged se elimina al subir con exito. Si el upload falla
// definitivamente, el archivo queda en stage_path para reintento manual.
func (r *Runner) runStaged(ctx context.Context) error {
	startedAt := time.Now()
	result := notify.Result{JobName: r.job.Name, StartedAt: startedAt}
	stageCfg := r.job.Storage.HTTP

	// --- Fase 1: dump a archivo local ---
	r.logger.Info("staged backup: fase 1 dump a local",
		"job", r.job.Name, "stage_path", stageCfg.StagePath)

	localStore, _ := storage.New(config.StorageConfig{
		Kind:  "local",
		Local: &config.LocalConfig{Path: stageCfg.StagePath},
	})

	dumper := database.NewMySQL(r.job.Database)
	compressor, err := compress.New(r.job.Compression)
	if err != nil {
		return fmt.Errorf("build compressor: %w", err)
	}

	pipeline := NewPipeline(r.job.Name, dumper, compressor, localStore)
	bytes, filename, err := pipeline.Run(ctx)
	if err != nil {
		return r.failure(ctx, &result, startedAt, 1,
			fmt.Errorf("fase 1 (dump): %w", err))
	}

	stagedPath := filepath.Join(stageCfg.StagePath, filename)
	r.logger.Info("staged backup: fase 1 completada",
		"job", r.job.Name, "file", stagedPath, "bytes", bytes)

	// --- Fase 2: upload con reintentos (sin re-correr mysqldump) ---
	maxAttempts := r.job.Retry.MaxAttempts
	if maxAttempts < 1 {
		maxAttempts = 1
	}

	uploader := storage.NewHTTP(stageCfg)
	var uploadErr error

	for attempt := 1; attempt <= maxAttempts; attempt++ {
		if attempt > 1 {
			if err := r.waitBackoff(ctx, attempt); err != nil {
				uploadErr = err
				break
			}
		}

		r.logger.Info("staged backup: fase 2 upload",
			"job", r.job.Name, "attempt", attempt, "url", stageCfg.URL)

		f, err := os.Open(stagedPath)
		if err != nil {
			uploadErr = fmt.Errorf("abrir archivo staged: %w", err)
			break
		}

		uploadErr = uploader.Store(ctx, filename, f)
		f.Close()

		if uploadErr == nil {
			if err := os.Remove(stagedPath); err != nil {
				r.logger.Warn("no se pudo eliminar archivo staged",
					"path", stagedPath, "error", err)
			}
			return r.success(ctx, &result, startedAt, bytes, stageCfg.URL+"/"+filename)
		}

		r.logger.Error("upload fallido",
			"job", r.job.Name, "attempt", attempt,
			"max_attempts", maxAttempts, "error", uploadErr)
	}

	r.logger.Warn("archivo staged conservado para reintento manual",
		"path", stagedPath)
	return r.failure(ctx, &result, startedAt, maxAttempts,
		fmt.Errorf("fase 2 (upload): %w", uploadErr))
}

// runOnce ejecuta un solo intento de pipeline streaming.
func (r *Runner) runOnce(ctx context.Context) (int64, string, error) {
	dumper := database.NewMySQL(r.job.Database)
	compressor, err := compress.New(r.job.Compression)
	if err != nil {
		return 0, "", fmt.Errorf("build compressor: %w", err)
	}
	store, err := storage.New(r.job.Storage)
	if err != nil {
		return 0, "", fmt.Errorf("build storage: %w", err)
	}
	pipeline := NewPipeline(r.job.Name, dumper, compressor, store)
	return pipeline.Run(ctx)
}

func (r *Runner) success(ctx context.Context, result *notify.Result,
	startedAt time.Time, bytes int64, dest string) error {

	result.Status = "success"
	result.BytesWritten = bytes
	result.Destination = dest
	result.FinishedAt = time.Now()
	result.DurationMs = result.FinishedAt.Sub(startedAt).Milliseconds()
	r.logger.Info("backup completed",
		"job", r.job.Name, "bytes", bytes,
		"destination", dest, "duration_ms", result.DurationMs)
	r.sendNotification(ctx, *result)
	return nil
}

func (r *Runner) failure(ctx context.Context, result *notify.Result,
	startedAt time.Time, attempts int, lastErr error) error {

	result.Status = "failure"
	result.Error = lastErr.Error()
	result.FinishedAt = time.Now()
	result.DurationMs = result.FinishedAt.Sub(startedAt).Milliseconds()
	r.sendNotification(ctx, *result)
	return fmt.Errorf("backup %q failed after %d attempt(s): %w",
		r.job.Name, attempts, lastErr)
}

func (r *Runner) waitBackoff(ctx context.Context, attempt int) error {
	delay := backoffDelay(r.job.Retry.DelaySeconds, attempt-1)
	r.logger.Info("retrying", "job", r.job.Name, "attempt", attempt, "delay", delay)
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-time.After(delay):
		return nil
	}
}

func (r *Runner) sendNotification(ctx context.Context, result notify.Result) {
	if r.job.Notify == nil {
		return
	}
	if err := notify.NewWebhook(r.job.Notify).Notify(ctx, result); err != nil {
		r.logger.Warn("webhook notification failed",
			"job", r.job.Name, "error", err)
	}
}

// backoffDelay: base * 2^(attempt-1), maximo 10 minutos.
func backoffDelay(baseSeconds, attempt int) time.Duration {
	if baseSeconds <= 0 {
		baseSeconds = 5
	}
	seconds := float64(baseSeconds) * math.Pow(2, float64(attempt-1))
	if seconds > 600 {
		seconds = 600
	}
	return time.Duration(seconds) * time.Second
}
