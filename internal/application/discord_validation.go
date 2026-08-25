package application

import (
	"encoding/json"
	"fmt"
	"math/big"
	"reflect"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/ken-kai/discord-control-experiments/internal/domain"
)

const (
	MaxJSONRequestBytes                    int64 = 1 << 20
	MaxJSONResponseBytes                   int64 = 8 << 20
	MaxErrorResponseBytes                  int64 = 64 << 10
	MaxAttachments                               = 10
	MaxAttachmentBytes                     int64 = 25 << 20
	MaxMultipartBytes                      int64 = 50 << 20
	MaxMessageContentLength                      = 2000
	MaxEmbeds                                    = 10
	MaxEmbedCharacters                           = 6000
	MaxEmbedFields                               = 25
	MaxAuditReasonLength                         = 512
	MaxChannelNameLength                         = 100
	MaxRoleNameLength                            = 100
	MaxWebhookNameLength                         = 80
	MaxApplicationCommandNameLength              = 32
	MaxApplicationCommandDescriptionLength       = 100
	MaxApplicationCommandOptions                 = 25
	MaxScheduledEventNameLength                  = 100
	MaxInviteCodeLength                          = 32
	MaxBulkDeleteMessages                        = 100
)

func validateCommand(command domain.Command) error {
	if command.GuildScope() == "" {
		return validation("guild_id", "required")
	}
	if err := validateSnowflake(string(command.GuildScope())); err != nil {
		return validation("guild_id", err.Error())
	}
	if err := validateTypedValues(reflect.ValueOf(command), "command"); err != nil {
		return err
	}
	context := commandContext(command)
	if context.AuditReason != nil {
		if !utf8.ValidString(*context.AuditReason) || utf8.RuneCountInString(*context.AuditReason) > MaxAuditReasonLength || strings.ContainsAny(*context.AuditReason, "\r\n") {
			return validation("audit_reason", "must be valid UTF-8, at most 512 characters, without CR/LF")
		}
	}
	if spec, _ := LookupCommandSpec(command.CommandName()); spec.Idempotency == IdempotencyRequired {
		if context.IdempotencyKey == "" || context.ReconciliationKey == "" {
			return validation("idempotency_key", "create commands require idempotency and reconciliation keys")
		}
	}
	if err := validateSpecific(command); err != nil {
		return err
	}
	encoded, err := json.Marshal(command)
	if err != nil {
		return validation("command", "cannot be encoded")
	}
	if int64(len(encoded)) > MaxJSONRequestBytes {
		return validation("command", "JSON payload exceeds limit")
	}
	return nil
}

func validation(field, rule string) error { return &domain.ValidationError{Field: field, Rule: rule} }
func validateSnowflake(value string) error {
	if value == "" {
		return fmt.Errorf("required")
	}
	if strings.TrimSpace(value) != value || strings.HasPrefix(value, "+") || strings.HasPrefix(value, "-") {
		return fmt.Errorf("must be an unsigned decimal string")
	}
	parsed, err := strconv.ParseUint(value, 10, 64)
	if err != nil || parsed == 0 {
		return fmt.Errorf("must be a non-zero uint64 decimal string")
	}
	return nil
}
func validateInviteCode(code string) error {
	if code == "" || len(code) > MaxInviteCodeLength {
		return fmt.Errorf("must contain 1-%d URL-safe characters", MaxInviteCodeLength)
	}
	for _, r := range code {
		if !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '-' || r == '_') {
			return fmt.Errorf("contains invalid characters")
		}
	}
	return nil
}
func validatePermissionBits(value domain.PermissionBits) error {
	text := string(value)
	parsed, ok := new(big.Int).SetString(text, 10)
	if !ok || parsed.Sign() < 0 || text == "" || strings.HasPrefix(text, "+") || (len(text) > 1 && text[0] == '0') {
		return fmt.Errorf("must be a canonical non-negative decimal integer")
	}
	return nil
}

var snowflakeTypeNames = map[string]bool{"Snowflake": true, "GuildID": true, "ChannelID": true, "RoleID": true, "UserID": true, "MessageID": true, "WebhookID": true, "ApplicationID": true, "ApplicationCommandID": true, "ScheduledEventID": true, "AutoModerationRuleID": true}

