package domain

import "time"

const SnapshotSchemaV1 = "discord-control/snapshot-v1"

type FieldDisposition string

type SupportStatus string

const (
	FieldManaged     FieldDisposition = "managed"
	FieldObserved    FieldDisposition = "observed"
	FieldIgnored     FieldDisposition = "ignored"
	FieldUnsupported FieldDisposition = "unsupported"

	SupportAvailable   SupportStatus = "available"
	SupportUnavailable SupportStatus = "unavailable"
	SupportUnknown     SupportStatus = "unknown"
)

type SnapshotMetadata struct {
	SchemaVersion string        `json:"schema_version"`
	GuildID       GuildID       `json:"guild_id"`
	ApplicationID ApplicationID `json:"application_id,omitempty"`
	CapturedAt    time.Time     `json:"captured_at"`
	CanonicalHash string        `json:"canonical_hash"`
}

type ResourceBinding struct {
	Ref         StableResourceRef `json:"ref"`
	RemoteID    Snowflake         `json:"remote_id"`
	Fingerprint string            `json:"fingerprint"`
}

type SnapshotField struct {
	Name        string           `json:"name"`
	Disposition FieldDisposition `json:"disposition"`
	Reason      string           `json:"reason,omitempty"`
}

type SnapshotResource struct {
	Ref         StableResourceRef  `json:"ref"`
	RemoteID    Snowflake          `json:"remote_id"`
	ParentRef   *StableResourceRef `json:"parent_ref,omitempty"`
	Name        string             `json:"name"`
	Type        int                `json:"type,omitempty"`
	Order       int                `json:"order"`
	Managed     bool               `json:"managed"`
	Protected   bool               `json:"protected,omitempty"`
	Fingerprint string             `json:"fingerprint"`
	Fields      []SnapshotField    `json:"fields"`
}

type SnapshotCapability struct {
	Resource ResourceKind  `json:"resource"`
	Status   SupportStatus `json:"status"`
	Reason   string        `json:"reason,omitempty"`
}

type GuildSnapshot struct {
	Metadata                    SnapshotMetadata     `json:"metadata"`
	Guild                       Guild                `json:"guild"`
	Categories                  []SnapshotResource   `json:"categories"`
	Channels                    []SnapshotResource   `json:"channels"`
	Roles                       []SnapshotResource   `json:"roles"`
	Webhooks                    []SnapshotResource   `json:"webhooks"`
	ApplicationCommands         []SnapshotResource   `json:"application_commands"`
	AutoModerationRules         []SnapshotResource   `json:"auto_moderation_rules,omitempty"`
	Onboarding                  *Onboarding          `json:"onboarding,omitempty"`
	Capabilities                []SnapshotCapability `json:"capabilities"`
	Bindings                    []ResourceBinding    `json:"bindings"`
	ObservedChannels            []Channel            `json:"observed_channels"`
	ObservedRoles               []Role               `json:"observed_roles"`
	ObservedWebhooks            []Webhook            `json:"observed_webhooks"`
	ObservedCommands            []ApplicationCommand `json:"observed_application_commands"`
	ObservedAutoModerationRules []AutoModerationRule `json:"observed_auto_moderation_rules,omitempty"`
}
