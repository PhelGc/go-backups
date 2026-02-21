package config

import (
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

// Load reads and parses the configuration file at path.
// References in the form ${VAR} or $VAR in the file are expanded using
// environment variables before parsing.
func Load(path string) (*Config, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read config file: %w", err)
	}

	expanded := os.ExpandEnv(string(raw))

	var cfg Config
	if err := yaml.Unmarshal([]byte(expanded), &cfg); err != nil {
		return nil, fmt.Errorf("parse config: %w", err)
	}

	return &cfg, nil
}

// Validate checks the configuration for required fields and consistency.
// It also sets default values for optional fields.
func Validate(cfg *Config) error {
	if cfg.Version == "" {
		return fmt.Errorf("config.version is required")
	}
	if len(cfg.Jobs) == 0 {
		return fmt.Errorf("config.jobs must contain at least one job")
	}

	names := make(map[string]struct{}, len(cfg.Jobs))
	for i, job := range cfg.Jobs {
		prefix := fmt.Sprintf("jobs[%d] (%s)", i, job.Name)

		if job.Name == "" {
			return fmt.Errorf("jobs[%d]: name is required", i)
		}
		if _, dup := names[job.Name]; dup {
			return fmt.Errorf("%s: duplicate job name", prefix)
		}
		names[job.Name] = struct{}{}

		if err := validateDB(prefix, job.Database); err != nil {
			return err
		}
		if err := validateCompression(prefix, job.Compression); err != nil {
			return err
		}
		if err := validateStorage(prefix, job.Storage); err != nil {
			return err
		}

		// Apply defaults
		if cfg.Jobs[i].Retry.MaxAttempts < 1 {
			cfg.Jobs[i].Retry.MaxAttempts = 1
		}
		if cfg.Jobs[i].Database.Port == 0 {
			cfg.Jobs[i].Database.Port = 3306
		}
	}
	return nil
}

func validateDB(prefix string, db DBConfig) error {
	if db.Host == "" {
		return fmt.Errorf("%s: database.host is required", prefix)
	}
	if db.User == "" {
		return fmt.Errorf("%s: database.user is required", prefix)
	}
	if db.Database == "" {
		return fmt.Errorf("%s: database.database is required", prefix)
	}
	return nil
}

func validateCompression(prefix string, c CompressConfig) error {
	switch c.Kind {
	case "gzip", "zstd":
		return nil
	default:
		return fmt.Errorf("%s: compression.kind must be 'gzip' or 'zstd', got %q", prefix, c.Kind)
	}
}

func validateStorage(prefix string, s StorageConfig) error {
	switch s.Kind {
	case "local":
		if s.Local == nil || s.Local.Path == "" {
			return fmt.Errorf("%s: storage.local.path is required", prefix)
		}
	case "http":
		if s.HTTP == nil || s.HTTP.URL == "" {
			return fmt.Errorf("%s: storage.http.url is required", prefix)
		}
		if s.HTTP.FieldName == "" {
			return fmt.Errorf("%s: storage.http.field_name is required", prefix)
		}
	default:
		return fmt.Errorf("%s: storage.kind must be 'local' or 'http', got %q", prefix, s.Kind)
	}
	return nil
}