func validateTypedValues(value reflect.Value, path string) error {
	if !value.IsValid() {
		return nil
	}
	if value.Kind() == reflect.Pointer {
		if value.IsNil() {
			return nil
		}
		return validateTypedValues(value.Elem(), path)
	}
	if value.Type().PkgPath() == "github.com/ken-kai/discord-control-experiments/internal/domain" {
		switch {
		case snowflakeTypeNames[value.Type().Name()]:
			if err := validateSnowflake(value.String()); err != nil {
				return validation(path, err.Error())
			}
			return nil
		case value.Type().Name() == "PermissionBits":
			if err := validatePermissionBits(domain.PermissionBits(value.String())); err != nil {
				return validation(path, err.Error())
			}
			return nil
		case value.Type().Name() == "InviteCode":
			if err := validateInviteCode(value.String()); err != nil {
				return validation(path, err.Error())
			}
			return nil
		}
	}
	switch value.Kind() {
	case reflect.Struct:
		for i := 0; i < value.NumField(); i++ {
			field := value.Type().Field(i)
			if field.Name == "Reader" {
				continue
			}
			if err := validateTypedValues(value.Field(i), path+"."+field.Name); err != nil {
				return err
			}
		}
	case reflect.Slice, reflect.Array:
		for i := 0; i < value.Len(); i++ {
			if err := validateTypedValues(value.Index(i), fmt.Sprintf("%s[%d]", path, i)); err != nil {
				return err
			}
		}
	case reflect.String:
		if !utf8.ValidString(value.String()) {
			return validation(path, "must be valid UTF-8")
		}
	}
	return nil
}

