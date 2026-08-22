package domain

type AutoModerationRuleID Snowflake
type AutoModerationEventType int
type AutoModerationTriggerType int
type AutoModerationPresetType int
type AutoModerationActionType int

const (
	AutoModerationEventMessageSend       AutoModerationEventType   = 1
	AutoModerationEventMemberUpdate      AutoModerationEventType   = 2
	AutoModerationTriggerKeyword         AutoModerationTriggerType = 1
	AutoModerationTriggerSpam            AutoModerationTriggerType = 3
	AutoModerationTriggerKeywordPreset   AutoModerationTriggerType = 4
	AutoModerationTriggerMentionSpam     AutoModerationTriggerType = 5
	AutoModerationTriggerMemberProfile   AutoModerationTriggerType = 6
	AutoModerationActionBlockMessage     AutoModerationActionType  = 1
	AutoModerationActionSendAlert        AutoModerationActionType  = 2
	AutoModerationActionTimeout          AutoModerationActionType  = 3
	AutoModerationActionBlockInteraction AutoModerationActionType  = 4
)

type AutoModerationTriggerMetadata struct {
	KeywordFilter                []string                   `json:"keyword_filter,omitempty"`
	RegexPatterns                []string                   `json:"regex_patterns,omitempty"`
	Presets                      []AutoModerationPresetType `json:"presets,omitempty"`
	AllowList                    []string                   `json:"allow_list,omitempty"`
	MentionTotalLimit            *int                       `json:"mention_total_limit,omitempty"`
	MentionRaidProtectionEnabled *bool                      `json:"mention_raid_protection_enabled,omitempty"`
}
type AutoModerationActionMetadata struct {
	ChannelID       *ChannelID `json:"channel_id,omitempty"`
	DurationSeconds *int       `json:"duration_seconds,omitempty"`
	CustomMessage   *string    `json:"custom_message,omitempty"`
}
type AutoModerationAction struct {
	Type     AutoModerationActionType      `json:"type"`
	Metadata *AutoModerationActionMetadata `json:"metadata,omitempty"`
}
type AutoModerationRule struct {
	ID              AutoModerationRuleID          `json:"id"`
	GuildID         GuildID                       `json:"guild_id"`
	Name            string                        `json:"name"`
	CreatorID       UserID                        `json:"creator_id"`
	EventType       AutoModerationEventType       `json:"event_type"`
	TriggerType     AutoModerationTriggerType     `json:"trigger_type"`
	TriggerMetadata AutoModerationTriggerMetadata `json:"trigger_metadata"`
	Actions         []AutoModerationAction        `json:"actions"`
	Enabled         bool                          `json:"enabled"`
	ExemptRoles     []RoleID                      `json:"exempt_roles"`
	ExemptChannels  []ChannelID                   `json:"exempt_channels"`
}

type ListAutoModerationRules struct{ CommandContext }
type GetAutoModerationRule struct {
	CommandContext
	RuleID AutoModerationRuleID `json:"-"`
}
type CreateAutoModerationRule struct {
	CommandContext
	Name            string                         `json:"name"`
	EventType       AutoModerationEventType        `json:"event_type"`
	TriggerType     AutoModerationTriggerType      `json:"trigger_type"`
	TriggerMetadata *AutoModerationTriggerMetadata `json:"trigger_metadata,omitempty"`
	Actions         []AutoModerationAction         `json:"actions"`
	Enabled         *bool                          `json:"enabled,omitempty"`
	ExemptRoles     []RoleID                       `json:"exempt_roles,omitempty"`
	ExemptChannels  []ChannelID                    `json:"exempt_channels,omitempty"`
}
type ModifyAutoModerationRule struct {
	CommandContext
	RuleID          AutoModerationRuleID           `json:"-"`
	Name            *string                        `json:"name,omitempty"`
	EventType       *AutoModerationEventType       `json:"event_type,omitempty"`
	TriggerMetadata *AutoModerationTriggerMetadata `json:"trigger_metadata,omitempty"`
	Actions         *[]AutoModerationAction        `json:"actions,omitempty"`
	Enabled         *bool                          `json:"enabled,omitempty"`
	ExemptRoles     *[]RoleID                      `json:"exempt_roles,omitempty"`
	ExemptChannels  *[]ChannelID                   `json:"exempt_channels,omitempty"`
}
type DeleteAutoModerationRule struct {
	CommandContext
	RuleID AutoModerationRuleID `json:"-"`
}

func (ListAutoModerationRules) CommandName() string                 { return "list_auto_moderation_rules" }
func (ListAutoModerationRules) discordCommand()                     {}
func (ListAutoModerationRules) discordResult() []AutoModerationRule { return nil }
func (GetAutoModerationRule) CommandName() string                   { return "get_auto_moderation_rule" }
func (GetAutoModerationRule) discordCommand()                       {}
func (GetAutoModerationRule) discordResult() AutoModerationRule {
	return resultOf[AutoModerationRule]()
}
func (CreateAutoModerationRule) CommandName() string { return "create_auto_moderation_rule" }
func (CreateAutoModerationRule) discordCommand()     {}
func (CreateAutoModerationRule) discordResult() AutoModerationRule {
	return resultOf[AutoModerationRule]()
}
func (ModifyAutoModerationRule) CommandName() string { return "modify_auto_moderation_rule" }
func (ModifyAutoModerationRule) discordCommand()     {}
func (ModifyAutoModerationRule) discordResult() AutoModerationRule {
	return resultOf[AutoModerationRule]()
}
func (DeleteAutoModerationRule) CommandName() string        { return "delete_auto_moderation_rule" }
func (DeleteAutoModerationRule) discordCommand()            {}
func (DeleteAutoModerationRule) discordResult() EmptyResult { return EmptyResult{} }
