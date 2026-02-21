package scheduler

import (
	"log/slog"

	"github.com/robfig/cron/v3"
)

// Scheduler wraps robfig/cron to schedule backup jobs.
type Scheduler struct {
	cron   *cron.Cron
	logger *slog.Logger
}

// New creates a new Scheduler with the given logger.
func New(logger *slog.Logger) *Scheduler {
	return &Scheduler{
		cron:   cron.New(),
		logger: logger,
	}
}

// Add registers a function to run on the given cron expression.
// expr uses standard 5-field cron syntax: minute hour day month weekday.
func (s *Scheduler) Add(expr string, fn func()) error {
	_, err := s.cron.AddFunc(expr, fn)
	return err
}

// Start begins the scheduler's background goroutine.
func (s *Scheduler) Start() {
	s.cron.Start()
}

// Stop signals the scheduler to stop accepting new jobs and waits for any
// currently running jobs to complete before returning.
func (s *Scheduler) Stop() {
	ctx := s.cron.Stop()
	<-ctx.Done()
	s.logger.Info("scheduler stopped, all in-progress jobs drained")
}
