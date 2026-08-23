package domain

import "time"

const PlanSchemaV1 = "discord-control/plan-v1"

type PlanHash string
type RiskClass string
type OperationKind string
type OperationID string

type ConflictKind string

const (
	RiskReadOnly            RiskClass = "read_only"
	RiskAdditive            RiskClass = "additive"
	RiskMutating            RiskClass = "mutating"
	RiskPermissionSensitive RiskClass = "permission_sensitive"
	RiskReplacement         RiskClass = "replacement"
	RiskDestructive         RiskClass = "destructive"

	OperationCreate    OperationKind = "create"
	OperationUpdate    OperationKind = "update"
	OperationMove      OperationKind = "move"
	OperationOverwrite OperationKind = "overwrite"
	OperationDelete    OperationKind = "delete"
	OperationReplace   OperationKind = "replace"
	OperationReorder   OperationKind = "reorder"
	OperationNoop      OperationKind = "noop"

	ConflictAmbiguousMatch    ConflictKind = "ambiguous_match"
	ConflictUnmanagedResource ConflictKind = "unmanaged_resource"
	ConflictUnsupportedField  ConflictKind = "unsupported_field"
	ConflictProtectedDelete   ConflictKind = "protected_delete"
	ConflictStalePrecondition ConflictKind = "stale_precondition"
)

type PlanBinding struct {
	GuildID             GuildID       `json:"guild_id"`
	ApplicationID       ApplicationID `json:"application_id,omitempty"`
	ActorID             ActorID       `json:"actor_id"`
	DesiredHash         string        `json:"desired_hash"`
	SnapshotHash        string        `json:"snapshot_hash"`
	AllowDesiredDeletes bool          `json:"allow_desired_deletes"`
}

type Preconditions struct {
	SnapshotHash        string    `json:"snapshot_hash"`
	ResourceRemoteID    Snowflake `json:"resource_remote_id,omitempty"`
	ResourceFingerprint string    `json:"resource_fingerprint,omitempty"`
	ExpectedAbsent      bool      `json:"expected_absent,omitempty"`
}

type OperationPayload struct {
	Category           *DesiredCategory           `json:"category,omitempty"`
	Channel            *DesiredChannel            `json:"channel,omitempty"`
	Role               *DesiredRole               `json:"role,omitempty"`
	Overwrite          *DesiredOverwrite          `json:"overwrite,omitempty"`
	Webhook            *DesiredWebhook            `json:"webhook,omitempty"`
	ApplicationCommand *DesiredApplicationCommand `json:"application_command,omitempty"`
	AutoModerationRule *DesiredAutoModerationRule `json:"auto_moderation_rule,omitempty"`
	Onboarding         *DesiredOnboarding         `json:"onboarding,omitempty"`
	Order              []StableResourceRef        `json:"order,omitempty"`
}

type PlanOperation struct {
	ID                 OperationID       `json:"id"`
	Kind               OperationKind     `json:"kind"`
	Resource           StableResourceRef `json:"resource"`
	Risk               RiskClass         `json:"risk"`
	RequiresApproval   bool              `json:"requires_approval"`
	Dependencies       []OperationID     `json:"dependencies"`
	Preconditions      Preconditions     `json:"preconditions"`
	Payload            OperationPayload  `json:"payload"`
	PayloadFingerprint string            `json:"payload_fingerprint"`
}

type PlanConflict struct {
	Kind     ConflictKind      `json:"kind"`
	Resource StableResourceRef `json:"resource"`
	Field    string            `json:"field,omitempty"`
	Message  string            `json:"message"`
}

type Plan struct {
	SchemaVersion string          `json:"schema_version"`
	Hash          PlanHash        `json:"hash"`
	Binding       PlanBinding     `json:"binding"`
	Operations    []PlanOperation `json:"operations"`
	Conflicts     []PlanConflict  `json:"conflicts"`
	CreatedAt     time.Time       `json:"created_at"`
	Summary       string          `json:"summary,omitempty"`
}
