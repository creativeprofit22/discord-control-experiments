package domain

import (
	"errors"
	"fmt"
	"sort"
	"strings"
)

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

const (
	MaxAutoModerationActions           = 3
	MaxAutoModerationRoleExemptions    = 20
	MaxAutoModerationChannelExemptions = 50
	MaxAutoModerationKeywords          = 1000
	MaxAutoModerationRegexPatterns     = 10
	MaxAutoModerationAllowList         = 100
)

func ValidateDesiredAutoModerationRule(rule DesiredAutoModerationRule) error {
	if rule.LogicalID == "" || len(rule.LogicalID) > 64 || strings.TrimSpace(rule.Name) == "" || len(rule.Name) > 100 {
		return errors.New("AutoMod logical_id and name must be bounded and non-empty")
	}
	if rule.Mode != "" && rule.Mode != ManagementModeManaged && rule.Mode != ManagementModeObserved && rule.Mode != ManagementModeIgnored {
		return errors.New("AutoMod management mode is invalid")
	}
	if !validAutoModerationTrigger(rule.TriggerType) || !validAutoModerationEvent(rule.EventType, rule.TriggerType) {
		return errors.New("AutoMod event and trigger combination is invalid")
	}
	if len(rule.Actions) == 0 || len(rule.Actions) > MaxAutoModerationActions || len(rule.ExemptRoles) > MaxAutoModerationRoleExemptions || len(rule.ExemptChannels) > MaxAutoModerationChannelExemptions {
		return errors.New("AutoMod action or exemption count is invalid")
	}
	if err := validateAutoModerationMetadata(rule.TriggerType, rule.TriggerMetadata); err != nil {
		return err
	}
	seenActions := make(map[AutoModerationActionType]struct{}, len(rule.Actions))
	for _, action := range rule.Actions {
		if _, duplicate := seenActions[action.Type]; duplicate {
			return errors.New("AutoMod action types must be unique")
		}
		seenActions[action.Type] = struct{}{}
		if err := validateDesiredAutoModerationAction(rule.TriggerType, action); err != nil {
			return err
		}
	}
	if err := validateAutoModerationRefs(rule.ExemptRoles, ResourceRole); err != nil {
		return err
	}
	if err := validateAutoModerationRefs(rule.ExemptChannels, ResourceChannel); err != nil {
		return err
	}
	return nil
}

func NormalizeDesiredAutoModerationRule(rule DesiredAutoModerationRule) DesiredAutoModerationRule {
	rule.TriggerMetadata.KeywordFilter = sortedStrings(rule.TriggerMetadata.KeywordFilter)
	rule.TriggerMetadata.RegexPatterns = sortedStrings(rule.TriggerMetadata.RegexPatterns)
	rule.TriggerMetadata.AllowList = sortedStrings(rule.TriggerMetadata.AllowList)
	sort.Slice(rule.TriggerMetadata.Presets, func(i, j int) bool { return rule.TriggerMetadata.Presets[i] < rule.TriggerMetadata.Presets[j] })
	sort.Slice(rule.ExemptRoles, func(i, j int) bool { return rule.ExemptRoles[i].LogicalID < rule.ExemptRoles[j].LogicalID })
	sort.Slice(rule.ExemptChannels, func(i, j int) bool { return rule.ExemptChannels[i].LogicalID < rule.ExemptChannels[j].LogicalID })
	sort.Slice(rule.Actions, func(i, j int) bool { return rule.Actions[i].Type < rule.Actions[j].Type })
	return rule
}

func validAutoModerationTrigger(trigger AutoModerationTriggerType) bool {
	return trigger == AutoModerationTriggerKeyword || trigger == AutoModerationTriggerSpam || trigger == AutoModerationTriggerKeywordPreset || trigger == AutoModerationTriggerMentionSpam || trigger == AutoModerationTriggerMemberProfile
}

func validAutoModerationEvent(event AutoModerationEventType, trigger AutoModerationTriggerType) bool {
	if trigger == AutoModerationTriggerMemberProfile {
		return event == AutoModerationEventMemberUpdate
	}
	return event == AutoModerationEventMessageSend
}