func validateSpecific(command domain.Command) error {
	switch c := command.(type) {
	case domain.ModifyMember:
		if c.RemoveCommunicationDisabled != nil && *c.RemoveCommunicationDisabled && c.CommunicationDisabledUntil != nil {
			return validation("communication_disabled_until", "cannot set and remove together")
		}
	case domain.CreateChannel:
		if err := boundedName("name", c.Name, 1, MaxChannelNameLength); err != nil {
			return err
		}
		if !validChannelType(c.Type) {
			return validation("type", "unsupported channel type")
		}
		if err := validateForumTags(c.AvailableTags); err != nil {
			return err
		}
		return validateOverwrites(c.PermissionOverwrites)
	case domain.ModifyChannel:
		if c.Name != nil {
			if err := boundedName("name", *c.Name, 1, MaxChannelNameLength); err != nil {
				return err
			}
		}
		if c.Type != nil && !validChannelType(*c.Type) {
			return validation("type", "unsupported channel type")
		}
		if c.AvailableTags != nil {
			if err := validateForumTags(*c.AvailableTags); err != nil {
				return err
			}
		}
		if c.PermissionOverwrites != nil {
			return validateOverwrites(*c.PermissionOverwrites)
		}
	case domain.CreateRole:
		return boundedName("name", c.Name, 1, MaxRoleNameLength)
	case domain.CreateWebhook:
		return boundedName("name", c.Name, 1, MaxWebhookNameLength)
	case domain.CreateMessage:
		return validateMessage(c.Content, c.Embeds, c.Uploads)
	case domain.StartForumThread:
		if err := boundedName("name", c.Name, 1, 100); err != nil {
			return err
		}
		if err := boundedName("content", c.Content, 1, MaxMessageContentLength); err != nil {
			return err
		}
		if len(c.AppliedTags) > 5 {
			return validation("applied_tags", "must contain at most 5 tags")
		}
	case domain.CreatePoll:
		if err := boundedName("question", c.Question, 1, 300); err != nil {
			return err
		}
		if len(c.Answers) < 2 || len(c.Answers) > 10 || c.DurationHours < 1 || c.DurationHours > 768 {
			return validation("poll", "requires 2-10 answers and duration 1-768 hours")
		}
		for _, answer := range c.Answers {
			if err := boundedName("answer", answer.Text, 1, 55); err != nil {
				return err
			}
		}
	case domain.ExecuteConfiguredWebhook:
		if err := boundedName("alias", c.Alias, 1, 64); err != nil {
			return err
		}
		return boundedName("content", c.Content, 1, MaxMessageContentLength)
	case domain.ModifyMessage:
		var embeds []domain.Embed
		if c.Embeds != nil {
			embeds = *c.Embeds
		}
		return validateMessage(c.Content, embeds, c.Uploads)
	case domain.BulkDeleteMessages:
		if len(c.MessageIDs) < 2 || len(c.MessageIDs) > MaxBulkDeleteMessages || hasDuplicates(c.MessageIDs) {
			return validation("message_ids", "must contain 2-100 unique IDs")
		}
	case domain.SetChannelPermission:
		return validateOverwrites([]domain.PermissionOverwrite{c.Overwrite})
	case domain.CreateGuildApplicationCommand:
		return validateApplicationCommand(c.Command, 0)
	case domain.ModifyGuildApplicationCommand:
		return validateApplicationCommand(c.Command, 0)
	case domain.BulkOverwriteGuildApplicationCommands:
		if len(c.Commands) > 100 {
			return validation("commands", "must contain at most 100 commands")
		}
		for _, x := range c.Commands {
			if err := validateApplicationCommand(x, 0); err != nil {
				return err
			}
		}
	case domain.ModifyGuildOnboarding:
		if len(c.Prompts) > 50 {
			return validation("prompts", "must contain at most 50 prompts")
		}
		if c.Mode < 0 || c.Mode > 1 {
			return validation("mode", "must be 0 or 1")
		}
		for _, prompt := range c.Prompts {
			if err := boundedName("prompts.title", prompt.Title, 1, 100); err != nil {
				return err
			}
			if prompt.Type < 0 || prompt.Type > 1 {
				return validation("prompts.type", "must be 0 or 1")
			}
			if len(prompt.Options) > 100 {
				return validation("prompts.options", "must contain at most 100 options")
			}
			for _, option := range prompt.Options {
				if err := boundedName("prompts.options.title", option.Title, 1, 100); err != nil {
					return err
				}
			}
		}
	case domain.CreateScheduledEvent:
		if err := boundedName("name", c.Name, 1, MaxScheduledEventNameLength); err != nil {
			return err
		}
		if !c.ScheduledStartTime.After(time.Now()) {
			return validation("scheduled_start_time", "must be in the future")
		}
		if c.ScheduledEndTime != nil && !c.ScheduledEndTime.After(c.ScheduledStartTime) {
			return validation("scheduled_end_time", "must follow start time")
		}
		if c.EntityType == 3 && (c.EntityMetadata == nil || c.EntityMetadata.Location == "") {
			return validation("entity_metadata.location", "required for external events")
		}
		if c.EntityType != 3 && c.ChannelID == nil {
			return validation("channel_id", "required for voice or stage events")
		}
	case domain.CreateAutoModerationRule:
		return validateAutoModerationRule(c.Name, c.EventType, c.TriggerType, c.TriggerMetadata, c.Actions, c.ExemptRoles, c.ExemptChannels)
	case domain.ModifyAutoModerationRule:
		if c.Name != nil {
			if err := boundedName("name", *c.Name, 1, 100); err != nil {
				return err
			}
		}
		if c.EventType != nil && *c.EventType != domain.AutoModerationEventMessageSend && *c.EventType != domain.AutoModerationEventMemberUpdate {
			return validation("event_type", "unsupported event type")
		}
		if c.Actions != nil {
			if err := validateAutoModerationActions(*c.Actions, 0); err != nil {
				return err
			}
		}
		if c.ExemptRoles != nil && len(*c.ExemptRoles) > 20 {
			return validation("exempt_roles", "must contain at most 20 roles")
		}
		if c.ExemptChannels != nil && len(*c.ExemptChannels) > 50 {
			return validation("exempt_channels", "must contain at most 50 channels")
		}
	case domain.CreateInvite:
		if c.MaxAge != nil && (*c.MaxAge < 0 || *c.MaxAge > 604800) {
			return validation("max_age", "must be 0-604800")
		}
		if c.MaxUses != nil && (*c.MaxUses < 0 || *c.MaxUses > 100) {
			return validation("max_uses", "must be 0-100")
		}
	}
	return nil
}
func validateAutoModerationRule(name string, event domain.AutoModerationEventType, trigger domain.AutoModerationTriggerType, metadata *domain.AutoModerationTriggerMetadata, actions []domain.AutoModerationAction, roles []domain.RoleID, channels []domain.ChannelID) error {
	if err := boundedName("name", name, 1, 100); err != nil {
		return err
	}
	if event != domain.AutoModerationEventMessageSend && event != domain.AutoModerationEventMemberUpdate {
		return validation("event_type", "unsupported event type")
	}
	switch trigger {
	case domain.AutoModerationTriggerKeyword, domain.AutoModerationTriggerSpam, domain.AutoModerationTriggerKeywordPreset, domain.AutoModerationTriggerMentionSpam, domain.AutoModerationTriggerMemberProfile:
	default:
		return validation("trigger_type", "unsupported trigger type")
	}
	if event == domain.AutoModerationEventMemberUpdate && trigger != domain.AutoModerationTriggerMemberProfile {
		return validation("trigger_type", "member update requires member profile trigger")
	}
	if len(roles) > 20 {
		return validation("exempt_roles", "must contain at most 20 roles")
	}
	if len(channels) > 50 {
		return validation("exempt_channels", "must contain at most 50 channels")
	}
	if metadata != nil {
		keywordMaximum, allowMaximum := 0, 0
		switch trigger {
		case domain.AutoModerationTriggerKeyword, domain.AutoModerationTriggerMemberProfile:
			keywordMaximum, allowMaximum = 1000, 100
		case domain.AutoModerationTriggerKeywordPreset:
			allowMaximum = 1000
		}
		if len(metadata.KeywordFilter) > keywordMaximum {
			return validation("trigger_metadata.keyword_filter", "exceeds trigger limit")
		}
		if len(metadata.RegexPatterns) > 10 || (trigger != domain.AutoModerationTriggerKeyword && trigger != domain.AutoModerationTriggerMemberProfile && len(metadata.RegexPatterns) > 0) {
			return validation("trigger_metadata.regex_patterns", "exceeds or conflicts with trigger limit")
		}
		if len(metadata.AllowList) > allowMaximum {
			return validation("trigger_metadata.allow_list", "exceeds trigger limit")
		}
		for _, item := range metadata.KeywordFilter {
			if utf8.RuneCountInString(item) > 60 {
				return validation("trigger_metadata.keyword_filter", "keyword exceeds 60 characters")
			}
		}
		for _, item := range metadata.AllowList {
			if utf8.RuneCountInString(item) > 60 {
				return validation("trigger_metadata.allow_list", "keyword exceeds 60 characters")
			}
		}
		for _, item := range metadata.RegexPatterns {
			if utf8.RuneCountInString(item) > 260 {
				return validation("trigger_metadata.regex_patterns", "pattern exceeds 260 characters")
			}
		}
		if len(metadata.Presets) > 3 || (trigger != domain.AutoModerationTriggerKeywordPreset && len(metadata.Presets) > 0) {
			return validation("trigger_metadata.presets", "conflicts with trigger type")
		}
		for _, preset := range metadata.Presets {
			if preset < 1 || preset > 3 {
				return validation("trigger_metadata.presets", "unsupported preset")
			}
		}
		if metadata.MentionTotalLimit != nil && (trigger != domain.AutoModerationTriggerMentionSpam || *metadata.MentionTotalLimit < 1 || *metadata.MentionTotalLimit > 50) {
			return validation("trigger_metadata.mention_total_limit", "must be 1-50 for mention spam")
		}
	}
	return validateAutoModerationActions(actions, trigger)
}

