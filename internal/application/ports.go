package application

import (
	"context"
	"time"

	"github.com/ken-kai/discord-control-experiments/internal/domain"
)

type Lifecycle interface {
	Name() domain.Adapter
	Start(context.Context) error
	Stop(context.Context) error
}

type DiscordTransport interface{ Lifecycle }
type MCPTransport interface{ Lifecycle }

// DiscordPort is the only Discord capability boundary exposed to application code.
// Implementations receive sealed commands, never caller-controlled methods or URLs.
type DiscordPort interface {
	ExecuteDiscord(context.Context, domain.Command) (DiscordPortResult, error)
}

type DiscordPortResult struct {
	Value       any
	Attempts    int
	Sent        bool
	Uncertain   bool
	StatusClass string
}

type DiscordTelemetry interface {
	ObserveDiscordCommand(context.Context, *DiscordObservation, func(context.Context) error) error
}

type DiscordObservation struct {
	CommandName    string
	RouteTemplate  string
	Method         string
	StatusClass    string
	ResultClass    string
	RetryReason    string
	RateLimitScope string
	Attempts       int
	Replay         domain.ReplayState
	Partial        *domain.PartialResult
}

type Reconciler interface {
	Reconcile(context.Context, any, any) (domain.Result, error)
}

type Clock interface{ Now() time.Time }

type CorrelationIDGenerator interface {
	NewCorrelationID() (domain.CorrelationID, error)
}

type Telemetry interface {
	Observe(context.Context, domain.Operation, func(context.Context) error) error
}
