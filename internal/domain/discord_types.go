package domain

import (
	"io"
	"time"
)

type Snowflake string
type GuildID Snowflake
type ChannelID Snowflake
type RoleID Snowflake
type UserID Snowflake
type MessageID Snowflake
type WebhookID Snowflake
type ApplicationID Snowflake
type ApplicationCommandID Snowflake
type ScheduledEventID Snowflake
type PermissionBits string
type InviteCode string
type IdempotencyKey string
type ReconciliationKey string

type ChannelType int
type OverwriteType int
type ApplicationCommandType int
type ApplicationCommandOptionType int
type ScheduledEventEntityType int
type ScheduledEventPrivacyLevel int

type Guild struct {
	ID      GuildID `json:"id"`
	Name    string  `json:"name"`
	OwnerID UserID  `json:"owner_id"`
}

type Channel struct {
	ID                            ChannelID             `json:"id"`
	GuildID                       *GuildID              `json:"guild_id,omitempty"`
	Type                          ChannelType           `json:"type"`
	Name                          *string               `json:"name,omitempty"`
	ParentID                      *ChannelID            `json:"parent_id,omitempty"`
	Position                      int                   `json:"position"`
	Topic                         *string               `json:"topic,omitempty"`
	NSFW                          *bool                 `json:"nsfw,omitempty"`
	RateLimitPerUser              *int                  `json:"rate_limit_per_user,omitempty"`
	Bitrate                       *int                  `json:"bitrate,omitempty"`
	UserLimit                     *int                  `json:"user_limit,omitempty"`
	RTCRegion                     *string               `json:"rtc_region,omitempty"`
	VideoQualityMode              *int                  `json:"video_quality_mode,omitempty"`
	DefaultAutoArchiveDuration    *int                  `json:"default_auto_archive_duration,omitempty"`
	DefaultThreadRateLimitPerUser *int                  `json:"default_thread_rate_limit_per_user,omitempty"`
	AvailableTags                 []ForumTag            `json:"available_tags,omitempty"`
	DefaultReactionEmoji          *DefaultReaction      `json:"default_reaction_emoji,omitempty"`
	DefaultSortOrder              *int                  `json:"default_sort_order,omitempty"`
	DefaultForumLayout            *int                  `json:"default_forum_layout,omitempty"`
	PermissionOverwrites          []PermissionOverwrite `json:"permission_overwrites,omitempty"`
}

type ForumTag struct {
	ID        Snowflake  `json:"id,omitempty"`
	Name      string     `json:"name"`
	Moderated bool       `json:"moderated"`
	EmojiID   *Snowflake `json:"emoji_id,omitempty"`
	EmojiName *string    `json:"emoji_name,omitempty"`
}

type DefaultReaction struct {
	EmojiID   *Snowflake `json:"emoji_id,omitempty"`
	EmojiName *string    `json:"emoji_name,omitempty"`
}

type PermissionOverwrite struct {
	ID    Snowflake      `json:"id"`
	Type  OverwriteType  `json:"type"`
	Allow PermissionBits `json:"allow"`
	Deny  PermissionBits `json:"deny"`
}

type Role struct {
	ID          RoleID         `json:"id"`
	Name        string         `json:"name"`
	Permissions PermissionBits `json:"permissions"`
	Position    int            `json:"position"`
	Color       int            `json:"color"`
	Hoist       bool           `json:"hoist"`
	Mentionable bool           `json:"mentionable"`
	Managed     bool           `json:"managed"`
}

type User struct {
	ID         UserID  `json:"id"`
	Username   string  `json:"username"`
	GlobalName *string `json:"global_name,omitempty"`
	Bot        *bool   `json:"bot,omitempty"`
}

type Member struct {
	User                       User       `json:"user"`
	Nick                       *string    `json:"nick,omitempty"`
	Roles                      []RoleID   `json:"roles"`
	JoinedAt                   time.Time  `json:"joined_at"`
	Deaf                       bool       `json:"deaf"`
	Mute                       bool       `json:"mute"`
	Pending                    *bool      `json:"pending,omitempty"`
	CommunicationDisabledUntil *time.Time `json:"communication_disabled_until,omitempty"`
}

type Embed struct {
	Title       *string      `json:"title,omitempty"`
	Description *string      `json:"description,omitempty"`
	URL         *string      `json:"url,omitempty"`
	Color       *int         `json:"color,omitempty"`
	Fields      []EmbedField `json:"fields,omitempty"`
	Footer      *EmbedFooter `json:"footer,omitempty"`
	Author      *EmbedAuthor `json:"author,omitempty"`
}

type EmbedField struct {
	Name   string `json:"name"`
	Value  string `json:"value"`
	Inline *bool  `json:"inline,omitempty"`
}
type EmbedFooter struct {
	Text string `json:"text"`
}
type EmbedAuthor struct {
	Name string  `json:"name"`
	URL  *string `json:"url,omitempty"`
}

type Attachment struct {
	ID          Snowflake `json:"id"`
	Filename    string    `json:"filename"`
	Description *string   `json:"description,omitempty"`
	Size        int64     `json:"size"`
	ContentType *string   `json:"content_type,omitempty"`
}

type Upload struct {
	Filename    string    `json:"filename"`
	Description *string   `json:"description,omitempty"`
	Size        int64     `json:"size"`
	Reader      io.Reader `json:"-"`
}

type Message struct {
	ID              MessageID    `json:"id"`
	ChannelID       ChannelID    `json:"channel_id"`
	GuildID         *GuildID     `json:"guild_id,omitempty"`
	Author          User         `json:"author"`
	Content         string       `json:"content"`
	Timestamp       time.Time    `json:"timestamp"`
	EditedTimestamp *time.Time   `json:"edited_timestamp,omitempty"`
	Embeds          []Embed      `json:"embeds"`
	Attachments     []Attachment `json:"attachments"`
	Pinned          bool         `json:"pinned"`
	Nonce           *string      `json:"nonce,omitempty"`
}

