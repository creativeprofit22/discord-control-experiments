package domain

const DeclarativeSchemaV1 = "discord-control/v1"

type ManagementMode string

const (
	ManagementModeManaged  ManagementMode = "managed"
	ManagementModeObserved ManagementMode = "observed"
	ManagementModeIgnored  ManagementMode = "ignored"
)

type StableResourceRef struct {
	Kind      ResourceKind `json:"kind"`
	LogicalID string       `json:"logical_id"`
}

type OwnershipMetadata struct {
	Owner          string `json:"owner"`
	AdoptByName    bool   `json:"adopt_by_name,omitempty"`
	DeleteOrphans  bool   `json:"delete_orphans,omitempty"`
	ProtectDeletes bool   `json:"protect_deletes"`
}

type DesiredGuild struct {
	SchemaVersion       string                      `json:"schema_version"`
	GuildID             GuildID                     `json:"guild_id"`
	ApplicationID       ApplicationID               `json:"application_id,omitempty"`
	Ownership           OwnershipMetadata           `json:"ownership"`
	Categories          []DesiredCategory           `json:"categories"`
	Channels            []DesiredChannel            `json:"channels"`
	Roles               []DesiredRole               `json:"roles"`
	Webhooks            []DesiredWebhook            `json:"webhooks"`
	ApplicationCommands []DesiredApplicationCommand `json:"application_commands"`
	Onboarding          *DesiredOnboarding          `json:"onboarding,omitempty"`
}

type DesiredCategory struct {
	LogicalID string         `json:"logical_id"`
	Name      string         `json:"name"`
	Position  int            `json:"position"`
	Mode      ManagementMode `json:"mode,omitempty"`
	Protected bool           `json:"protected,omitempty"`
}

type DesiredChannel struct {
	LogicalID              string                `json:"logical_id"`
	Name                   string                `json:"name"`
	Type                   ChannelType           `json:"type"`
	Category               *StableResourceRef    `json:"category,omitempty"`
	Position               int                   `json:"position"`
	Topic                  *string               `json:"topic,omitempty"`
	NSFW                   *bool                 `json:"nsfw,omitempty"`
	RateLimitPerUser       *int                  `json:"rate_limit_per_user,omitempty"`
	Bitrate                *int                  `json:"bitrate,omitempty"`
	UserLimit              *int                  `json:"user_limit,omitempty"`
	RTCRegion              *string               `json:"rtc_region,omitempty"`
	VideoQualityMode       *int                  `json:"video_quality_mode,omitempty"`
	DefaultAutoArchive     *int                  `json:"default_auto_archive_duration,omitempty"`
	DefaultThreadRateLimit *int                  `json:"default_thread_rate_limit_per_user,omitempty"`
	Forum                  *DesiredForumSettings `json:"forum,omitempty"`
	Overwrites             []DesiredOverwrite    `json:"permission_overwrites"`
	Mode                   ManagementMode        `json:"mode,omitempty"`
	Protected              bool                  `json:"protected,omitempty"`
}

type DesiredForumSettings struct {
	AvailableTags        []DesiredForumTag `json:"available_tags"`
	DefaultReactionEmoji *DefaultReaction  `json:"default_reaction_emoji,omitempty"`
	DefaultSortOrder     *int              `json:"default_sort_order,omitempty"`
	DefaultForumLayout   *int              `json:"default_forum_layout,omitempty"`
}

type DesiredForumTag struct {
	Name      string  `json:"name"`
	Moderated bool    `json:"moderated"`
	EmojiName *string `json:"emoji_name,omitempty"`
}

type DesiredRole struct {
	LogicalID   string         `json:"logical_id"`
	Name        string         `json:"name"`
	Permissions PermissionBits `json:"permissions"`
	Position    int            `json:"position"`
	Color       int            `json:"color,omitempty"`
	Hoist       bool           `json:"hoist,omitempty"`
	Mentionable bool           `json:"mentionable,omitempty"`
	Mode        ManagementMode `json:"mode,omitempty"`
	Protected   bool           `json:"protected,omitempty"`
}

type DesiredOverwrite struct {
	Target StableResourceRef `json:"target"`
	Type   OverwriteType     `json:"type"`
	Allow  PermissionBits    `json:"allow"`
	Deny   PermissionBits    `json:"deny"`
}

type DesiredWebhook struct {
	LogicalID string            `json:"logical_id"`
	Name      string            `json:"name"`
	Channel   StableResourceRef `json:"channel"`
	Mode      ManagementMode    `json:"mode,omitempty"`
	Protected bool              `json:"protected,omitempty"`
}

type DesiredApplicationCommand struct {
	LogicalID                string                     `json:"logical_id"`
	Type                     ApplicationCommandType     `json:"type"`
	Name                     string                     `json:"name"`
	Description              string                     `json:"description"`
	Options                  []ApplicationCommandOption `json:"options"`
	DefaultMemberPermissions *PermissionBits            `json:"default_member_permissions,omitempty"`
	NSFW                     *bool                      `json:"nsfw,omitempty"`
	Mode                     ManagementMode             `json:"mode,omitempty"`
	Protected                bool                       `json:"protected,omitempty"`
}

type DesiredOnboarding struct {
	Enabled           bool                      `json:"enabled"`
	Mode              int                       `json:"mode"`
	DefaultChannelIDs []StableResourceRef       `json:"default_channels"`
	Prompts           []DesiredOnboardingPrompt `json:"prompts"`
}

type DesiredOnboardingPrompt struct {
	LogicalID string                    `json:"logical_id"`
	Type      int                       `json:"type"`
	Title     string                    `json:"title"`
	Single    bool                      `json:"single_select"`
	Required  bool                      `json:"required"`
	InOnboard bool                      `json:"in_onboarding"`
	Options   []DesiredOnboardingOption `json:"options"`
}

type DesiredOnboardingOption struct {
	LogicalID   string              `json:"logical_id"`
	Title       string              `json:"title"`
	Description *string             `json:"description,omitempty"`
	EmojiID     *Snowflake          `json:"emoji_id,omitempty"`
	EmojiName   *string             `json:"emoji_name,omitempty"`
	Channels    []StableResourceRef `json:"channels"`
	Roles       []StableResourceRef `json:"roles"`
}
