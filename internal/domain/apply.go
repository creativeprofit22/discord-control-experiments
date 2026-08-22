package domain

import (
	"errors"
	"fmt"
	"time"
)

type RunID string
type RunState string
type StepState string
type ResumeStatus string

type JournalEventType string

const (
	RunPending   RunState = "pending"
	RunApplying  RunState = "applying"
	RunCompleted RunState = "completed"
	RunPartial   RunState = "partial"
	RunFailed    RunState = "failed"

	StepPending   StepState = "pending"
	StepStarted   StepState = "started"
	StepCompleted StepState = "completed"
	StepFailed    StepState = "failed"
	StepUncertain StepState = "uncertain"
	StepSkipped   StepState = "skipped"

	ResumeNotNeeded ResumeStatus = "not_needed"
	ResumeContinued ResumeStatus = "continued"
	ResumeConflict  ResumeStatus = "conflict"

	JournalRunStarted   JournalEventType = "run_started"
	JournalStepStarted  JournalEventType = "step_started"
	JournalStepComplete JournalEventType = "step_completed"
	JournalStepFailed   JournalEventType = "step_failed"
	JournalRunComplete  JournalEventType = "run_completed"
	JournalRunFailed    JournalEventType = "run_failed"
)

type ApplyRequest struct {
	Plan          Plan          `json:"plan"`
	PlanHash      PlanHash      `json:"plan_hash"`
	Principal     Principal     `json:"-"`
	ActorID       ActorID       `json:"actor_id"`
	ApprovalID    ApprovalID    `json:"approval_id,omitempty"`
	AllowDelete   bool          `json:"allow_delete"`
	CorrelationID CorrelationID `json:"correlation_id"`
}

type ApplyStepResult struct {
	OperationID OperationID       `json:"operation_id"`
	Resource    StableResourceRef `json:"resource"`
	State       StepState         `json:"state"`
	RemoteID    Snowflake         `json:"remote_id,omitempty"`
	Attempt     int               `json:"attempt"`
	ErrorCode   string            `json:"error_code,omitempty"`
}

type ApplyResult struct {
	RunID        RunID             `json:"run_id"`
	PlanHash     PlanHash          `json:"plan_hash"`
	State        RunState          `json:"state"`
	ResumeStatus ResumeStatus      `json:"resume_status"`
	Steps        []ApplyStepResult `json:"steps"`
	StartedAt    time.Time         `json:"started_at"`
	FinishedAt   time.Time         `json:"finished_at,omitempty"`
}

type JournalEvent struct {
	RunID        RunID              `json:"run_id"`
	Sequence     uint64             `json:"sequence"`
	PreviousHash string             `json:"previous_hash"`
	RecordHash   string             `json:"record_hash"`
	PlanHash     PlanHash           `json:"plan_hash"`
	GuildID      GuildID            `json:"guild_id"`
	Type         JournalEventType   `json:"type"`
	OperationID  OperationID        `json:"operation_id,omitempty"`
	Resource     *StableResourceRef `json:"resource,omitempty"`
	State        StepState          `json:"state,omitempty"`
	RemoteID     Snowflake          `json:"remote_id,omitempty"`
	Fingerprint  string             `json:"fingerprint,omitempty"`
	OccurredAt   time.Time          `json:"occurred_at"`
	ErrorCode    string             `json:"error_code,omitempty"`
}

type DriftReport struct {
	PlanHash           PlanHash       `json:"plan_hash"`
	SnapshotHash       string         `json:"snapshot_hash"`
	ManagedDrift       []PlanConflict `json:"managed_drift"`
	UnmanagedConflicts []PlanConflict `json:"unmanaged_conflicts"`
	Stale              bool           `json:"stale"`
}

var (
	ErrStalePlan       = errors.New("reconciliation plan is stale")
	ErrPlanConflict    = errors.New("reconciliation plan has conflicts")
	ErrUnsafeOperation = errors.New("reconciliation operation is unsafe")
)

type StalePlanError struct{ Reason string }

func (e *StalePlanError) Error() string { return fmt.Sprintf("%v: %s", ErrStalePlan, e.Reason) }
func (e *StalePlanError) Unwrap() error { return ErrStalePlan }

type ConflictError struct {
	Reason    string
	Conflicts []PlanConflict
}

func (e *ConflictError) Error() string { return fmt.Sprintf("%v: %s", ErrPlanConflict, e.Reason) }
func (e *ConflictError) Unwrap() error { return ErrPlanConflict }

type UnsafeOperationError struct {
	OperationID OperationID
	Reason      string
}

func (e *UnsafeOperationError) Error() string {
	return fmt.Sprintf("%v: operation %s: %s", ErrUnsafeOperation, e.OperationID, e.Reason)
}
func (e *UnsafeOperationError) Unwrap() error { return ErrUnsafeOperation }
