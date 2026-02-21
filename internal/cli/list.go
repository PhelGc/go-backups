package cli

import (
	"fmt"
	"os"
	"text/tabwriter"

	"github.com/spf13/cobra"

	"gobackups/internal/config"
)

func newListCmd() *cobra.Command {
	var configPath string

	cmd := &cobra.Command{
		Use:   "list",
		Short: "List all configured backup jobs",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := config.Load(configPath)
			if err != nil {
				return fmt.Errorf("load config: %w", err)
			}

			w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
			fmt.Fprintln(w, "NAME\tDATABASE\tHOST\tCOMPRESSION\tSTORAGE\tSCHEDULE")
			fmt.Fprintln(w, "----\t--------\t----\t-----------\t-------\t--------")
			for _, job := range cfg.Jobs {
				schedule := job.Schedule
				if schedule == "" {
					schedule = "-"
				}
				fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\t%s\n",
					job.Name,
					job.Database.Database,
					job.Database.Host,
					job.Compression.Kind,
					job.Storage.Kind,
					schedule,
				)
			}
			w.Flush()
			return nil
		},
	}

	cmd.Flags().StringVarP(&configPath, "config", "c", "config.yaml", "Path to config file")
	return cmd
}
