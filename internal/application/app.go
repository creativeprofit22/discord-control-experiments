package application

import (
	"context"
	"errors"
	"fmt"

	"github.com/ken-kai/discord-control-experiments/internal/domain"
)

type App struct {
	transports []Lifecycle
	telemetry  Telemetry
	clock      Clock
	correlate  func() (domain.CorrelationID, error)
}

func New(discord DiscordTransport, mcp MCPTransport, telemetry Telemetry, clock Clock, correlate func() (domain.CorrelationID, error)) (*App, error) {
	if discord == nil || mcp == nil || telemetry == nil || clock == nil || correlate == nil {
		return nil, errors.New("application dependencies are required")
	}
	return &App{transports: []Lifecycle{discord, mcp}, telemetry: telemetry, clock: clock, correlate: correlate}, nil
}

func (a *App) Run(ctx context.Context) error {
	correlationID, err := a.correlate()
	if err != nil {
		return fmt.Errorf("create correlation ID: %w", err)
	}
	started := make([]Lifecycle, 0, len(a.transports))
	for _, transport := range a.transports {
		for attempt := 0; attempt < 2; attempt++ {
			op := domain.Operation{Name: "startup", Adapter: transport.Name(), CorrelationID: correlationID, StartedAt: a.clock.Now(), RetryCount: attempt}
			err = a.telemetry.Observe(ctx, op, transport.Start)
			if err == nil {
				break
			}
		}
		if err != nil {
			_ = a.stop(context.WithoutCancel(ctx), started, correlationID)
			return fmt.Errorf("start %s after retry: %w", transport.Name(), err)
		}
		started = append(started, transport)
	}
	<-ctx.Done()
	return a.stop(context.WithoutCancel(ctx), started, correlationID)
}

func (a *App) stop(ctx context.Context, started []Lifecycle, correlationID domain.CorrelationID) error {
	var result error
	for i := len(started) - 1; i >= 0; i-- {
		transport := started[i]
		op := domain.Operation{Name: "shutdown", Adapter: transport.Name(), CorrelationID: correlationID, StartedAt: a.clock.Now()}
		result = errors.Join(result, a.telemetry.Observe(ctx, op, transport.Stop))
	}
	return result
}
