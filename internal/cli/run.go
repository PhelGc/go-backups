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
)

func newRunCmd() *cobra.Command {
	var configPath string
	var jobName string
	var dryRun bool

	cmd := &cobra.Command{
		Use:   "run",
		Short: "Execute backup job(s) immediately",
		Long: `Execute one or all backup jobs defined in the config file.
Without --job, all jobs are executed sequentially.
Use --dry-run to preview actions without executing mysqldump.`,
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

			jobs := cfg.Jobs
			if jobName != "" {
				jobs = nil
				for _, j := range cfg.Jobs {
					if j.Name == jobName {
						jobs = []config.JobConfig{j}
						break
					}
				}
				if jobs == nil {
					return fmt.Errorf("job %q not found in config", jobName)
				}
			}

			var failed bool
			for _, job := range jobs {
				if dryRun {
					logger.Info("dry run: would execute backup",
						"job", job.Name,
						"db_host", job.Database.Host,
						"db_port", job.Database.Port,
						"db_name", job.Database.Database,
						"compression", job.Compression.Kind,
						"storage", job.Storage.Kind,
					)
					continue
				}

				runner := backup.NewRunner(job, logger)
				if err := runner.Run(ctx); err != nil {
					logger.Error("job failed", "job", job.Name, "error", err)
					failed = true
				}
			}

			if failed {
				os.Exit(1)
			}
			return nil
		},
	}

	cmd.Flags().StringVarP(&configPath, "config", "c", "config.yaml", "Path to config file")
	cmd.Flags().StringVarP(&jobName, "job", "j", "", "Run only the job with this name (default: all jobs)")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "Print what would be done without executing")

	return cmd
}