func validateAutoModerationActions(actions []domain.AutoModerationAction, trigger domain.AutoModerationTriggerType) error {
	if len(actions) == 0 || len(actions) > 4 {
		return validation("actions", "must contain 1-4 actions")
	}
	seen := map[domain.AutoModerationActionType]bool{}
	for _, action := range actions {
		if action.Type < domain.AutoModerationActionBlockMessage || action.Type > domain.AutoModerationActionBlockInteraction || seen[action.Type] {
			return validation("actions.type", "must be supported and unique")
		}
		seen[action.Type] = true
		metadata := action.Metadata
		switch action.Type {
		case domain.AutoModerationActionBlockMessage:
			if metadata != nil && metadata.CustomMessage != nil && utf8.RuneCountInString(*metadata.CustomMessage) > 150 {
				return validation("actions.metadata.custom_message", "exceeds 150 characters")
			}
		case domain.AutoModerationActionSendAlert:
			if metadata == nil || metadata.ChannelID == nil {
				return validation("actions.metadata.channel_id", "required for alert action")
			}
		case domain.AutoModerationActionTimeout:
			if trigger != 0 && trigger != domain.AutoModerationTriggerKeyword && trigger != domain.AutoModerationTriggerMentionSpam {
				return validation("actions.type", "timeout conflicts with trigger type")
			}
			if metadata == nil || metadata.DurationSeconds == nil || *metadata.DurationSeconds < 1 || *metadata.DurationSeconds > 2419200 {
				return validation("actions.metadata.duration_seconds", "must be 1-2419200")
			}
		}
	}
	return nil
}

