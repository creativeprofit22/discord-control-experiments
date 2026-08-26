package application

import (
	"context"
	"time"

	"github.com/ken-kai/discord-control-experiments/internal/domain"
)

type AutomationClock interface{ Now() time.Time }

type AutomationStore interface {
	SaveDefinition(context.Context, domain.AutomationWorkflow) (string, error)
	LoadDefinition(string) (domain.AutomationWorkflow, error)
	SaveSchedule(context.Context, uint64, domain.AutomationSchedule) error
	LoadSchedule(string) (domain.AutomationSchedule, error)
	EnqueueRun(context.Context, domain.AutomationRun) (bool, error)
	LoadRun(string) (domain.AutomationRun, error)
	CompareAndSwapRun(context.Context, uint64, domain.AutomationRun) error
	CompareAndSwapOwnedRun(context.Context, uint64, domain.AutomationRun, string, string, time.Time) error
	ListRuns() ([]domain.AutomationRun, error)
	DeadLetter(context.Context, domain.AutomationRun) error
	ClaimRun(context.Context, string, string, string, time.Time, time.Duration) (domain.AutomationRun, error)
	Heartbeat(context.Context, string, string, string, time.Time, time.Duration) error
}

type AutomationJournal interface {
	AppendDispatching(string, time.Time) (domain.AutomationJournalEvent, error)
	Append(string, string, time.Time) (domain.AutomationJournalEvent, error)
	Close() error
}

type AutomationJournalStore interface {
	OpenAutomationJournal(string, string) (AutomationJournal, []domain.AutomationJournalEvent, error)
}

type SourceItem struct {
	ID        string
	Kind      string
	Title     string
	Text      string
	Link      string
	Published time.Time
}

type SourceBatch struct {
	Items       []SourceItem
	Cursor      string
	NotModified bool
}

type AutomationSource interface {
	Fetch(context.Context, string, string, int) (SourceBatch, error)
}

type SummaryRequest struct {
	Title, Text, Link string
	Published         time.Time
	MaxBytes          int
}
type SummaryResult struct{ Text, Model string }
type AutomationSummarizer interface {
	Summarize(context.Context, SummaryRequest) (SummaryResult, error)
}

type AutomationMemberScanner interface {
	ListAutomationMembers(context.Context, domain.GuildID, string, int) ([]domain.Member, error)
}

type AutomationSnapshot interface {
	Snapshot(context.Context, domain.GuildID) error
}

type AutomationActionResult struct {
	ProvenUnsent, Retryable, Uncertain bool
	ReviewChanged                      bool
	Rendered                           string
}
type AutomationActionPreviewer interface {
	PreviewAutomationAction(context.Context, Invocation, domain.GuildID, domain.AutomationAction) (string, error)
}

type AutomationActionExecutor interface {
	ExecuteAutomationAction(context.Context, Invocation, domain.GuildID, domain.AutomationAction, string, string) (AutomationActionResult, error)
}

type WorkflowObservation struct {
	CorrelationID                      domain.CorrelationID
	WorkflowID, RunID, GuildID, StepID string
	Event, Outcome                     string
	Lag                                time.Duration
	Attempt                            int
	Provenance                         domain.AutomationProvenance
}

type WorkflowTelemetry interface {
	ObserveWorkflow(context.Context, WorkflowObservation)
}
