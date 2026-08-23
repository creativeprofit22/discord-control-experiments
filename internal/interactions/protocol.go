package interactions

import "encoding/json"

type InteractionType uint8
type CallbackType uint8

const (
	InteractionPing               InteractionType = 1
	InteractionApplicationCommand InteractionType = 2
	InteractionMessageComponent   InteractionType = 3
	InteractionAutocomplete       InteractionType = 4
	InteractionModalSubmit        InteractionType = 5
)

const (
	CallbackPong                   CallbackType = 1
	CallbackChannelMessage         CallbackType = 4
	CallbackDeferredChannelMessage CallbackType = 5
	CallbackDeferredMessageUpdate  CallbackType = 6
	CallbackUpdateMessage          CallbackType = 7
	CallbackAutocomplete           CallbackType = 8
	CallbackModal                  CallbackType = 9
)

const EphemeralFlag uint64 = 1 << 6

type User struct {
	ID                   string          `json:"id"`
	Username             string          `json:"username,omitempty"`
	Discriminator        string          `json:"discriminator,omitempty"`
	GlobalName           *string         `json:"global_name,omitempty"`
	Avatar               *string         `json:"avatar,omitempty"`
	Bot                  bool            `json:"bot,omitempty"`
	System               bool            `json:"system,omitempty"`
	MFAEnabled           bool            `json:"mfa_enabled,omitempty"`
	Banner               *string         `json:"banner,omitempty"`
	AccentColor          *int            `json:"accent_color,omitempty"`
	Locale               string          `json:"locale,omitempty"`
	Verified             bool            `json:"verified,omitempty"`
	Email                *string         `json:"email,omitempty"`
	Flags                uint64          `json:"flags,omitempty"`
	PremiumType          int             `json:"premium_type,omitempty"`
	PublicFlags          uint64          `json:"public_flags,omitempty"`
	AvatarDecorationData json.RawMessage `json:"avatar_decoration_data,omitempty"`
}
type Member struct {
	User                       User     `json:"user"`
	GuildID                    string   `json:"guild_id,omitempty"`
	Nick                       *string  `json:"nick,omitempty"`
	Avatar                     *string  `json:"avatar,omitempty"`
	Roles                      []string `json:"roles,omitempty"`
	JoinedAt                   string   `json:"joined_at,omitempty"`
	PremiumSince               *string  `json:"premium_since,omitempty"`
	Deaf                       bool     `json:"deaf,omitempty"`
	Mute                       bool     `json:"mute,omitempty"`
	Flags                      uint64   `json:"flags,omitempty"`
	Pending                    bool     `json:"pending,omitempty"`
	Permissions                string   `json:"permissions,omitempty"`
	CommunicationDisabledUntil *string  `json:"communication_disabled_until,omitempty"`
}

type Envelope struct {
	ID                           string          `json:"id"`
	ApplicationID                string          `json:"application_id"`
	Type                         InteractionType `json:"type"`
	Token                        string          `json:"token"`
	Version                      int             `json:"version,omitempty"`
	GuildID                      string          `json:"guild_id,omitempty"`
	ChannelID                    string          `json:"channel_id,omitempty"`
	Member                       Member          `json:"member,omitempty"`
	User                         User            `json:"user,omitempty"`
	Guild                        json.RawMessage `json:"guild,omitempty"`
	Channel                      json.RawMessage `json:"channel,omitempty"`
	Data                         json.RawMessage `json:"data,omitempty"`
	Locale                       string          `json:"locale,omitempty"`
	GuildLocale                  string          `json:"guild_locale,omitempty"`
	AppPermissions               string          `json:"app_permissions,omitempty"`
	Message                      json.RawMessage `json:"message,omitempty"`
	Entitlements                 json.RawMessage `json:"entitlements,omitempty"`
	AuthorizingIntegrationOwners json.RawMessage `json:"authorizing_integration_owners,omitempty"`
	Context                      int             `json:"context,omitempty"`
	AttachmentSizeLimit          int64           `json:"attachment_size_limit,omitempty"`
}

type CommandOption struct {
	Name    string          `json:"name"`
	Type    uint8           `json:"type"`
	Value   json.RawMessage `json:"value,omitempty"`
	Options []CommandOption `json:"options,omitempty"`
	Focused bool            `json:"focused,omitempty"`
}
type CommandData struct {
	ID       string          `json:"id"`
	Name     string          `json:"name"`
	Type     uint8           `json:"type"`
	Options  []CommandOption `json:"options,omitempty"`
	Resolved json.RawMessage `json:"resolved,omitempty"`
	TargetID string          `json:"target_id,omitempty"`
}
type ComponentData struct {
	CustomID      string          `json:"custom_id"`
	ComponentType uint8           `json:"component_type"`
	Values        []string        `json:"values,omitempty"`
	Resolved      json.RawMessage `json:"resolved,omitempty"`
}
type ModalComponent struct {
	Type       uint8            `json:"type"`
	CustomID   string           `json:"custom_id,omitempty"`
	Value      string           `json:"value,omitempty"`
	Components []ModalComponent `json:"components,omitempty"`
}
type ModalData struct {
	CustomID   string           `json:"custom_id"`
	Components []ModalComponent `json:"components"`
}

type Callback struct {
	Type CallbackType  `json:"type"`
	Data *CallbackData `json:"data,omitempty"`
}
type CallbackData struct {
	Content    string      `json:"content,omitempty"`
	Flags      uint64      `json:"flags,omitempty"`
	Embeds     []Embed     `json:"embeds,omitempty"`
	Components []Component `json:"components,omitempty"`
	Choices    []Choice    `json:"choices,omitempty"`
	CustomID   string      `json:"custom_id,omitempty"`
	Title      string      `json:"title,omitempty"`
}
type Embed struct {
	Title       string       `json:"title,omitempty"`
	Description string       `json:"description,omitempty"`
	Color       int          `json:"color,omitempty"`
	Fields      []EmbedField `json:"fields,omitempty"`
}
type EmbedField struct {
	Name   string `json:"name"`
	Value  string `json:"value"`
	Inline bool   `json:"inline,omitempty"`
}
type Component struct {
	Type        uint8          `json:"type"`
	Style       uint8          `json:"style,omitempty"`
	Label       string         `json:"label,omitempty"`
	CustomID    string         `json:"custom_id,omitempty"`
	Placeholder string         `json:"placeholder,omitempty"`
	MinValues   int            `json:"min_values,omitempty"`
	MaxValues   int            `json:"max_values,omitempty"`
	Options     []SelectOption `json:"options,omitempty"`
	Components  []Component    `json:"components,omitempty"`
	Required    bool           `json:"required,omitempty"`
	MinLength   int            `json:"min_length,omitempty"`
	MaxLength   int            `json:"max_length,omitempty"`
}
type SelectOption struct {
	Label       string `json:"label"`
	Value       string `json:"value"`
	Description string `json:"description,omitempty"`
}
type Choice struct {
	Name  string `json:"name"`
	Value string `json:"value"`
}
