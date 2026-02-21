package cli

import (
	"log/slog"
	"os"

	"github.com/spf13/cobra"
)

// logger is the shared structured logger for all CLI commands.
// Initialized by the PersistentPreRunE on the root command.
var logger *slog.Logger

// NewRootCmd builds and returns the root cobra command with all subcommands.
func NewRootCmd() *cobra.Command {
	var logLevel string
	var logFormat string

	root := &cobra.Command{
		Use:   "gobackups",
		Short: "Database backup tool for MySQL/MariaDB",
		Long: `gobackups takes consistent, non-locking backups of MySQL/MariaDB databases.
Supports gzip and zstd compression, local and HTTP storage destinations,
webhook notifications, and cron-based scheduling.`,
		SilenceUsage: true,
		PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
			logger = buildLogger(logLevel, logFormat)
			return nil
		},
	}

	root.PersistentFlags().StringVar(&logLevel, "log-level", "info",
		"Log level: debug, info, warn, error")
	root.PersistentFlags().StringVar(&logFormat, "log-format", "text",
		"Log format: text, json")

	root.AddCommand(newRunCmd())
	root.AddCommand(newValidateCmd())
	root.AddCommand(newListCmd())
	root.AddCommand(newDaemonCmd())

	return root
}

func buildLogger(level, format string) *slog.Logger {
	var lvl slog.Level
	switch level {
	case "debug":
		lvl = slog.LevelDebug
	case "warn":
		lvl = slog.LevelWarn
	case "error":
		lvl = slog.LevelError
	default:
		lvl = slog.LevelInfo
	}

	opts := &slog.HandlerOptions{Level: lvl}
	var handler slog.Handler
	if format == "json" {
		handler = slog.NewJSONHandler(os.Stderr, opts)
	} else {
		handler = slog.NewTextHandler(os.Stderr, opts)
	}
	return slog.New(handler)
}
