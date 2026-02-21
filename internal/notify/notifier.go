package notify

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"gobackups/internal/config"
)

// DBResult es el resultado del backup de una base de datos individual.
type DBResult struct {
	Database string `json:"database"`
	File     string `json:"file"`
	Bytes    int64  `json:"bytes"`
	Error    string `json:"error,omitempty"`
}

// Result carries the outcome of a single backup job execution.
type Result struct {
	JobName      string     `json:"job_name"`
	Status       string     `json:"status"`         // "success" o "failure"
	StartedAt    time.Time  `json:"started_at"`
	FinishedAt   time.Time  `json:"finished_at"`
	DurationMs   int64      `json:"duration_ms"`
	TotalBytes   int64      `json:"total_bytes"`
	Databases    []DBResult `json:"databases"`       // detalle por DB
	Error        string     `json:"error,omitempty"` // error global si aplica
}

// Notifier sends a notification after a backup completes or fails.
type Notifier interface {
	Notify(ctx context.Context, result Result) error
}

// WebhookNotifier sends a JSON POST to a configured webhook URL.
type WebhookNotifier struct {
	cfg    *config.NotifyConfig
	client *http.Client
}

// NewWebhook creates a WebhookNotifier from the given config.
func NewWebhook(cfg *config.NotifyConfig) *WebhookNotifier {
	return &WebhookNotifier{
		cfg:    cfg,
		client: &http.Client{Timeout: 30 * time.Second},
	}
}

// Notify sends the backup result as a JSON POST to the configured webhook URL.
func (n *WebhookNotifier) Notify(ctx context.Context, result Result) error {
	body, err := json.Marshal(result)
	if err != nil {
		return fmt.Errorf("marshal result: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, n.cfg.WebhookURL, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("build webhook request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	for k, v := range n.cfg.Headers {
		req.Header.Set(k, v)
	}

	resp, err := n.client.Do(req)
	if err != nil {
		return fmt.Errorf("send webhook: %w", err)
	}
	defer resp.Body.Close()
	io.Copy(io.Discard, resp.Body)

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("webhook returned status %d", resp.StatusCode)
	}
	return nil
}
