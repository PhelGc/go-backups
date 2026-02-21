package config

// Config is the root of the YAML configuration file.
type Config struct {
	Version string      `yaml:"version"`
	Jobs    []JobConfig `yaml:"jobs"`
}

// JobConfig defines one complete backup job.
type JobConfig struct {
	Name        string         `yaml:"name"`
	Schedule    string         `yaml:"schedule"`
	Database    DBConfig       `yaml:"database"`
	Compression CompressConfig `yaml:"compression"`
	Storage     StorageConfig  `yaml:"storage"`
	Notify      *NotifyConfig  `yaml:"notify,omitempty"`
	Retry       RetryConfig    `yaml:"retry"`
}

// DBConfig holds MySQL/MariaDB connection details.
type DBConfig struct {
	Host     string   `yaml:"host"`
	Port     int      `yaml:"port"`
	User     string   `yaml:"user"`
	Password string   `yaml:"password"`
	Database string   `yaml:"database"`
	Flags    []string `yaml:"flags,omitempty"`
}

// CompressConfig selects and configures a compressor.
type CompressConfig struct {
	Kind  string `yaml:"kind"`
	Level int    `yaml:"level,omitempty"`
}

// StorageConfig selects and configures a storage backend.
type StorageConfig struct {
	Kind  string      `yaml:"kind"`
	Local *LocalConfig `yaml:"local,omitempty"`
	HTTP  *HTTPConfig  `yaml:"http,omitempty"`
}

// LocalConfig configures local filesystem storage.
type LocalConfig struct {
	Path string `yaml:"path"`
}

// HTTPConfig configures HTTP multipart upload storage.
type HTTPConfig struct {
	URL            string            `yaml:"url"`
	Headers        map[string]string `yaml:"headers,omitempty"`
	FieldName      string            `yaml:"field_name"`
	TimeoutSeconds int               `yaml:"timeout_seconds"`
	// StagePath: si se configura, el backup se escribe primero en este
	// directorio local y luego se sube. Si la conexion falla solo se
	// reintenta el upload sin volver a correr mysqldump.
	// El archivo se elimina automaticamente al subir con exito.
	StagePath string `yaml:"stage_path,omitempty"`
}

// NotifyConfig configures webhook notifications.
type NotifyConfig struct {
	WebhookURL string            `yaml:"webhook_url"`
	Headers    map[string]string `yaml:"headers,omitempty"`
}

// RetryConfig configures retry behavior.
type RetryConfig struct {
	MaxAttempts  int `yaml:"max_attempts"`
	DelaySeconds int `yaml:"delay_seconds"`
}
