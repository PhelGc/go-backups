package main

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
	"time"
)

type DBResult struct {
	Database string `json:"database"`
	File     string `json:"file"`
	Bytes    int64  `json:"bytes"`
	Error    string `json:"error,omitempty"`
}

type Result struct {
	JobName    string     `json:"job_name"`
	Status     string     `json:"status"`
	StartedAt  time.Time  `json:"started_at"`
	FinishedAt time.Time  `json:"finished_at"`
	DurationMs int64      `json:"duration_ms"`
	TotalBytes int64      `json:"total_bytes"`
	Databases  []DBResult `json:"databases"`
	Error      string     `json:"error,omitempty"`
}

func main() {
	http.HandleFunc("/webhook", handleWebhook)

	log.Println("========================================")
	log.Println("  Webhook test server escuchando :8081")
	log.Println("  endpoint: POST http://localhost:8081/webhook")
	log.Println("========================================")
	log.Fatal(http.ListenAndServe(":8081", nil))
}

func handleWebhook(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "solo POST", http.StatusMethodNotAllowed)
		return
	}

	body, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, "error leyendo body", http.StatusBadRequest)
		return
	}
	defer r.Body.Close()

	var result Result
	if err := json.Unmarshal(body, &result); err != nil {
		log.Printf("[ERROR] JSON inválido: %v\nBody: %s\n", err, string(body))
		http.Error(w, "json inválido", http.StatusBadRequest)
		return
	}

	printResult(result)
	w.WriteHeader(http.StatusOK)
	w.Write([]byte(`{"status":"received"}`))
}

func printResult(r Result) {
	sep := strings.Repeat("=", 50)
	fmt.Println(sep)

	if r.Status == "success" {
		fmt.Printf("  BACKUP OK  |  job: %s\n", r.JobName)
	} else {
		fmt.Printf("  BACKUP FALLO  |  job: %s\n", r.JobName)
	}

	fmt.Printf("  Inicio:    %s\n", r.StartedAt.Format("2006-01-02 15:04:05"))
	fmt.Printf("  Fin:       %s\n", r.FinishedAt.Format("2006-01-02 15:04:05"))
	fmt.Printf("  Duracion:  %s\n", formatDuration(r.DurationMs))
	fmt.Printf("  Total:     %s\n", formatBytes(r.TotalBytes))

	if r.Error != "" {
		fmt.Printf("  Error:     %s\n", r.Error)
	}

	fmt.Println("  ----------------------------------------")
	fmt.Println("  Bases de datos:")
	for _, db := range r.Databases {
		if db.Error != "" {
			fmt.Printf("    [FALLO] %-30s  %s\n", db.Database, db.Error)
		} else {
			fmt.Printf("    [OK]    %-30s  %s\n", db.Database, formatBytes(db.Bytes))
		}
	}

	fmt.Println(sep)
	fmt.Println()
}

func formatBytes(b int64) string {
	const (
		KB = 1024
		MB = 1024 * KB
		GB = 1024 * MB
	)
	switch {
	case b >= GB:
		return fmt.Sprintf("%.2f GB", float64(b)/GB)
	case b >= MB:
		return fmt.Sprintf("%.2f MB", float64(b)/MB)
	case b >= KB:
		return fmt.Sprintf("%.2f KB", float64(b)/KB)
	default:
		return fmt.Sprintf("%d bytes", b)
	}
}

func formatDuration(ms int64) string {
	d := time.Duration(ms) * time.Millisecond
	if d >= time.Minute {
		return fmt.Sprintf("%dm %ds", int(d.Minutes()), int(d.Seconds())%60)
	}
	return fmt.Sprintf("%.1fs", d.Seconds())
}
