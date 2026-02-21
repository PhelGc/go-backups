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

// Result carries the outcome of a single backup job execution.
type Result struct {
	JobName      string    `json:"job_name"`
	Status       string    `json:"status"`        // "success" or "failure"
	StartedAt    time.Time `json:"started_at"`
	FinishedAt   time.Time `json:"finished_at"`
	DurationMs   int64     `json:"duration_ms"`
	BytesWritten int64     `json:"bytes_written"`
	Destination  string    `json:"destination"`
	Error        string    `json:"error,omitempty"`
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
	io.Copy(io.Discard, resp.Body) // drain to allow connection reuse

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("webhook returned status %d", resp.StatusCode)
	}
	return nil
}
