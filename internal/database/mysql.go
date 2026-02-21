package database

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strconv"

	"gobackups/internal/config"
)

// MySQLDumper runs mysqldump as a subprocess and streams its stdout.
// It uses --single-transaction to take a consistent InnoDB snapshot
// without acquiring global table locks.
type MySQLDumper struct {
	cfg config.DBConfig
}

// NewMySQL creates a new MySQLDumper from the given DB config.
func NewMySQL(cfg config.DBConfig) *MySQLDumper {
	return &MySQLDumper{cfg: cfg}
}

// FileExtension returns the SQL dump file extension.
func (d *MySQLDumper) FileExtension() string {
	return ".sql"
}

// Dump starts mysqldump and returns its stdout as an io.ReadCloser.
//
// The dump runs with --single-transaction, which:
//   - Creates a consistent snapshot of all InnoDB tables (MVCC, no lock)
//   - Correctly handles tables with partitions
//   - Does NOT block concurrent writes
//
// The password is passed via the MYSQL_PWD environment variable so it
// does not appear in the OS process listing.
//
// The caller must close the returned ReadCloser. Close() drains any
// remaining output and calls Wait() on the subprocess.
func (d *MySQLDumper) Dump(ctx context.Context) (io.ReadCloser, error) {
	port := d.cfg.Port
	if port == 0 {
		port = 3306
	}

	args := []string{
		"--single-transaction",
		"--routines",
		"--triggers",
		"--events",
		"--no-tablespaces",
		"--host", d.cfg.Host,
		"--port", strconv.Itoa(port),
		"--user", d.cfg.User,
	}
	args = append(args, d.cfg.Flags...)
	args = append(args, d.cfg.Database)

	cmd := exec.CommandContext(ctx, "mysqldump", args...)

	// Pass password via env var to avoid it appearing in the process list.
	cmd.Env = append(os.Environ(), "MYSQL_PWD="+d.cfg.Password)

	// mysqldump warnings and errors go directly to stderr so the operator
	// can see them. Phase 6 will route these to slog.
	cmd.Stderr = os.Stderr

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("mysqldump stdout pipe: %w", err)
	}

	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("start mysqldump: %w", err)
	}

	return &dumpReader{ReadCloser: stdout, cmd: cmd}, nil
}

// dumpReader wraps the mysqldump stdout pipe.
// Close() closes the pipe and waits for the subprocess to exit,
// returning any non-zero exit code as an error.
type dumpReader struct {
	io.ReadCloser
	cmd *exec.Cmd
}

func (r *dumpReader) Close() error {
	pipeErr := r.ReadCloser.Close()
	waitErr := r.cmd.Wait()
	if waitErr != nil {
		return fmt.Errorf("mysqldump exited with error: %w", waitErr)
	}
	return pipeErr
}
