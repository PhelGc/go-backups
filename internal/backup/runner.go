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

// Runner orchestrates a single backup job: loops over all databases,
// handles retries with exponential backoff, and sends webhook notifications.
type Runner struct {
	job    config.JobConfig
	logger *slog.Logger
}

// NewRunner creates a Runner for the given job configuration.
func NewRunner(job config.JobConfig, logger *slog.Logger) *Runner {
	return &Runner{job: job, logger: logger}
}

// Run elige automaticamente el modo segun la config:
//   - HTTP con stage_path: dump a local primero, luego upload con reintentos
//   - Cualquier otro caso: streaming directo con reintentos completos
func (r *Runner) Run(ctx context.Context) error {
	if r.job.Storage.Kind == "http" &&
		r.job.Storage.HTTP != nil &&
		r.job.Storage.HTTP.StagePath != "" {
		return r.runStaged(ctx)
	}
	return r.runStreaming(ctx)
}

// runStreaming: pipeline directo dump -> compress -> destino para cada DB.
func (r *Runner) runStreaming(ctx context.Context) error {
	startedAt := time.Now()
	result := notify.Result{JobName: r.job.Name, StartedAt: startedAt}

	maxAttempts := r.job.Retry.MaxAttempts
	if maxAttempts < 1 {
		maxAttempts = 1
	}

	var lastErr error
	for _, dbName := range r.job.Database.DatabaseList() {
		var dbErr error
		for attempt := 1; attempt <= maxAttempts; attempt++ {
			if attempt > 1 {
				if err := r.waitBackoff(ctx, attempt); err != nil {
					dbErr = err
					break
				}
			}
			r.logger.Info("starting backup",
				"job", r.job.Name, "db", dbName, "attempt", attempt)

			bytes, filename, err := r.runOneDB(ctx, dbName)
			if err == nil {
				result.Databases = append(result.Databases, notify.DBResult{
					Database: dbName, File: filename, Bytes: bytes,
				})
				result.TotalBytes += bytes
				r.logger.Info("db backup completed",
					"job", r.job.Name, "db", dbName,
					"file", filename, "bytes", bytes)
				dbErr = nil
				break
			}
			dbErr = err
			r.logger.Error("db backup attempt failed",
				"job", r.job.Name, "db", dbName,
				"attempt", attempt, "max_attempts", maxAttempts, "error", err)
		}

		if dbErr != nil {
			result.Databases = append(result.Databases, notify.DBResult{
				Database: dbName, Error: dbErr.Error(),
			})
			lastErr = dbErr
		}
	}

	if lastErr != nil {
		return r.failure(ctx, &result, startedAt, lastErr)
	}
	return r.success(ctx, &result, startedAt)
}

