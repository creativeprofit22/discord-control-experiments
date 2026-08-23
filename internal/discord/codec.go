package discord

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"reflect"
	"strings"
	"unicode"

	"github.com/ken-kai/discord-control-experiments/internal/domain"
)

func encodeJSONCommand(command domain.Command) (io.Reader, string, error) {
	payload, ok := commandPayload(command)
	if !ok {
		return nil, "", nil
	}
	encoded, err := json.Marshal(payload)
	if err != nil {
		return nil, "", fmt.Errorf("encode Discord request: %w", err)
	}
	return bytes.NewReader(encoded), "application/json", nil
}
func commandPayload(command domain.Command) (any, bool) {
	switch c := command.(type) {
	case domain.CreateGuildApplicationCommand:
		return c.Command, true
	case domain.ModifyGuildApplicationCommand:
		return c.Command, true
	case domain.BulkOverwriteGuildApplicationCommands:
		return c.Commands, true
	case domain.BulkDeleteMessages:
		return map[string]any{"messages": c.MessageIDs}, true
	case domain.SetChannelPermission:
		return map[string]any{"type": c.Overwrite.Type, "allow": c.Overwrite.Allow, "deny": c.Overwrite.Deny}, true
	case domain.CreateMessage:
		return messagePayload(c), true
	case domain.ModifyMember:
		return modifyMemberPayload(c), true
	}
	specBody := map[string]bool{"create_channel": true, "modify_channel": true, "reorder_channels": true, "create_role": true, "modify_role": true, "reorder_roles": true, "modify_member": true, "create_member_ban": true, "modify_message": true, "create_webhook": true, "modify_webhook": true, "modify_guild_onboarding": true, "create_invite": true, "create_scheduled_event": true, "modify_scheduled_event": true, "create_auto_moderation_rule": true, "modify_auto_moderation_rule": true}
	if !specBody[command.CommandName()] {
		return nil, false
	}
	return structPayload(command), true
}
func structPayload(command domain.Command) map[string]any {
	value := reflect.ValueOf(command)
	if value.Kind() == reflect.Pointer {
		value = value.Elem()
	}
	typ := value.Type()
	out := map[string]any{}
	pathFields := map[string]bool{"CommandContext": true, "GuildID": true, "ChannelID": true, "RoleID": true, "UserID": true, "MessageID": true, "WebhookID": true, "ApplicationID": true, "ApplicationCommandID": true, "ScheduledEventID": true, "OverwriteID": true, "RuleID": true}
	for i := 0; i < value.NumField(); i++ {
		field := typ.Field(i)
		if pathFields[field.Name] {
			continue
		}
		v := value.Field(i)
		if v.Kind() == reflect.Pointer && v.IsNil() {
			continue
		}
		if (v.Kind() == reflect.Slice || v.Kind() == reflect.Map) && v.IsNil() {
			continue
		}
		out[snake(field.Name)] = v.Interface()
	}
	return out
}
func modifyMemberPayload(c domain.ModifyMember) map[string]any {
	out := structPayload(c)
	delete(out, "remove_communication_disabled")
	if c.RemoveCommunicationDisabled != nil && *c.RemoveCommunicationDisabled {
		out["communication_disabled_until"] = nil
	}
	return out
}

func messagePayload(c domain.CreateMessage) map[string]any {
	out := map[string]any{}
	if c.Content != nil {
		out["content"] = *c.Content
	}
	if len(c.Embeds) > 0 {
		out["embeds"] = c.Embeds
	}
	if c.AllowedMentions != nil {
		out["allowed_mentions"] = c.AllowedMentions
	}
	if c.MessageReference != nil {
		out["message_reference"] = c.MessageReference
	}
	nonce := deterministicNonce(c.CommandContext)
	out["nonce"] = nonce
	out["enforce_nonce"] = true
	if len(c.Uploads) > 0 {
		attachments := make([]map[string]any, len(c.Uploads))
		for i, u := range c.Uploads {
			attachments[i] = map[string]any{"id": i, "filename": normalizeFilename(u.Filename)}
			if u.Description != nil {
				attachments[i]["description"] = *u.Description
			}
		}
		out["attachments"] = attachments
	}
	return out
}
func deterministicNonce(c domain.CommandContext) string {
	sum := sha256.Sum256([]byte(string(c.GuildID) + "\x00" + string(c.IdempotencyKey) + "\x00" + string(c.ReconciliationKey)))
	return hex.EncodeToString(sum[:16])
}
func normalizeFilename(name string) string {
	name = strings.ReplaceAll(name, "\\", "_")
	name = strings.ReplaceAll(name, "/", "_")
	var b strings.Builder
	for _, r := range name {
		if unicode.IsControl(r) {
			b.WriteRune('_')
		} else {
			b.WriteRune(r)
		}
	}
	if b.Len() == 0 {
		return "attachment"
	}
	return b.String()
}
func snake(name string) string {
	var b strings.Builder
	for i, r := range name {
		if unicode.IsUpper(r) && i > 0 {
			b.WriteByte('_')
		}
		b.WriteRune(unicode.ToLower(r))
	}
	return b.String()
}

