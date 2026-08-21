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

type Policy interface {
	Decide(context.Context, string) domain.PolicyDecision
}

type Reconciler interface {
	Reconcile(context.Context, any, any) (domain.Result, error)
}

type Clock interface{ Now() time.Time }

type Telemetry interface {
	Observe(context.Context, domain.Operation, func(context.Context) error) error
}