// runStaged: para HTTP con stage_path.
// Fase 1 (una vez por DB): dump a archivo local.
// Fase 2 (con reintentos por DB): upload del archivo local.
func (r *Runner) runStaged(ctx context.Context) error {
	startedAt := time.Now()
	result := notify.Result{JobName: r.job.Name, StartedAt: startedAt}
	stageCfg := r.job.Storage.HTTP
	maxAttempts := r.job.Retry.MaxAttempts
	if maxAttempts < 1 {
		maxAttempts = 1
	}

	var lastErr error
	for _, dbName := range r.job.Database.DatabaseList() {
		// Fase 1: dump a local
		r.logger.Info("staged: fase 1 dump",
			"job", r.job.Name, "db", dbName, "stage_path", stageCfg.StagePath)

		localStore, _ := storage.New(config.StorageConfig{
			Kind:  "local",
			Local: &config.LocalConfig{Path: stageCfg.StagePath},
		})
		dumper := database.NewMySQL(r.buildDBConfig(dbName))
		compressor, err := compress.New(r.job.Compression)
		if err != nil {
			e := fmt.Errorf("db %s: build compressor: %w", dbName, err)
			result.Databases = append(result.Databases, notify.DBResult{
				Database: dbName, Error: e.Error()})
			lastErr = e
			continue
		}

		pipeline := NewPipeline(r.job.Name, dbName, dumper, compressor, localStore)
		bytes, filename, err := pipeline.Run(ctx)
		if err != nil {
			e := fmt.Errorf("db %s: dump: %w", dbName, err)
			result.Databases = append(result.Databases, notify.DBResult{
				Database: dbName, Error: e.Error()})
			lastErr = e
			continue
		}

		stagedPath := filepath.Join(stageCfg.StagePath, filename)
		r.logger.Info("staged: fase 1 completada",
			"job", r.job.Name, "db", dbName, "file", stagedPath, "bytes", bytes)

		// Fase 2: upload con reintentos
		uploader := storage.NewHTTP(stageCfg)
		var uploadErr error
		for attempt := 1; attempt <= maxAttempts; attempt++ {
			if attempt > 1 {
				if err := r.waitBackoff(ctx, attempt); err != nil {
					uploadErr = err
					break
				}
			}
			r.logger.Info("staged: fase 2 upload",
				"job", r.job.Name, "db", dbName,
				"attempt", attempt, "url", stageCfg.URL)

			f, err := os.Open(stagedPath)
			if err != nil {
				uploadErr = fmt.Errorf("abrir archivo staged: %w", err)
				break
			}
			uploadErr = uploader.Store(ctx, filename, f)
			f.Close()

			if uploadErr == nil {
				os.Remove(stagedPath)
				break
			}
			r.logger.Error("upload fallido",
				"job", r.job.Name, "db", dbName,
				"attempt", attempt, "error", uploadErr)
		}

		if uploadErr != nil {
			r.logger.Warn("archivo staged conservado", "path", stagedPath)
			e := fmt.Errorf("db %s: upload: %w", dbName, uploadErr)
			result.Databases = append(result.Databases, notify.DBResult{
				Database: dbName, File: filename, Bytes: bytes,
				Error: uploadErr.Error()})
			lastErr = e
		} else {
			result.Databases = append(result.Databases, notify.DBResult{
				Database: dbName, File: filename, Bytes: bytes})
			result.TotalBytes += bytes
		}
	}

	if lastErr != nil {
		return r.failure(ctx, &result, startedAt, lastErr)
	}
	return r.success(ctx, &result, startedAt)
}

// runOneDB ejecuta un pipeline de streaming para una sola base de datos.
func (r *Runner) runOneDB(ctx context.Context, dbName string) (int64, string, error) {
	dumper := database.NewMySQL(r.buildDBConfig(dbName))
	compressor, err := compress.New(r.job.Compression)
	if err != nil {
		return 0, "", fmt.Errorf("build compressor: %w", err)
	}
	store, err := storage.New(r.job.Storage)
	if err != nil {
		return 0, "", fmt.Errorf("build storage: %w", err)
	}
	pipeline := NewPipeline(r.job.Name, dbName, dumper, compressor, store)
	return pipeline.Run(ctx)
}

// buildDBConfig devuelve una copia del DBConfig con una sola base de datos.
func (r *Runner) buildDBConfig(dbName string) config.DBConfig {
	cfg := r.job.Database
	cfg.Database = dbName
	cfg.Databases = nil
	return cfg
}

func (r *Runner) success(ctx context.Context, result *notify.Result, startedAt time.Time) error {
	result.Status = "success"
	result.FinishedAt = time.Now()
	result.DurationMs = result.FinishedAt.Sub(startedAt).Milliseconds()
	r.logger.Info("job completed",
		"job", r.job.Name,
		"databases", len(result.Databases),
		"total_bytes", result.TotalBytes,
		"duration_ms", result.DurationMs)
	r.sendNotification(ctx, *result)
	return nil
}

func (r *Runner) failure(ctx context.Context, result *notify.Result,
	startedAt time.Time, lastErr error) error {

	result.Status = "failure"
	result.Error = lastErr.Error()
	result.FinishedAt = time.Now()
	result.DurationMs = result.FinishedAt.Sub(startedAt).Milliseconds()
	r.sendNotification(ctx, *result)
	return fmt.Errorf("backup job %q failed: %w", r.job.Name, lastErr)
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
