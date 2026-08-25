package domain

import "time"

// Command is deliberately sealed: only this package can add Discord capabilities.
type Command interface {
	CommandName() string
	GuildScope() GuildID
	discordCommand()
}

type DiscordCommand[R any] interface {
	Command
	discordResult() R
}

type CommandContext struct {
	GuildID           GuildID
	AuditReason       *string
	IdempotencyKey    IdempotencyKey
	ReconciliationKey ReconciliationKey
}

func (c CommandContext) GuildScope() GuildID { return c.GuildID }

type GetCurrentUser struct{ CommandContext }

type InspectGuild struct {
	CommandContext
	ApplicationID *ApplicationID
}
type ListGuildChannels struct{ CommandContext }
type GetChannel struct {
	CommandContext
	ChannelID ChannelID
}
type CreateChannel struct {
	CommandContext
	Name                          string
	Type                          ChannelType
	Topic                         *string
	ParentID                      *ChannelID
	NSFW                          *bool
	RateLimitPerUser              *int
	Bitrate                       *int
	UserLimit                     *int
	RTCRegion                     *string
	VideoQualityMode              *int
	DefaultAutoArchiveDuration    *int
	DefaultThreadRateLimitPerUser *int
	AvailableTags                 []ForumTag
	DefaultReactionEmoji          *DefaultReaction
	DefaultSortOrder              *int
	DefaultForumLayout            *int
	PermissionOverwrites          []PermissionOverwrite
}
type ModifyChannel struct {
	CommandContext
	ChannelID                     ChannelID
	Name                          *string
	Type                          *ChannelType
	Topic                         *string
	ParentID                      *ChannelID
	NSFW                          *bool
	RateLimitPerUser              *int
	Bitrate                       *int
	UserLimit                     *int
	RTCRegion                     *string
	VideoQualityMode              *int
	DefaultAutoArchiveDuration    *int
	DefaultThreadRateLimitPerUser *int
	AvailableTags                 *[]ForumTag
	DefaultReactionEmoji          *DefaultReaction
	DefaultSortOrder              *int
	DefaultForumLayout            *int
	PermissionOverwrites          *[]PermissionOverwrite
}
type DeleteChannel struct {
	CommandContext
	ChannelID ChannelID
}
type ChannelPosition struct {
	ID              ChannelID  `json:"id"`
	Position        *int       `json:"position,omitempty"`
	ParentID        *ChannelID `json:"parent_id,omitempty"`
	LockPermissions *bool      `json:"lock_permissions,omitempty"`
}
type ReorderChannels struct {
	CommandContext
	Positions []ChannelPosition
}

type ListGuildRoles struct{ CommandContext }
type CreateRole struct {
	CommandContext
	Name        string
	Permissions *PermissionBits
	Color       *int
	Hoist       *bool
	Mentionable *bool
}
type ModifyRole struct {
	CommandContext
	RoleID      RoleID
	Name        *string
	Permissions *PermissionBits
	Color       *int
	Hoist       *bool
	Mentionable *bool
}
type DeleteRole struct {
	CommandContext
	RoleID RoleID
}
type RolePosition struct {
	ID       RoleID `json:"id"`
	Position int    `json:"position"`
}
type ReorderRoles struct {
	CommandContext
	Positions []RolePosition
}

type SetChannelPermission struct {
	CommandContext
	ChannelID ChannelID
	Overwrite PermissionOverwrite
}
type DeleteChannelPermission struct {
	CommandContext
	ChannelID   ChannelID
	OverwriteID Snowflake
}

type ListMembers struct {
	CommandContext
	Limit *int
	After *UserID
}
type GetMember struct {
	CommandContext
	UserID UserID
}
type ModifyMember struct {
	CommandContext
	UserID                      UserID
	Nick                        *string
	Roles                       *[]RoleID
	Mute                        *bool
	Deaf                        *bool
	ChannelID                   *ChannelID
	CommunicationDisabledUntil  *time.Time
	RemoveCommunicationDisabled *bool
}
type AddMemberRole struct {
	CommandContext
	UserID UserID
	RoleID RoleID
}
type RemoveMemberRole struct {
	CommandContext
	UserID UserID
	RoleID RoleID
}
type RemoveMember struct {
	CommandContext
	UserID UserID
}
type CreateMemberBan struct {
	CommandContext
	UserID               UserID
	DeleteMessageSeconds *int
}
type DeleteMemberBan struct {
	CommandContext
	UserID UserID
}

