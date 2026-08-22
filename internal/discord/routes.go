package discord

import (
	"fmt"
	"net/url"
	"strconv"

	"github.com/ken-kai/discord-control-experiments/internal/application"
	"github.com/ken-kai/discord-control-experiments/internal/domain"
)

type resolvedRoute struct {
	method, path, template, major string
	query                         url.Values
}

func resolveRoute(command domain.Command) (resolvedRoute, error) {
	spec, ok := application.LookupCommandSpec(command.CommandName())
	if !ok {
		return resolvedRoute{}, fmt.Errorf("unknown sealed Discord command")
	}
	r := resolvedRoute{method: spec.Method, template: spec.RouteTemplate, query: make(url.Values)}
	g := url.PathEscape(string(command.GuildScope()))
	r.major = g
	switch c := command.(type) {
	case domain.InspectGuild:
		r.path = "/guilds/" + g
	case domain.ListGuildChannels:
		r.path = "/guilds/" + g + "/channels"
	case domain.GetChannel:
		r.path = "/channels/" + id(c.ChannelID)
		r.major = id(c.ChannelID)
	case domain.CreateChannel:
		r.path = "/guilds/" + g + "/channels"
	case domain.ModifyChannel:
		r.path = "/channels/" + id(c.ChannelID)
		r.major = id(c.ChannelID)
	case domain.DeleteChannel:
		r.path = "/channels/" + id(c.ChannelID)
		r.major = id(c.ChannelID)
	case domain.ReorderChannels:
		r.path = "/guilds/" + g + "/channels"
	case domain.ListGuildRoles:
		r.path = "/guilds/" + g + "/roles"
	case domain.CreateRole:
		r.path = "/guilds/" + g + "/roles"
	case domain.ModifyRole:
		r.path = "/guilds/" + g + "/roles/" + id(c.RoleID)
	case domain.DeleteRole:
		r.path = "/guilds/" + g + "/roles/" + id(c.RoleID)
	case domain.ReorderRoles:
		r.path = "/guilds/" + g + "/roles"
	case domain.SetChannelPermission:
		r.path = "/channels/" + id(c.ChannelID) + "/permissions/" + id(c.Overwrite.ID)
		r.major = id(c.ChannelID)
	case domain.DeleteChannelPermission:
		r.path = "/channels/" + id(c.ChannelID) + "/permissions/" + id(c.OverwriteID)
		r.major = id(c.ChannelID)
	case domain.ListMembers:
		r.path = "/guilds/" + g + "/members"
		setInt(r.query, "limit", c.Limit)
		setID(r.query, "after", c.After)
	case domain.GetMember:
		r.path = "/guilds/" + g + "/members/" + id(c.UserID)
	case domain.ModifyMember:
		r.path = "/guilds/" + g + "/members/" + id(c.UserID)
	case domain.AddMemberRole:
		r.path = "/guilds/" + g + "/members/" + id(c.UserID) + "/roles/" + id(c.RoleID)
	case domain.RemoveMemberRole:
		r.path = "/guilds/" + g + "/members/" + id(c.UserID) + "/roles/" + id(c.RoleID)
	case domain.RemoveMember:
		r.path = "/guilds/" + g + "/members/" + id(c.UserID)
	case domain.CreateMemberBan:
		r.path = "/guilds/" + g + "/bans/" + id(c.UserID)
	case domain.DeleteMemberBan:
		r.path = "/guilds/" + g + "/bans/" + id(c.UserID)
	case domain.ListMessages:
		r.path = "/channels/" + id(c.ChannelID) + "/messages"
		r.major = id(c.ChannelID)
		setID(r.query, "around", c.Around)
		setID(r.query, "before", c.Before)
		setID(r.query, "after", c.After)
		setInt(r.query, "limit", c.Limit)
	case domain.GetMessage:
		r.path = "/channels/" + id(c.ChannelID) + "/messages/" + id(c.MessageID)
		r.major = id(c.ChannelID)
	case domain.CreateMessage:
		r.path = "/channels/" + id(c.ChannelID) + "/messages"
		r.major = id(c.ChannelID)
	case domain.ModifyMessage:
		r.path = "/channels/" + id(c.ChannelID) + "/messages/" + id(c.MessageID)
		r.major = id(c.ChannelID)
	case domain.DeleteMessage:
		r.path = "/channels/" + id(c.ChannelID) + "/messages/" + id(c.MessageID)
		r.major = id(c.ChannelID)
	case domain.BulkDeleteMessages:
		r.path = "/channels/" + id(c.ChannelID) + "/messages/bulk-delete"
		r.major = id(c.ChannelID)
	case domain.PinMessage:
		r.path = "/channels/" + id(c.ChannelID) + "/pins/" + id(c.MessageID)
		r.major = id(c.ChannelID)
	case domain.UnpinMessage:
		r.path = "/channels/" + id(c.ChannelID) + "/pins/" + id(c.MessageID)
		r.major = id(c.ChannelID)
	case domain.ListGuildWebhooks:
		r.path = "/guilds/" + g + "/webhooks"
	case domain.ListChannelWebhooks:
		r.path = "/channels/" + id(c.ChannelID) + "/webhooks"
		r.major = id(c.ChannelID)
	case domain.CreateWebhook:
		r.path = "/channels/" + id(c.ChannelID) + "/webhooks"
		r.major = id(c.ChannelID)
	case domain.GetWebhook:
		r.path = "/webhooks/" + id(c.WebhookID)
		r.major = id(c.WebhookID)
	case domain.ModifyWebhook:
		r.path = "/webhooks/" + id(c.WebhookID)
		r.major = id(c.WebhookID)
	case domain.DeleteWebhook:
		r.path = "/webhooks/" + id(c.WebhookID)
		r.major = id(c.WebhookID)
	case domain.ListGuildApplicationCommands:
		r.path = appPath(c.ApplicationID, g)
		setBool(r.query, "with_localizations", c.WithLocalizations)
	case domain.GetGuildApplicationCommand:
		r.path = appPath(c.ApplicationID, g) + "/" + id(c.ApplicationCommandID)
	case domain.CreateGuildApplicationCommand:
		r.path = appPath(c.ApplicationID, g)
	case domain.ModifyGuildApplicationCommand:
		r.path = appPath(c.ApplicationID, g) + "/" + id(c.ApplicationCommandID)
	case domain.DeleteGuildApplicationCommand:
		r.path = appPath(c.ApplicationID, g) + "/" + id(c.ApplicationCommandID)
	case domain.BulkOverwriteGuildApplicationCommands:
		r.path = appPath(c.ApplicationID, g)
	case domain.GetGuildOnboarding:
		r.path = "/guilds/" + g + "/onboarding"
	case domain.ModifyGuildOnboarding:
		r.path = "/guilds/" + g + "/onboarding"
	case domain.ListGuildInvites:
		r.path = "/guilds/" + g + "/invites"
	case domain.ListChannelInvites:
		r.path = "/channels/" + id(c.ChannelID) + "/invites"
		r.major = id(c.ChannelID)
	case domain.GetInvite:
		r.path = "/invites/" + url.PathEscape(string(c.Code))
		setBool(r.query, "with_counts", c.WithCounts)
		setBool(r.query, "with_expiration", c.WithExpiration)
		setID(r.query, "guild_scheduled_event_id", c.GuildScheduledEventID)
	case domain.CreateInvite:
		r.path = "/channels/" + id(c.ChannelID) + "/invites"
		r.major = id(c.ChannelID)
	case domain.DeleteInvite:
		r.path = "/invites/" + url.PathEscape(string(c.Code))
	case domain.ListScheduledEvents:
		r.path = eventPath(g)
		setBool(r.query, "with_user_count", c.WithUserCount)
	case domain.GetScheduledEvent:
		r.path = eventPath(g) + "/" + id(c.ScheduledEventID)
		setBool(r.query, "with_user_count", c.WithUserCount)
	case domain.CreateScheduledEvent:
		r.path = eventPath(g)
	case domain.ModifyScheduledEvent:
		r.path = eventPath(g) + "/" + id(c.ScheduledEventID)
	case domain.DeleteScheduledEvent:
		r.path = eventPath(g) + "/" + id(c.ScheduledEventID)
	case domain.ListAutoModerationRules, domain.CreateAutoModerationRule:
		r.path = autoModerationPath(g)
	case domain.GetAutoModerationRule:
		r.path = autoModerationPath(g) + "/" + id(c.RuleID)
	case domain.ModifyAutoModerationRule:
		r.path = autoModerationPath(g) + "/" + id(c.RuleID)
	case domain.DeleteAutoModerationRule:
		r.path = autoModerationPath(g) + "/" + id(c.RuleID)
	default:
		return resolvedRoute{}, fmt.Errorf("sealed command has no route")
	}
	return r, nil
}
func id[T ~string](v T) string { return url.PathEscape(string(v)) }
func setID[T ~string](q url.Values, k string, v *T) {
	if v != nil {
		q.Set(k, string(*v))
	}
}
func setInt(q url.Values, k string, v *int) {
	if v != nil {
		q.Set(k, strconv.Itoa(*v))
	}
}
func setBool(q url.Values, k string, v *bool) {
	if v != nil {
		q.Set(k, strconv.FormatBool(*v))
	}
}
func appPath(a domain.ApplicationID, g string) string {
	return "/applications/" + id(a) + "/guilds/" + g + "/commands"
}
func eventPath(g string) string          { return "/guilds/" + g + "/scheduled-events" }
func autoModerationPath(g string) string { return "/guilds/" + g + "/auto-moderation/rules" }
