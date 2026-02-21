package cli

import (
	"fmt"

	"github.com/spf13/cobra"

	"gobackups/internal/config"
)

func newValidateCmd() *cobra.Command {
	var configPath string

	cmd := &cobra.Command{
		Use:   "validate",
		Short: "Validate the configuration file",
		Long:  `Parse the configuration file, expand environment variables, and check all required fields.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := config.Load(configPath)
			if err != nil {
				return fmt.Errorf("load config: %w", err)
			}
			if err := config.Validate(cfg); err != nil {
				return fmt.Errorf("invalid config: %w", err)
			}

			fmt.Printf("Config valid  (version: %s, jobs: %d)\n\n", cfg.Version, len(cfg.Jobs))
			for _, job := range cfg.Jobs {
				schedule := job.Schedule
				if schedule == "" {
					schedule = "(manual only)"
				}
				fmt.Printf("  job: %s\n", job.Name)
				fmt.Printf("    database:    %s@%s:%d/%s\n",
					job.Database.User,
					job.Database.Host,
					job.Database.Port,
					job.Database.Database,
				)
				fmt.Printf("    compression: %s (level %d)\n",
					job.Compression.Kind, job.Compression.Level)
				fmt.Printf("    storage:     %s\n", job.Storage.Kind)
				fmt.Printf("    schedule:    %s\n", schedule)
				fmt.Printf("    retry:       %d attempts, %ds base delay\n\n",
					job.Retry.MaxAttempts, job.Retry.DelaySeconds)
			}
			return nil
		},
	}

	cmd.Flags().StringVarP(&configPath, "config", "c", "config.yaml", "Path to config file")
	return cmd
}
