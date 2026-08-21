package jobs

import (
	"context"
	"errors"
)

type Job func(context.Context) error

type Runner struct{ limit chan struct{} }

func New(limit int) (*Runner, error) {
	if limit < 1 || limit > 64 {
		return nil, errors.New("job concurrency limit must be between 1 and 64")
	}
	return &Runner{limit: make(chan struct{}, limit)}, nil
}

func (r *Runner) Run(ctx context.Context, job Job) error {
	if job == nil {
		return errors.New("job is required")
	}
	select {
	case r.limit <- struct{}{}:
		defer func() { <-r.limit }()
		return job(ctx)
	case <-ctx.Done():
		return ctx.Err()
	}
}