type ListMessages struct {
	CommandContext
	ChannelID ChannelID
	Around    *MessageID
	Before    *MessageID
	After     *MessageID
	Limit     *int
}
type GetMessage struct {
	CommandContext
	ChannelID ChannelID
	MessageID MessageID
}
type CreateMessage struct {
	CommandContext
	ChannelID        ChannelID
	Content          *string
	Embeds           []Embed
	Uploads          []Upload
	AllowedMentions  *AllowedMentions
	MessageReference *MessageReference
}
type StartForumThread struct {
	CommandContext
	ChannelID           ChannelID
	Name                string
	Content             string
	AutoArchiveDuration *int
	RateLimitPerUser    *int
	AppliedTags         []string
	AllowedMentions     *AllowedMentions
}

type PollAnswer struct {
	Text string `json:"text"`
}
type CreatePoll struct {
	CommandContext
	ChannelID        ChannelID
	Content          *string
	Question         string
	Answers          []PollAnswer
	DurationHours    int
	AllowMultiselect bool
	AllowedMentions  *AllowedMentions
}

type ExecuteConfiguredWebhook struct {
	CommandContext
	Alias           string
	Content         string
	AllowedMentions *AllowedMentions
}

type ModifyMessage struct {
	CommandContext
	ChannelID       ChannelID
	MessageID       MessageID
	Content         *string
	Embeds          *[]Embed
	Uploads         []Upload
	AllowedMentions *AllowedMentions
}
type DeleteMessage struct {
	CommandContext
	ChannelID ChannelID
	MessageID MessageID
}
type BulkDeleteMessages struct {
	CommandContext
	ChannelID  ChannelID
	MessageIDs []MessageID
}
type PinMessage struct {
	CommandContext
	ChannelID ChannelID
	MessageID MessageID
}
type UnpinMessage struct {
	CommandContext
	ChannelID ChannelID
	MessageID MessageID
}
type AllowedMentions struct {
	Parse       []string `json:"parse,omitempty"`
	Roles       []RoleID `json:"roles,omitempty"`
	Users       []UserID `json:"users,omitempty"`
	RepliedUser *bool    `json:"replied_user,omitempty"`
}
type MessageReference struct {
	MessageID       MessageID  `json:"message_id"`
	ChannelID       *ChannelID `json:"channel_id,omitempty"`
	GuildID         *GuildID   `json:"guild_id,omitempty"`
	FailIfNotExists *bool      `json:"fail_if_not_exists,omitempty"`
}

type ListGuildWebhooks struct{ CommandContext }
type ListChannelWebhooks struct {
	CommandContext
	ChannelID ChannelID
}
type CreateWebhook struct {
	CommandContext
	ChannelID ChannelID
	Name      string
}
type GetWebhook struct {
	CommandContext
	WebhookID WebhookID
}
type ModifyWebhook struct {
	CommandContext
	WebhookID WebhookID
	Name      *string
	ChannelID *ChannelID
}
type DeleteWebhook struct {
	CommandContext
	WebhookID WebhookID
}

type ListGuildApplicationCommands struct {
	CommandContext
	ApplicationID     ApplicationID
	WithLocalizations *bool
}
type GetGuildApplicationCommand struct {
	CommandContext
	ApplicationID        ApplicationID
	ApplicationCommandID ApplicationCommandID
}
type CreateGuildApplicationCommand struct {
	CommandContext
	ApplicationID ApplicationID
	Command       ApplicationCommandInput
}
type ModifyGuildApplicationCommand struct {
	CommandContext
	ApplicationID        ApplicationID
	ApplicationCommandID ApplicationCommandID
	Command              ApplicationCommandInput
}
type DeleteGuildApplicationCommand struct {
	CommandContext
	ApplicationID        ApplicationID
	ApplicationCommandID ApplicationCommandID
}
type BulkOverwriteGuildApplicationCommands struct {
	CommandContext
	ApplicationID ApplicationID
	Commands      []ApplicationCommandInput
}
type ApplicationCommandInput struct {
	Type                     *ApplicationCommandType    `json:"type,omitempty"`
	Name                     string                     `json:"name"`
	Description              string                     `json:"description"`
	Options                  []ApplicationCommandOption `json:"options,omitempty"`
	DefaultMemberPermissions *PermissionBits            `json:"default_member_permissions,omitempty"`
	DMPermission             *bool                      `json:"dm_permission,omitempty"`
	NSFW                     *bool                      `json:"nsfw,omitempty"`
}