type Webhook struct {
	ID            WebhookID      `json:"id"`
	GuildID       *GuildID       `json:"guild_id,omitempty"`
	ChannelID     *ChannelID     `json:"channel_id,omitempty"`
	Name          *string        `json:"name,omitempty"`
	Type          int            `json:"type"`
	ApplicationID *ApplicationID `json:"application_id,omitempty"`
}

type ApplicationCommandOption struct {
	Type         ApplicationCommandOptionType `json:"type"`
	Name         string                       `json:"name"`
	Description  string                       `json:"description"`
	Required     *bool                        `json:"required,omitempty"`
	Choices      []ApplicationCommandChoice   `json:"choices,omitempty"`
	Options      []ApplicationCommandOption   `json:"options,omitempty"`
	MinValue     *float64                     `json:"min_value,omitempty"`
	MaxValue     *float64                     `json:"max_value,omitempty"`
	MinLength    *int                         `json:"min_length,omitempty"`
	MaxLength    *int                         `json:"max_length,omitempty"`
	ChannelTypes []ChannelType                `json:"channel_types,omitempty"`
}
type ApplicationCommandChoice struct {
	Name  string `json:"name"`
	Value any    `json:"value"`
}
type ApplicationCommand struct {
	ID                       ApplicationCommandID       `json:"id"`
	ApplicationID            ApplicationID              `json:"application_id"`
	GuildID                  *GuildID                   `json:"guild_id,omitempty"`
	Type                     ApplicationCommandType     `json:"type"`
	Name                     string                     `json:"name"`
	Description              string                     `json:"description"`
	Options                  []ApplicationCommandOption `json:"options,omitempty"`
	DefaultMemberPermissions *PermissionBits            `json:"default_member_permissions,omitempty"`
	DMPermission             *bool                      `json:"dm_permission,omitempty"`
	NSFW                     *bool                      `json:"nsfw,omitempty"`
	Version                  Snowflake                  `json:"version"`
}

type Invite struct {
	Code      InviteCode `json:"code"`
	Guild     *Guild     `json:"guild,omitempty"`
	Channel   *Channel   `json:"channel,omitempty"`
	Inviter   *User      `json:"inviter,omitempty"`
	MaxAge    int        `json:"max_age"`
	MaxUses   int        `json:"max_uses"`
	Temporary bool       `json:"temporary"`
	Uses      int        `json:"uses"`
	ExpiresAt *time.Time `json:"expires_at,omitempty"`
}

type ScheduledEventMetadata struct {
	Location string `json:"location"`
}
type ScheduledEvent struct {
	ID                 ScheduledEventID           `json:"id"`
	GuildID            GuildID                    `json:"guild_id"`
	ChannelID          *ChannelID                 `json:"channel_id,omitempty"`
	Name               string                     `json:"name"`
	Description        *string                    `json:"description,omitempty"`
	ScheduledStartTime time.Time                  `json:"scheduled_start_time"`
	ScheduledEndTime   *time.Time                 `json:"scheduled_end_time,omitempty"`
	PrivacyLevel       ScheduledEventPrivacyLevel `json:"privacy_level"`
	EntityType         ScheduledEventEntityType   `json:"entity_type"`
	EntityMetadata     *ScheduledEventMetadata    `json:"entity_metadata,omitempty"`
	Status             int                        `json:"status"`
	UserCount          *int                       `json:"user_count,omitempty"`
}

type GuildInspection struct {
	Guild                 Guild                `json:"guild"`
	Channels              []Channel            `json:"channels"`
	Roles                 []Role               `json:"roles"`
	Webhooks              []Webhook            `json:"webhooks"`
	ApplicationCommands   []ApplicationCommand `json:"application_commands,omitempty"`
	Onboarding            *Onboarding          `json:"onboarding,omitempty"`
	OnboardingUnsupported bool                 `json:"onboarding_unsupported,omitempty"`
	Invites               []Invite             `json:"invites"`
	ScheduledEvents       []ScheduledEvent     `json:"scheduled_events"`
}

type Onboarding struct {
	GuildID           GuildID            `json:"guild_id"`
	Prompts           []OnboardingPrompt `json:"prompts"`
	DefaultChannelIDs []ChannelID        `json:"default_channel_ids"`
	Enabled           bool               `json:"enabled"`
	Mode              int                `json:"mode"`
}

type OnboardingPrompt struct {
	ID           Snowflake          `json:"id"`
	Type         int                `json:"type"`
	Options      []OnboardingOption `json:"options"`
	Title        string             `json:"title"`
	SingleSelect bool               `json:"single_select"`
	Required     bool               `json:"required"`
	InOnboarding bool               `json:"in_onboarding"`
}

type OnboardingOption struct {
	ID            Snowflake   `json:"id"`
	ChannelIDs    []ChannelID `json:"channel_ids"`
	RoleIDs       []RoleID    `json:"role_ids"`
	EmojiID       *Snowflake  `json:"emoji_id,omitempty"`
	EmojiName     *string     `json:"emoji_name,omitempty"`
	EmojiAnimated *bool       `json:"emoji_animated,omitempty"`
	Title         string      `json:"title"`
	Description   *string     `json:"description,omitempty"`
}

type EmptyResult struct{}
type DeleteResult struct {
	ID Snowflake `json:"id"`
}
type BulkDeleteResult struct {
	IDs []MessageID `json:"ids"`
}