func boundedName(field, value string, min, max int) error {
	n := utf8.RuneCountInString(value)
	if !utf8.ValidString(value) || n < min || n > max {
		return validation(field, fmt.Sprintf("must contain %d-%d characters", min, max))
	}
	return nil
}
func validChannelType(t domain.ChannelType) bool {
	switch t {
	case 0, 2, 4, 5, 13, 15, 16:
		return true
	}
	return false
}
func validateForumTags(tags []domain.ForumTag) error {
	if len(tags) > 20 {
		return validation("available_tags", "must contain at most 20 tags")
	}
	for _, tag := range tags {
		if err := boundedName("available_tags.name", tag.Name, 1, 20); err != nil {
			return err
		}
	}
	return nil
}

func validateOverwrites(items []domain.PermissionOverwrite) error {
	seen := map[string]bool{}
	for _, x := range items {
		if x.Type != 0 && x.Type != 1 {
			return validation("permission_overwrites.type", "must be role or member")
		}
		if err := validatePermissionBits(x.Allow); err != nil {
			return validation("permission_overwrites.allow", err.Error())
		}
		if err := validatePermissionBits(x.Deny); err != nil {
			return validation("permission_overwrites.deny", err.Error())
		}
		a, _ := new(big.Int).SetString(string(x.Allow), 10)
		d, _ := new(big.Int).SetString(string(x.Deny), 10)
		if new(big.Int).And(a, d).Sign() != 0 {
			return validation("permission_overwrites", "allow and deny overlap")
		}
		k := string(x.ID)
		if seen[k] {
			return validation("permission_overwrites", "duplicate target")
		}
		seen[k] = true
	}
	return nil
}
func validateMessage(content *string, embeds []domain.Embed, uploads []domain.Upload) error {
	if content == nil && len(embeds) == 0 && len(uploads) == 0 {
		return validation("message", "content, embed, or attachment required")
	}
	if content != nil && utf8.RuneCountInString(*content) > MaxMessageContentLength {
		return validation("content", "exceeds 2000 characters")
	}
	if len(embeds) > MaxEmbeds {
		return validation("embeds", "exceeds 10")
	}
	total := 0
	for _, e := range embeds {
		if e.Title != nil {
			total += utf8.RuneCountInString(*e.Title)
		}
		if e.Description != nil {
			total += utf8.RuneCountInString(*e.Description)
		}
		if len(e.Fields) > MaxEmbedFields {
			return validation("embeds.fields", "exceeds 25")
		}
		for _, f := range e.Fields {
			total += utf8.RuneCountInString(f.Name) + utf8.RuneCountInString(f.Value)
		}
	}
	if total > MaxEmbedCharacters {
		return validation("embeds", "exceeds 6000 characters")
	}
	if len(uploads) > MaxAttachments {
		return validation("uploads", "exceeds 10")
	}
	var aggregate int64
	for _, u := range uploads {
		if u.Reader == nil || u.Size < 0 || u.Size > MaxAttachmentBytes {
			return validation("uploads", "reader and valid declared size required")
		}
		if u.Filename == "" || strings.ContainsAny(u.Filename, "/\\\x00\r\n") {
			return validation("uploads.filename", "must be a safe basename")
		}
		aggregate += u.Size
	}
	if aggregate > MaxMultipartBytes {
		return validation("uploads", "aggregate size exceeds limit")
	}
	return nil
}
func validateApplicationCommand(c domain.ApplicationCommandInput, depth int) error {
	if depth > 3 {
		return validation("options", "nesting exceeds 3")
	}
	if err := boundedName("name", c.Name, 1, MaxApplicationCommandNameLength); err != nil {
		return err
	}
	if utf8.RuneCountInString(c.Description) > MaxApplicationCommandDescriptionLength {
		return validation("description", "exceeds 100 characters")
	}
	if len(c.Options) > MaxApplicationCommandOptions {
		return validation("options", "exceeds 25")
	}
	for _, o := range c.Options {
		child := domain.ApplicationCommandInput{Name: o.Name, Description: o.Description, Options: o.Options}
		if err := validateApplicationCommand(child, depth+1); err != nil {
			return err
		}
	}
	return nil
}
func hasDuplicates[T ~string](items []T) bool {
	seen := map[T]bool{}
	for _, x := range items {
		if seen[x] {
			return true
		}
		seen[x] = true
	}
	return false
}
func commandContext(command domain.Command) domain.CommandContext {
	value := reflect.ValueOf(command)
	if value.Kind() == reflect.Pointer {
		value = value.Elem()
	}
	field := value.FieldByName("CommandContext")
	if field.IsValid() {
		return field.Interface().(domain.CommandContext)
	}
	return domain.CommandContext{}
}
