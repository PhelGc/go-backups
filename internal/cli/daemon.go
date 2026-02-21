package cli

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/spf13/cobra"

	"gobackups/internal/backup"
	"gobackups/internal/config"
	"gobackups/internal/scheduler"
)

func newDaemonCmd() *cobra.Command {
	var configPath string

	cmd := &cobra.Command{
		Use:   "daemon",
		Short: "Run backup jobs on their configured cron schedules",
		Long: `Start a long-running process that executes backup jobs at the times
defined by their 'schedule' field (standard 5-field cron syntax).
Jobs with an empty schedule are ignored in daemon mode; use 'run' for those.
Stops gracefully on SIGINT or SIGTERM, waiting for any running jobs to finish.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := config.Load(configPath)
			if err != nil {
				return fmt.Errorf("load config: %w", err)
			}
			if err := config.Validate(cfg); err != nil {
				return fmt.Errorf("invalid config: %w", err)
			}

			ctx, cancel := signal.NotifyContext(context.Background(),
				os.Interrupt, syscall.SIGTERM)
			defer cancel()

			sched := scheduler.New(logger)
			scheduled := 0

			for _, job := range cfg.Jobs {
				if job.Schedule == "" {
					logger.Debug("skipping job with no schedule", "job", job.Name)
					continue
				}

				j := job // capture loop variable for the closure
				if err := sched.Add(j.Schedule, func() {
					runner := backup.NewRunner(j, logger)
					if err := runner.Run(ctx); err != nil {
						logger.Error("scheduled job failed", "job", j.Name, "error", err)
					}
				}); err != nil {
					return fmt.Errorf("register job %q with schedule %q: %w",
						job.Name, job.Schedule, err)
				}

				logger.Info("registered job",
					"job", job.Name,
					"schedule", job.Schedule,
				)
				scheduled++
			}

			if scheduled == 0 {
				return fmt.Errorf("no jobs with a schedule found in config; add a 'schedule' field or use 'run' instead")
			}

			sched.Start()
			logger.Info("daemon started", "scheduled_jobs", scheduled)

			<-ctx.Done()
			logger.Info("shutdown signal received, draining in-progress jobs")
			sched.Stop()
			return nil
		},
	}

	cmd.Flags().StringVarP(&configPath, "config", "c", "config.yaml", "Path to config file")
	return cmd
}