type GetGuildOnboarding struct{ CommandContext }

type ModifyGuildOnboarding struct {
	CommandContext
	Prompts           []OnboardingPromptInput
	DefaultChannelIDs []ChannelID
	Enabled           bool
	Mode              int
}

type OnboardingPromptInput struct {
	ID           *Snowflake              `json:"id,omitempty"`
	Type         int                     `json:"type"`
	Options      []OnboardingOptionInput `json:"options"`
	Title        string                  `json:"title"`
	SingleSelect bool                    `json:"single_select"`
	Required     bool                    `json:"required"`
	InOnboarding bool                    `json:"in_onboarding"`
}

type OnboardingOptionInput struct {
	ID            *Snowflake  `json:"id,omitempty"`
	ChannelIDs    []ChannelID `json:"channel_ids"`
	RoleIDs       []RoleID    `json:"role_ids"`
	EmojiID       *Snowflake  `json:"emoji_id,omitempty"`
	EmojiName     *string     `json:"emoji_name,omitempty"`
	EmojiAnimated *bool       `json:"emoji_animated,omitempty"`
	Title         string      `json:"title"`
	Description   *string     `json:"description,omitempty"`
}
type ListGuildInvites struct{ CommandContext }
type ListChannelInvites struct {
	CommandContext
	ChannelID ChannelID
}
type GetInvite struct {
	CommandContext
	Code                  InviteCode
	WithCounts            *bool
	WithExpiration        *bool
	GuildScheduledEventID *ScheduledEventID
}
type CreateInvite struct {
	CommandContext
	ChannelID           ChannelID
	MaxAge              *int
	MaxUses             *int
	Temporary           *bool
	Unique              *bool
	TargetType          *int
	TargetUserID        *UserID
	TargetApplicationID *ApplicationID
}
type DeleteInvite struct {
	CommandContext
	Code InviteCode
}

type ListScheduledEvents struct {
	CommandContext
	WithUserCount *bool
}
type GetScheduledEvent struct {
	CommandContext
	ScheduledEventID ScheduledEventID
	WithUserCount    *bool
}
type CreateScheduledEvent struct {
	CommandContext
	ChannelID          *ChannelID
	EntityMetadata     *ScheduledEventMetadata
	Name               string
	PrivacyLevel       ScheduledEventPrivacyLevel
	ScheduledStartTime time.Time
	ScheduledEndTime   *time.Time
	Description        *string
	EntityType         ScheduledEventEntityType
}
type ModifyScheduledEvent struct {
	CommandContext
	ScheduledEventID   ScheduledEventID
	ChannelID          *ChannelID
	EntityMetadata     *ScheduledEventMetadata
	Name               *string
	PrivacyLevel       *ScheduledEventPrivacyLevel
	ScheduledStartTime *time.Time
	ScheduledEndTime   *time.Time
	Description        *string
	EntityType         *ScheduledEventEntityType
	Status             *int
}
type DeleteScheduledEvent struct {
	CommandContext
	ScheduledEventID ScheduledEventID
}

// Command/result methods form the compile-time sealed catalog.
func resultOf[R any]() (zero R) { return zero }

func (StartForumThread) CommandName() string            { return "start_forum_thread" }
func (StartForumThread) discordCommand()                {}
func (StartForumThread) discordResult() Channel         { return Channel{} }
func (CreatePoll) CommandName() string                  { return "create_poll" }
func (CreatePoll) discordCommand()                      {}
func (CreatePoll) discordResult() Message               { return Message{} }
func (ExecuteConfiguredWebhook) CommandName() string    { return "execute_configured_webhook" }
func (ExecuteConfiguredWebhook) discordCommand()        {}
func (ExecuteConfiguredWebhook) discordResult() Message { return Message{} }

