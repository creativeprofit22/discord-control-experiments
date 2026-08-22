package domain

import "fmt"

type ReplayState string

const (
	ReplayNone      ReplayState = "none"
	ReplayCoalesced ReplayState = "coalesced"
	ReplaySucceeded ReplayState = "succeeded"
)

type PartialOutcome string

const (
	PartialNotSent            PartialOutcome = "not_sent"
	PartialSentUnknown        PartialOutcome = "sent_unknown"
	PartialResponseReceived   PartialOutcome = "response_received"
	PartialReconcileAmbiguous PartialOutcome = "reconcile_ambiguous"
)

type PartialResult struct {
	Outcome              PartialOutcome
	CompletedResourceIDs []Snowflake
	IdempotencyKey       IdempotencyKey
	ReconciliationKey    ReconciliationKey
	Recoverable          bool
}

type Execution[R any] struct {
	Result   R
	Plan     *OperationPlan
	Attempts int
	Replay   ReplayState
	Partial  *PartialResult
}

type ValidationError struct{ Field, Rule string }

func (e *ValidationError) Error() string {
	return fmt.Sprintf("validation failed for %s: %s", e.Field, e.Rule)
}

type OwnershipError struct {
	Resource         string
	Expected, Actual GuildID
}

func (e *OwnershipError) Error() string {
	return "resource ownership is unknown or does not match the command guild"
}

type TransportError struct {
	Uncertain bool
	Err       error
}

func (e *TransportError) Error() string { return "Discord transport failed" }
func (e *TransportError) Unwrap() error { return e.Err }

type CancellationError struct{ Err error }

func (e *CancellationError) Error() string { return "Discord operation canceled" }
func (e *CancellationError) Unwrap() error { return e.Err }

type BodyLimitError struct {
	Direction string
	Limit     int64
}

func (e *BodyLimitError) Error() string {
	return fmt.Sprintf("Discord %s body exceeded configured limit", e.Direction)
}

type PayloadError struct{ Kind string }

func (e *PayloadError) Error() string { return "Discord returned a malformed payload" }

type DiscordFieldError struct{ Path, Code, Message string }
type DiscordAPIError struct {
	Status, Code int
	Message      string
	Fields       []DiscordFieldError
}

func (e *DiscordAPIError) Error() string {
	return fmt.Sprintf("Discord API request failed (status %d, code %d)", e.Status, e.Code)
}

type RetryExhaustedError struct {
	Attempts int
	Reason   string
}

func (e *RetryExhaustedError) Error() string { return "Discord retry budget exhausted" }

type IdempotencyConflictError struct{}

func (*IdempotencyConflictError) Error() string {
	return "idempotency key was reused for a different command"
}

type ReconciliationAmbiguityError struct{ Matches int }

func (*ReconciliationAmbiguityError) Error() string {
	return "Discord reconciliation found multiple matching resources"
}

type InvariantError struct{ Detail string }

func (*InvariantError) Error() string { return "internal Discord invariant failed" }
