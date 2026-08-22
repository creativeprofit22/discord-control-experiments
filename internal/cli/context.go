package cli

import (
	"context"
	"errors"
	"os"
	"os/signal"
	"syscall"
	"time"
)

func SignalContext(parent context.Context) (context.Context, context.CancelFunc) {
	return signal.NotifyContext(parent, os.Interrupt, syscall.SIGTERM)
}

func CommandContext(parent context.Context, timeout time.Duration) (context.Context, context.CancelFunc, error) {
	if timeout <= 0 || timeout > 30*time.Minute {
		return nil, nil, &ParseError{Code: "invalid_timeout", Detail: "timeout must be positive and at most 30m"}
	}
	contextWithTimeout, cancel := context.WithTimeout(parent, timeout)
	return contextWithTimeout, cancel, nil
}

func NormalizeCancellation(commandContext, signalContext context.Context, err error) error {
	if err == nil {
		return nil
	}
	if signalContext != nil && errors.Is(signalContext.Err(), context.Canceled) {
		return ErrInterrupted
	}
	if commandContext != nil && errors.Is(commandContext.Err(), context.DeadlineExceeded) {
		return context.DeadlineExceeded
	}
	return err
}