func (GetCurrentUser) CommandName() string                               { return "get_current_user" }
func (GetCurrentUser) discordCommand()                                   {}
func (GetCurrentUser) discordResult() User                               { return User{} }
func (InspectGuild) CommandName() string                                 { return "inspect_guild" }
func (InspectGuild) discordCommand()                                     {}
func (InspectGuild) discordResult() GuildInspection                      { return resultOf[GuildInspection]() }
func (ListGuildChannels) CommandName() string                            { return "list_guild_channels" }
func (ListGuildChannels) discordCommand()                                {}
func (ListGuildChannels) discordResult() []Channel                       { return nil }
func (GetChannel) CommandName() string                                   { return "get_channel" }
func (GetChannel) discordCommand()                                       {}
func (GetChannel) discordResult() Channel                                { return resultOf[Channel]() }
func (CreateChannel) CommandName() string                                { return "create_channel" }
func (CreateChannel) discordCommand()                                    {}
func (CreateChannel) discordResult() Channel                             { return resultOf[Channel]() }
func (ModifyChannel) CommandName() string                                { return "modify_channel" }
func (ModifyChannel) discordCommand()                                    {}
func (ModifyChannel) discordResult() Channel                             { return resultOf[Channel]() }
func (DeleteChannel) CommandName() string                                { return "delete_channel" }
func (DeleteChannel) discordCommand()                                    {}
func (DeleteChannel) discordResult() DeleteResult                        { return resultOf[DeleteResult]() }
func (ReorderChannels) CommandName() string                              { return "reorder_channels" }
func (ReorderChannels) discordCommand()                                  {}
func (ReorderChannels) discordResult() []Channel                         { return nil }
func (ListGuildRoles) CommandName() string                               { return "list_guild_roles" }
func (ListGuildRoles) discordCommand()                                   {}
func (ListGuildRoles) discordResult() []Role                             { return nil }
func (CreateRole) CommandName() string                                   { return "create_role" }
func (CreateRole) discordCommand()                                       {}
func (CreateRole) discordResult() Role                                   { return resultOf[Role]() }
func (ModifyRole) CommandName() string                                   { return "modify_role" }
func (ModifyRole) discordCommand()                                       {}
func (ModifyRole) discordResult() Role                                   { return resultOf[Role]() }
func (DeleteRole) CommandName() string                                   { return "delete_role" }
func (DeleteRole) discordCommand()                                       {}
func (DeleteRole) discordResult() DeleteResult                           { return resultOf[DeleteResult]() }
func (ReorderRoles) CommandName() string                                 { return "reorder_roles" }
func (ReorderRoles) discordCommand()                                     {}
func (ReorderRoles) discordResult() []Role                               { return nil }
func (SetChannelPermission) CommandName() string                         { return "set_channel_permission" }
func (SetChannelPermission) discordCommand()                             {}
func (SetChannelPermission) discordResult() EmptyResult                  { return EmptyResult{} }
func (DeleteChannelPermission) CommandName() string                      { return "delete_channel_permission" }
func (DeleteChannelPermission) discordCommand()                          {}
func (DeleteChannelPermission) discordResult() EmptyResult               { return EmptyResult{} }
func (ListMembers) CommandName() string                                  { return "list_members" }
func (ListMembers) discordCommand()                                      {}
func (ListMembers) discordResult() []Member                              { return nil }
func (GetMember) CommandName() string                                    { return "get_member" }
func (GetMember) discordCommand()                                        {}
func (GetMember) discordResult() Member                                  { return resultOf[Member]() }
func (ModifyMember) CommandName() string                                 { return "modify_member" }
func (ModifyMember) discordCommand()                                     {}
func (ModifyMember) discordResult() Member                               { return resultOf[Member]() }
func (AddMemberRole) CommandName() string                                { return "add_member_role" }
func (AddMemberRole) discordCommand()                                    {}
func (AddMemberRole) discordResult() EmptyResult                         { return EmptyResult{} }
func (RemoveMemberRole) CommandName() string                             { return "remove_member_role" }
func (RemoveMemberRole) discordCommand()                                 {}
func (RemoveMemberRole) discordResult() EmptyResult                      { return EmptyResult{} }
func (RemoveMember) CommandName() string                                 { return "remove_member" }
func (RemoveMember) discordCommand()                                     {}
func (RemoveMember) discordResult() EmptyResult                          { return EmptyResult{} }
func (CreateMemberBan) CommandName() string                              { return "create_member_ban" }
func (CreateMemberBan) discordCommand()                                  {}
func (CreateMemberBan) discordResult() EmptyResult                       { return EmptyResult{} }
func (DeleteMemberBan) CommandName() string                              { return "delete_member_ban" }
func (DeleteMemberBan) discordCommand()                                  {}
func (DeleteMemberBan) discordResult() EmptyResult                       { return EmptyResult{} }
func (ListMessages) CommandName() string                                 { return "list_messages" }
func (ListMessages) discordCommand()                                     {}
func (ListMessages) discordResult() []Message                            { return nil }
func (GetMessage) CommandName() string                                   { return "get_message" }
func (GetMessage) discordCommand()                                       {}
func (GetMessage) discordResult() Message                                { return resultOf[Message]() }
func (CreateMessage) CommandName() string                                { return "create_message" }
func (CreateMessage) discordCommand()                                    {}
func (CreateMessage) discordResult() Message                             { return resultOf[Message]() }
func (ModifyMessage) CommandName() string                                { return "modify_message" }
func (ModifyMessage) discordCommand()                                    {}
func (ModifyMessage) discordResult() Message                             { return resultOf[Message]() }
func (DeleteMessage) CommandName() string                                { return "delete_message" }
func (DeleteMessage) discordCommand()                                    {}
func (DeleteMessage) discordResult() EmptyResult                         { return EmptyResult{} }
func (BulkDeleteMessages) CommandName() string                           { return "bulk_delete_messages" }
func (BulkDeleteMessages) discordCommand()                               {}
func (BulkDeleteMessages) discordResult() BulkDeleteResult               { return resultOf[BulkDeleteResult]() }
func (PinMessage) CommandName() string                                   { return "pin_message" }
func (PinMessage) discordCommand()                                       {}
func (PinMessage) discordResult() EmptyResult                            { return EmptyResult{} }
func (UnpinMessage) CommandName() string                                 { return "unpin_message" }
func (UnpinMessage) discordCommand()                                     {}
func (UnpinMessage) discordResult() EmptyResult                          { return EmptyResult{} }
func (ListGuildWebhooks) CommandName() string                            { return "list_guild_webhooks" }
func (ListGuildWebhooks) discordCommand()                                {}
func (ListGuildWebhooks) discordResult() []Webhook                       { return nil }
func (ListChannelWebhooks) CommandName() string                          { return "list_channel_webhooks" }
func (ListChannelWebhooks) discordCommand()                              {}
func (ListChannelWebhooks) discordResult() []Webhook                     { return nil }
func (CreateWebhook) CommandName() string                                { return "create_webhook" }
func (CreateWebhook) discordCommand()                                    {}
func (CreateWebhook) discordResult() Webhook                             { return resultOf[Webhook]() }
func (GetWebhook) CommandName() string                                   { return "get_webhook" }
func (GetWebhook) discordCommand()                                       {}
func (GetWebhook) discordResult() Webhook                                { return resultOf[Webhook]() }
func (ModifyWebhook) CommandName() string                                { return "modify_webhook" }
func (ModifyWebhook) discordCommand()                                    {}
func (ModifyWebhook) discordResult() Webhook                             { return resultOf[Webhook]() }
func (DeleteWebhook) CommandName() string                                { return "delete_webhook" }
func (DeleteWebhook) discordCommand()                                    {}
func (DeleteWebhook) discordResult() EmptyResult                         { return EmptyResult{} }
func (ListGuildApplicationCommands) CommandName() string                 { return "list_guild_application_commands" }
func (ListGuildApplicationCommands) discordCommand()                     {}
func (ListGuildApplicationCommands) discordResult() []ApplicationCommand { return nil }
func (GetGuildApplicationCommand) CommandName() string                   { return "get_guild_application_command" }
func (GetGuildApplicationCommand) discordCommand()                       {}
func (GetGuildApplicationCommand) discordResult() ApplicationCommand {
	return resultOf[ApplicationCommand]()
}
func (CreateGuildApplicationCommand) CommandName() string { return "create_guild_application_command" }
func (CreateGuildApplicationCommand) discordCommand()     {}
func (CreateGuildApplicationCommand) discordResult() ApplicationCommand {
	return resultOf[ApplicationCommand]()
}
func (ModifyGuildApplicationCommand) CommandName() string { return "modify_guild_application_command" }
func (ModifyGuildApplicationCommand) discordCommand()     {}
func (ModifyGuildApplicationCommand) discordResult() ApplicationCommand {
	return resultOf[ApplicationCommand]()
}
func (DeleteGuildApplicationCommand) CommandName() string        { return "delete_guild_application_command" }
func (DeleteGuildApplicationCommand) discordCommand()            {}
func (DeleteGuildApplicationCommand) discordResult() EmptyResult { return EmptyResult{} }
func (BulkOverwriteGuildApplicationCommands) CommandName() string {
	return "bulk_overwrite_guild_application_commands"
}
func (BulkOverwriteGuildApplicationCommands) discordCommand()                     {}
func (BulkOverwriteGuildApplicationCommands) discordResult() []ApplicationCommand { return nil }
func (GetGuildOnboarding) CommandName() string                                    { return "get_guild_onboarding" }
func (GetGuildOnboarding) discordCommand()                                        {}
func (GetGuildOnboarding) discordResult() Onboarding                              { return resultOf[Onboarding]() }
func (ModifyGuildOnboarding) CommandName() string                                 { return "modify_guild_onboarding" }
func (ModifyGuildOnboarding) discordCommand()                                     {}
func (ModifyGuildOnboarding) discordResult() Onboarding                           { return resultOf[Onboarding]() }
func (ListGuildInvites) CommandName() string                                      { return "list_guild_invites" }
func (ListGuildInvites) discordCommand()                                          {}
func (ListGuildInvites) discordResult() []Invite                                  { return nil }
func (ListChannelInvites) CommandName() string                                    { return "list_channel_invites" }
func (ListChannelInvites) discordCommand()                                        {}
func (ListChannelInvites) discordResult() []Invite                                { return nil }
func (GetInvite) CommandName() string                                             { return "get_invite" }
func (GetInvite) discordCommand()                                                 {}
func (GetInvite) discordResult() Invite                                           { return resultOf[Invite]() }
func (CreateInvite) CommandName() string                                          { return "create_invite" }
func (CreateInvite) discordCommand()                                              {}
func (CreateInvite) discordResult() Invite                                        { return resultOf[Invite]() }
func (DeleteInvite) CommandName() string                                          { return "delete_invite" }
func (DeleteInvite) discordCommand()                                              {}
func (DeleteInvite) discordResult() Invite                                        { return resultOf[Invite]() }
func (ListScheduledEvents) CommandName() string                                   { return "list_scheduled_events" }
func (ListScheduledEvents) discordCommand()                                       {}
func (ListScheduledEvents) discordResult() []ScheduledEvent                       { return nil }
func (GetScheduledEvent) CommandName() string                                     { return "get_scheduled_event" }
func (GetScheduledEvent) discordCommand()                                         {}
func (GetScheduledEvent) discordResult() ScheduledEvent                           { return resultOf[ScheduledEvent]() }
func (CreateScheduledEvent) CommandName() string                                  { return "create_scheduled_event" }
func (CreateScheduledEvent) discordCommand()                                      {}
func (CreateScheduledEvent) discordResult() ScheduledEvent                        { return resultOf[ScheduledEvent]() }
func (ModifyScheduledEvent) CommandName() string                                  { return "modify_scheduled_event" }
func (ModifyScheduledEvent) discordCommand()                                      {}
func (ModifyScheduledEvent) discordResult() ScheduledEvent                        { return resultOf[ScheduledEvent]() }
func (DeleteScheduledEvent) CommandName() string                                  { return "delete_scheduled_event" }
func (DeleteScheduledEvent) discordCommand()                                      {}
func (DeleteScheduledEvent) discordResult() EmptyResult                           { return EmptyResult{} }
