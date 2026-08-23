package application

import (
	"context"
	"io"
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

type ReconciliationJournal interface {
	Append(domain.JournalEvent) (domain.JournalEvent, error)
}

type ReconciliationState interface {
	LoadBindings(domain.GuildID) ([]domain.ResourceBinding, error)
	SaveBindings(domain.GuildID, []domain.ResourceBinding) error
	AcquireGuildLock(domain.GuildID) (io.Closer, error)
	LoadJournalEvents(domain.GuildID, domain.RunID, domain.PlanHash) ([]domain.JournalEvent, error)
	OpenJournal(domain.GuildID, domain.RunID, domain.PlanHash) (ReconciliationJournal, []domain.JournalEvent, error)
	SavePlan(domain.GuildID, domain.RunID, domain.Plan) error
	LoadPlan(domain.GuildID, domain.RunID) (domain.Plan, error)
}

type ReconciliationObservation struct {
	CorrelationID      domain.CorrelationID
	PlanHashPrefix     string
	RunID              domain.RunID
	OperationID        domain.OperationID
	ResourceKind       domain.ResourceKind
	Risk               domain.RiskClass
	Attempt            int
	Outcome            string
	OperationCount     int
	RiskCounts         map[domain.RiskClass]int
	ManagedDrift       int
	UnmanagedConflicts int
	Retries            int
	Partial            bool
	Resumed            bool
	Latency            time.Duration
}

type ReconciliationTelemetry interface {
	ObserveReconciliation(context.Context, ReconciliationObservation)
}

type Telemetry interface {
	Observe(context.Context, domain.Operation, func(context.Context) error) error
}