func decodeResult(command domain.Command, body []byte) (any, error) {
	if len(bytes.TrimSpace(body)) == 0 {
		return emptyResult(command), nil
	}
	decode := func(target any) error {
		decoder := json.NewDecoder(bytes.NewReader(body))
		if err := decoder.Decode(target); err != nil {
			return &domain.PayloadError{Kind: "json"}
		}
		var trailing any
		if err := decoder.Decode(&trailing); err != io.EOF {
			return &domain.PayloadError{Kind: "trailing_json"}
		}
		return nil
	}
	switch command.(type) {
	case domain.ListGuildChannels, domain.ReorderChannels:
		var v []domain.Channel
		return v, decode(&v)
	case domain.GetChannel, domain.CreateChannel, domain.ModifyChannel:
		var v domain.Channel
		return v, decode(&v)
	case domain.DeleteChannel:
		var v domain.Channel
		if err := decode(&v); err != nil {
			return nil, err
		}
		return domain.DeleteResult{ID: domain.Snowflake(v.ID)}, nil
	case domain.ListGuildRoles, domain.ReorderRoles:
		var v []domain.Role
		return v, decode(&v)
	case domain.CreateRole, domain.ModifyRole:
		var v domain.Role
		return v, decode(&v)
	case domain.ListMembers:
		var v []domain.Member
		return v, decode(&v)
	case domain.GetMember, domain.ModifyMember:
		var v domain.Member
		return v, decode(&v)
	case domain.ListMessages:
		var v []domain.Message
		return v, decode(&v)
	case domain.GetMessage, domain.CreateMessage, domain.ModifyMessage:
		var v domain.Message
		return v, decode(&v)
	case domain.ListGuildWebhooks, domain.ListChannelWebhooks:
		var v []domain.Webhook
		return v, decode(&v)
	case domain.CreateWebhook, domain.GetWebhook, domain.ModifyWebhook:
		var v domain.Webhook
		return v, decode(&v)
	case domain.ListGuildApplicationCommands, domain.BulkOverwriteGuildApplicationCommands:
		var v []domain.ApplicationCommand
		return v, decode(&v)
	case domain.GetGuildApplicationCommand, domain.CreateGuildApplicationCommand, domain.ModifyGuildApplicationCommand:
		var v domain.ApplicationCommand
		return v, decode(&v)
	case domain.GetGuildOnboarding, domain.ModifyGuildOnboarding:
		var v domain.Onboarding
		return v, decode(&v)
	case domain.ListGuildInvites, domain.ListChannelInvites:
		var v []domain.Invite
		return v, decode(&v)
	case domain.GetInvite, domain.CreateInvite, domain.DeleteInvite:
		var v domain.Invite
		return v, decode(&v)
	case domain.ListScheduledEvents:
		var v []domain.ScheduledEvent
		return v, decode(&v)
	case domain.GetScheduledEvent, domain.CreateScheduledEvent, domain.ModifyScheduledEvent:
		var v domain.ScheduledEvent
		return v, decode(&v)
	case domain.ListAutoModerationRules:
		var v []domain.AutoModerationRule
		return v, decode(&v)
	case domain.GetAutoModerationRule, domain.CreateAutoModerationRule, domain.ModifyAutoModerationRule:
		var v domain.AutoModerationRule
		return v, decode(&v)
	case domain.InspectGuild:
		var v domain.Guild
		return v, decode(&v)
	}
	return nil, &domain.InvariantError{Detail: "missing response codec"}
}
func emptyResult(command domain.Command) any {
	switch c := command.(type) {
	case domain.DeleteRole:
		return domain.DeleteResult{ID: domain.Snowflake(c.RoleID)}
	case domain.BulkDeleteMessages:
		return domain.BulkDeleteResult{IDs: c.MessageIDs}
	default:
		return domain.EmptyResult{}
	}
}