func validateAutoModerationMetadata(trigger AutoModerationTriggerType, metadata AutoModerationTriggerMetadata) error {
	keywordCount, regexCount, allowCount := len(metadata.KeywordFilter), len(metadata.RegexPatterns), len(metadata.AllowList)
	maxAllowList := MaxAutoModerationAllowList
	if trigger == AutoModerationTriggerKeywordPreset {
		maxAllowList = MaxAutoModerationKeywords
	}
	if keywordCount > MaxAutoModerationKeywords || regexCount > MaxAutoModerationRegexPatterns || allowCount > maxAllowList {
		return errors.New("AutoMod trigger metadata exceeds Discord limits")
	}
	if !boundedUniqueStrings(metadata.KeywordFilter, 60) || !boundedUniqueStrings(metadata.RegexPatterns, 260) || !boundedUniqueStrings(metadata.AllowList, 60) {
		return errors.New("AutoMod trigger strings must be bounded and unique")
	}
	switch trigger {
	case AutoModerationTriggerKeyword, AutoModerationTriggerMemberProfile:
		if keywordCount+regexCount == 0 || len(metadata.Presets) != 0 || metadata.MentionTotalLimit != nil || metadata.MentionRaidProtectionEnabled != nil {
			return errors.New("invalid keyword trigger metadata")
		}
	case AutoModerationTriggerSpam:
		if keywordCount+regexCount+allowCount+len(metadata.Presets) != 0 || metadata.MentionTotalLimit != nil || metadata.MentionRaidProtectionEnabled != nil {
			return errors.New("spam trigger metadata must be empty")
		}
	case AutoModerationTriggerKeywordPreset:
		if keywordCount+regexCount != 0 || len(metadata.Presets) == 0 || len(metadata.Presets) > 3 || metadata.MentionTotalLimit != nil || metadata.MentionRaidProtectionEnabled != nil {
			return errors.New("invalid preset trigger metadata")
		}
		seen := map[AutoModerationPresetType]struct{}{}
		for _, preset := range metadata.Presets {
			if preset < 1 || preset > 3 {
				return errors.New("unsupported AutoMod preset")
			}
			if _, ok := seen[preset]; ok {
				return errors.New("duplicate AutoMod preset")
			}
			seen[preset] = struct{}{}
		}
	case AutoModerationTriggerMentionSpam:
		if keywordCount+regexCount+allowCount+len(metadata.Presets) != 0 || metadata.MentionTotalLimit == nil || *metadata.MentionTotalLimit < 1 || *metadata.MentionTotalLimit > 50 {
			return errors.New("mention limit must be between 1 and 50")
		}
	}
	return nil
}

func validateDesiredAutoModerationAction(trigger AutoModerationTriggerType, action DesiredAutoModerationAction) error {
	switch action.Type {
	case AutoModerationActionBlockMessage:
		if action.AlertChannel != nil || action.DurationSeconds != nil || action.CustomMessage != nil && len(*action.CustomMessage) > 150 {
			return errors.New("invalid block-message action metadata")
		}
	case AutoModerationActionSendAlert:
		if action.AlertChannel == nil || action.AlertChannel.Kind != ResourceChannel || action.AlertChannel.LogicalID == "" || action.DurationSeconds != nil || action.CustomMessage != nil {
			return errors.New("alert action requires one logical channel")
		}
	case AutoModerationActionTimeout:
		if trigger != AutoModerationTriggerKeyword && trigger != AutoModerationTriggerMentionSpam || action.DurationSeconds == nil || *action.DurationSeconds < 1 || *action.DurationSeconds > int(MaxDiscordTimeout.Seconds()) || action.AlertChannel != nil || action.CustomMessage != nil {
			return errors.New("invalid timeout action")
		}
	case AutoModerationActionBlockInteraction:
		if trigger != AutoModerationTriggerMemberProfile || action.AlertChannel != nil || action.DurationSeconds != nil || action.CustomMessage != nil {
			return errors.New("block-interaction is only valid for member-profile rules")
		}
	default:
		return errors.New("unsupported AutoMod action")
	}
	return nil
}

func validateAutoModerationRefs(refs []StableResourceRef, kind ResourceKind) error {
	seen := make(map[string]struct{}, len(refs))
	for _, ref := range refs {
		if ref.Kind != kind || ref.LogicalID == "" {
			return fmt.Errorf("AutoMod exemption must reference a %s", kind)
		}
		if _, duplicate := seen[ref.LogicalID]; duplicate {
			return errors.New("duplicate AutoMod exemption")
		}
		seen[ref.LogicalID] = struct{}{}
	}
	return nil
}

func boundedUniqueStrings(values []string, maximum int) bool {
	seen := make(map[string]struct{}, len(values))
	for _, value := range values {
		if value == "" || len(value) > maximum {
			return false
		}
		if _, duplicate := seen[value]; duplicate {
			return false
		}
		seen[value] = struct{}{}
	}
	return true
}

func sortedStrings(values []string) []string {
	result := append([]string(nil), values...)
	sort.Strings(result)
	return result
}
