package application

import (
	"reflect"
	"sort"
	"strconv"
	"strings"

	"github.com/ken-kai/discord-control-experiments/internal/domain"
)

type MutationClass string
type RetryClass string
type IdempotencyMode string
type OwnershipRequirement string

const (
	MutationRead            MutationClass        = "read"
	MutationWrite           MutationClass        = "write"
	MutationCreate          MutationClass        = "create"
	RetrySafe               RetryClass           = "safe"
	RetryIdempotent         RetryClass           = "idempotent"
	RetryUnsafe             RetryClass           = "unsafe"
	IdempotencyNone         IdempotencyMode      = "none"
	IdempotencyRequired     IdempotencyMode      = "required"
	OwnershipGuildQualified OwnershipRequirement = "guild_qualified"
	OwnershipChannel        OwnershipRequirement = "channel"
	OwnershipWebhook        OwnershipRequirement = "webhook"
	OwnershipInvite         OwnershipRequirement = "invite"
)

type CommandSpec struct {
	Name, Method, RouteTemplate string
	MajorResource               string
	Ownership                   OwnershipRequirement
	Mutation                    MutationClass
	Retry                       RetryClass
	Idempotency                 IdempotencyMode
	RequestCodec, ResponseCodec string
	Reconciliation              string
	Capability                  domain.Capability
	MutationExposed             bool
	DestructiveClass            domain.DestructiveClass
	Sensitive                   bool
	ResourceExtractor           func(domain.Command) []domain.ResourceRef
	IntentSanitizer             func(domain.Command) map[string]string
}

var commandRegistry = buildCommandRegistry()

func LookupCommandSpec(name string) (CommandSpec, bool) {
	spec, ok := commandRegistry[name]
	return spec, ok
}
func CommandCatalog() []CommandSpec {
	result := make([]CommandSpec, 0, len(commandRegistry))
	for _, spec := range commandRegistry {
		result = append(result, spec)
	}
	sort.Slice(result, func(i, j int) bool { return result[i].Name < result[j].Name })
	return result
}

func buildCommandRegistry() map[string]CommandSpec {
	m := make(map[string]CommandSpec)
	add := func(name, method, route, major string, ownership OwnershipRequirement, mutation MutationClass, retry RetryClass, idem IdempotencyMode, reconciliation string) {
		m[name] = CommandSpec{Name: name, Method: method, RouteTemplate: route, MajorResource: major, Ownership: ownership, Mutation: mutation, Retry: retry, Idempotency: idem, RequestCodec: "json", ResponseCodec: "json", Reconciliation: reconciliation, Capability: capabilityFor(name, mutation), MutationExposed: mutation != MutationRead, DestructiveClass: destructiveForName(name), Sensitive: true, ResourceExtractor: extractCommandResources, IntentSanitizer: sanitizeCommandIntent}
	}
	g := OwnershipGuildQualified
	add("get_current_user", "GET", "/users/@me", "guild.id", g, MutationRead, RetrySafe, IdempotencyNone, "none")
	add("inspect_guild", "GET", "/guilds/{guild.id}", "guild.id", g, MutationRead, RetrySafe, IdempotencyNone, "none")
	add("list_guild_channels", "GET", "/guilds/{guild.id}/channels", "guild.id", g, MutationRead, RetrySafe, IdempotencyNone, "none")
	add("get_channel", "GET", "/channels/{channel.id}", "channel.id", OwnershipChannel, MutationRead, RetrySafe, IdempotencyNone, "none")
	add("create_channel", "POST", "/guilds/{guild.id}/channels", "guild.id", g, MutationCreate, RetryUnsafe, IdempotencyRequired, "channel_fields")
	add("modify_channel", "PATCH", "/channels/{channel.id}", "channel.id", OwnershipChannel, MutationWrite, RetryIdempotent, IdempotencyNone, "none")
	add("delete_channel", "DELETE", "/channels/{channel.id}", "channel.id", OwnershipChannel, MutationWrite, RetryIdempotent, IdempotencyNone, "none")
	add("reorder_channels", "PATCH", "/guilds/{guild.id}/channels", "guild.id", g, MutationWrite, RetryIdempotent, IdempotencyNone, "none")
	add("list_guild_roles", "GET", "/guilds/{guild.id}/roles", "guild.id", g, MutationRead, RetrySafe, IdempotencyNone, "none")
	add("create_role", "POST", "/guilds/{guild.id}/roles", "guild.id", g, MutationCreate, RetryUnsafe, IdempotencyRequired, "role_fields")
	add("modify_role", "PATCH", "/guilds/{guild.id}/roles/{role.id}", "guild.id", g, MutationWrite, RetryIdempotent, IdempotencyNone, "none")
	add("delete_role", "DELETE", "/guilds/{guild.id}/roles/{role.id}", "guild.id", g, MutationWrite, RetryIdempotent, IdempotencyNone, "none")
	add("reorder_roles", "PATCH", "/guilds/{guild.id}/roles", "guild.id", g, MutationWrite, RetryIdempotent, IdempotencyNone, "none")
	add("set_channel_permission", "PUT", "/channels/{channel.id}/permissions/{overwrite.id}", "channel.id", OwnershipChannel, MutationWrite, RetryIdempotent, IdempotencyNone, "none")
	add("delete_channel_permission", "DELETE", "/channels/{channel.id}/permissions/{overwrite.id}", "channel.id", OwnershipChannel, MutationWrite, RetryIdempotent, IdempotencyNone, "none")
	member := []struct{ name, method, route string }{{"list_members", "GET", "/guilds/{guild.id}/members"}, {"get_member", "GET", "/guilds/{guild.id}/members/{user.id}"}, {"modify_member", "PATCH", "/guilds/{guild.id}/members/{user.id}"}, {"add_member_role", "PUT", "/guilds/{guild.id}/members/{user.id}/roles/{role.id}"}, {"remove_member_role", "DELETE", "/guilds/{guild.id}/members/{user.id}/roles/{role.id}"}, {"remove_member", "DELETE", "/guilds/{guild.id}/members/{user.id}"}, {"create_member_ban", "PUT", "/guilds/{guild.id}/bans/{user.id}"}, {"delete_member_ban", "DELETE", "/guilds/{guild.id}/bans/{user.id}"}}
	for _, x := range member {
		mut, retry := MutationWrite, RetryIdempotent
		if x.method == "GET" {
			mut, retry = MutationRead, RetrySafe
		}
		add(x.name, x.method, x.route, "guild.id", g, mut, retry, IdempotencyNone, "none")
	}
	messages := []struct {
		name, method, route string
		mut                 MutationClass
		retry               RetryClass
		idem                IdempotencyMode
		rec                 string
	}{{"list_messages", "GET", "/channels/{channel.id}/messages", MutationRead, RetrySafe, IdempotencyNone, "none"}, {"get_message", "GET", "/channels/{channel.id}/messages/{message.id}", MutationRead, RetrySafe, IdempotencyNone, "none"}, {"create_message", "POST", "/channels/{channel.id}/messages", MutationCreate, RetryUnsafe, IdempotencyRequired, "message_nonce"}, {"modify_message", "PATCH", "/channels/{channel.id}/messages/{message.id}", MutationWrite, RetryIdempotent, IdempotencyNone, "none"}, {"delete_message", "DELETE", "/channels/{channel.id}/messages/{message.id}", MutationWrite, RetryIdempotent, IdempotencyNone, "none"}, {"bulk_delete_messages", "POST", "/channels/{channel.id}/messages/bulk-delete", MutationWrite, RetryIdempotent, IdempotencyNone, "none"}, {"pin_message", "PUT", "/channels/{channel.id}/pins/{message.id}", MutationWrite, RetryIdempotent, IdempotencyNone, "none"}, {"unpin_message", "DELETE", "/channels/{channel.id}/pins/{message.id}", MutationWrite, RetryIdempotent, IdempotencyNone, "none"}}
	for _, x := range messages {
		add(x.name, x.method, x.route, "channel.id", OwnershipChannel, x.mut, x.retry, x.idem, x.rec)
	}
	webhooks := []struct {
		name, method, route string
		own                 OwnershipRequirement
		mut                 MutationClass
		retry               RetryClass
		idem                IdempotencyMode
	}{{"list_guild_webhooks", "GET", "/guilds/{guild.id}/webhooks", g, MutationRead, RetrySafe, IdempotencyNone}, {"list_channel_webhooks", "GET", "/channels/{channel.id}/webhooks", OwnershipChannel, MutationRead, RetrySafe, IdempotencyNone}, {"create_webhook", "POST", "/channels/{channel.id}/webhooks", OwnershipChannel, MutationCreate, RetryUnsafe, IdempotencyRequired}, {"get_webhook", "GET", "/webhooks/{webhook.id}", OwnershipWebhook, MutationRead, RetrySafe, IdempotencyNone}, {"modify_webhook", "PATCH", "/webhooks/{webhook.id}", OwnershipWebhook, MutationWrite, RetryIdempotent, IdempotencyNone}, {"delete_webhook", "DELETE", "/webhooks/{webhook.id}", OwnershipWebhook, MutationWrite, RetryIdempotent, IdempotencyNone}}
	for _, x := range webhooks {
		add(x.name, x.method, x.route, map[bool]string{true: "guild.id", false: "channel.id"}[x.name == "list_guild_webhooks"], x.own, x.mut, x.retry, x.idem, map[bool]string{true: "webhook_fields", false: "none"}[x.mut == MutationCreate])
	}
	apps := []struct {
		name, method, tail string
		mut                MutationClass
		retry              RetryClass
		idem               IdempotencyMode
	}{{"list_guild_application_commands", "GET", "", MutationRead, RetrySafe, IdempotencyNone}, {"get_guild_application_command", "GET", "/{command.id}", MutationRead, RetrySafe, IdempotencyNone}, {"create_guild_application_command", "POST", "", MutationCreate, RetryUnsafe, IdempotencyRequired}, {"modify_guild_application_command", "PATCH", "/{command.id}", MutationWrite, RetryIdempotent, IdempotencyNone}, {"delete_guild_application_command", "DELETE", "/{command.id}", MutationWrite, RetryIdempotent, IdempotencyNone}, {"bulk_overwrite_guild_application_commands", "PUT", "", MutationWrite, RetryIdempotent, IdempotencyNone}}
	for _, x := range apps {
		add(x.name, x.method, "/applications/{application.id}/guilds/{guild.id}/commands"+x.tail, "guild.id", g, x.mut, x.retry, x.idem, map[bool]string{true: "application_command_fields", false: "none"}[x.mut == MutationCreate])
	}
	add("get_guild_onboarding", "GET", "/guilds/{guild.id}/onboarding", "guild.id", g, MutationRead, RetrySafe, IdempotencyNone, "none")
	add("modify_guild_onboarding", "PUT", "/guilds/{guild.id}/onboarding", "guild.id", g, MutationWrite, RetryIdempotent, IdempotencyNone, "none")
	invites := []struct {
		name, method, route string
		own                 OwnershipRequirement
		mut                 MutationClass
		retry               RetryClass
		idem                IdempotencyMode
	}{{"list_guild_invites", "GET", "/guilds/{guild.id}/invites", g, MutationRead, RetrySafe, IdempotencyNone}, {"list_channel_invites", "GET", "/channels/{channel.id}/invites", OwnershipChannel, MutationRead, RetrySafe, IdempotencyNone}, {"get_invite", "GET", "/invites/{invite.code}", OwnershipInvite, MutationRead, RetrySafe, IdempotencyNone}, {"create_invite", "POST", "/channels/{channel.id}/invites", OwnershipChannel, MutationCreate, RetryUnsafe, IdempotencyRequired}, {"delete_invite", "DELETE", "/invites/{invite.code}", OwnershipInvite, MutationWrite, RetryIdempotent, IdempotencyNone}}
	for _, x := range invites {
		add(x.name, x.method, x.route, "guild.id", x.own, x.mut, x.retry, x.idem, map[bool]string{true: "invite_fields", false: "none"}[x.mut == MutationCreate])
	}
	events := []struct {
		name, method, tail string
		mut                MutationClass
		retry              RetryClass
		idem               IdempotencyMode
	}{{"list_scheduled_events", "GET", "", MutationRead, RetrySafe, IdempotencyNone}, {"get_scheduled_event", "GET", "/{event.id}", MutationRead, RetrySafe, IdempotencyNone}, {"create_scheduled_event", "POST", "", MutationCreate, RetryUnsafe, IdempotencyRequired}, {"modify_scheduled_event", "PATCH", "/{event.id}", MutationWrite, RetryIdempotent, IdempotencyNone}, {"delete_scheduled_event", "DELETE", "/{event.id}", MutationWrite, RetryIdempotent, IdempotencyNone}}
	for _, x := range events {
		add(x.name, x.method, "/guilds/{guild.id}/scheduled-events"+x.tail, "guild.id", g, x.mut, x.retry, x.idem, map[bool]string{true: "scheduled_event_fields", false: "none"}[x.mut == MutationCreate])
	}
	automod := []struct {
		name, method, tail string
		mut                MutationClass
		retry              RetryClass
		idem               IdempotencyMode
	}{
		{"list_auto_moderation_rules", "GET", "", MutationRead, RetrySafe, IdempotencyNone},
		{"get_auto_moderation_rule", "GET", "/{rule.id}", MutationRead, RetrySafe, IdempotencyNone},
		{"create_auto_moderation_rule", "POST", "", MutationCreate, RetryUnsafe, IdempotencyRequired},
		{"modify_auto_moderation_rule", "PATCH", "/{rule.id}", MutationWrite, RetryIdempotent, IdempotencyNone},
		{"delete_auto_moderation_rule", "DELETE", "/{rule.id}", MutationWrite, RetryIdempotent, IdempotencyNone},
	}
	for _, x := range automod {
		add(x.name, x.method, "/guilds/{guild.id}/auto-moderation/rules"+x.tail, "guild.id", g, x.mut, x.retry, x.idem, map[bool]string{true: "auto_moderation_fields", false: "none"}[x.mut == MutationCreate])
	}
	return m
}

func capabilityFor(name string, mutation MutationClass) domain.Capability {
	manage := mutation != MutationRead
	switch {
	case strings.Contains(name, "auto_moderation"):
		if manage {
			return domain.CapabilityAutoModerationManage
		}
		return domain.CapabilityAutoModerationRead
	case strings.Contains(name, "onboarding"):
		if manage {
			return domain.CapabilityOnboardingManage
		}
		return domain.CapabilityOnboardingRead
	case strings.Contains(name, "application_command"):
		if manage {
			return domain.CapabilityApplicationCommandManage
		}
		return domain.CapabilityApplicationCommandRead
	case strings.Contains(name, "scheduled_event"):
		if manage {
			return domain.CapabilityScheduledEventManage
		}
		return domain.CapabilityScheduledEventRead
	case strings.Contains(name, "webhook"):
		if manage {
			return domain.CapabilityWebhookManage
		}
		return domain.CapabilityWebhookRead
	case strings.Contains(name, "invite"):
		if manage {
			return domain.CapabilityInviteManage
		}
		return domain.CapabilityInviteRead
	case strings.Contains(name, "message") || strings.Contains(name, "pin_"):
		if manage {
			return domain.CapabilityMessageManage
		}
		return domain.CapabilityMessageRead
	case strings.Contains(name, "member") || strings.Contains(name, "ban"):
		if manage {
			return domain.CapabilityMemberModerate
		}
		return domain.CapabilityMemberRead
	case strings.Contains(name, "role") || strings.Contains(name, "permission"):
		if manage {
			return domain.CapabilityRoleManage
		}
		return domain.CapabilityRoleRead
	case strings.Contains(name, "channel"):
		if manage {
			return domain.CapabilityChannelManage
		}
		return domain.CapabilityChannelRead
	default:
		return domain.CapabilityGuildInspect
	}
}

func destructiveForName(name string) domain.DestructiveClass {
	switch name {
	case "bulk_delete_messages":
		return domain.DestructiveBulkDelete
	case "create_member_ban":
		return domain.DestructiveBan
	case "remove_member":
		return domain.DestructiveKick
	case "create_webhook":
		return domain.DestructiveWebhookCreate
	case "create_invite":
		return domain.DestructiveInviteCreate
	case "bulk_overwrite_guild_application_commands":
		return domain.DestructiveBulkOverwrite
	case "set_channel_permission", "delete_channel_permission", "reorder_channels", "modify_guild_onboarding":
		return domain.DestructivePermissionChange
	case "add_member_role":
		return domain.DestructiveRoleEscalation
	}
	if strings.HasPrefix(name, "delete_") {
		return domain.DestructiveDelete
	}
	return domain.DestructiveNone
}

func DestructiveClassForCommand(command domain.Command, spec CommandSpec) domain.DestructiveClass {
	switch typed := command.(type) {
	case domain.CreateRole:
		if typed.Permissions != nil {
			return domain.DestructiveRoleEscalation
		}
	case *domain.CreateRole:
		if typed != nil && typed.Permissions != nil {
			return domain.DestructiveRoleEscalation
		}
	case domain.ModifyRole:
		if typed.Permissions != nil {
			return domain.DestructiveRoleEscalation
		}
	case *domain.ModifyRole:
		if typed != nil && typed.Permissions != nil {
			return domain.DestructiveRoleEscalation
		}
	case domain.ModifyMember:
		if typed.Roles != nil {
			return domain.DestructiveRoleEscalation
		}
	case *domain.ModifyMember:
		if typed != nil && typed.Roles != nil {
			return domain.DestructiveRoleEscalation
		}
	}
	return spec.DestructiveClass
}

func extractCommandResources(command domain.Command) []domain.ResourceRef {
	refs := []domain.ResourceRef{{Kind: domain.ResourceGuild, ID: string(command.GuildScope()), GuildID: command.GuildScope(), Owned: true}}
	seen := map[string]bool{"guild:" + string(command.GuildScope()): true}
	extractResourceValues(reflect.ValueOf(command), command.GuildScope(), &refs, seen)
	sort.Slice(refs, func(i, j int) bool {
		if refs[i].Kind == refs[j].Kind {
			return refs[i].ID < refs[j].ID
		}
		return refs[i].Kind < refs[j].Kind
	})
	return refs
}

func extractResourceValues(v reflect.Value, guild domain.GuildID, refs *[]domain.ResourceRef, seen map[string]bool) {
	if !v.IsValid() {
		return
	}
	if v.Kind() == reflect.Pointer {
		if v.IsNil() {
			return
		}
		extractResourceValues(v.Elem(), guild, refs, seen)
		return
	}
	if v.Kind() == reflect.Slice || v.Kind() == reflect.Array {
		for i := 0; i < v.Len(); i++ {
			extractResourceValues(v.Index(i), guild, refs, seen)
		}
		return
	}
	if v.Kind() != reflect.Struct {
		return
	}
	for i := 0; i < v.NumField(); i++ {
		field, value := v.Type().Field(i), v.Field(i)
		if field.Name == "CommandContext" || field.Name == "AuditReason" || field.Name == "IdempotencyKey" || field.Name == "ReconciliationKey" {
			continue
		}
		kind, ok := resourceKindForType(value)
		if ok {
			for _, id := range stringValues(value) {
				key := string(kind) + ":" + id
				if id != "" && !seen[key] {
					seen[key] = true
					*refs = append(*refs, domain.ResourceRef{Kind: kind, ID: id, GuildID: guild})
				}
			}
			continue
		}
		extractResourceValues(value, guild, refs, seen)
	}
}

func resourceKindForType(v reflect.Value) (domain.ResourceKind, bool) {
	for v.IsValid() && (v.Kind() == reflect.Pointer || v.Kind() == reflect.Slice || v.Kind() == reflect.Array) {
		if v.Kind() == reflect.Pointer {
			if v.IsNil() {
				return "", false
			}
			v = v.Elem()
		} else {
			if v.Len() == 0 {
				return kindForTypeName(v.Type().Elem().Name())
			}
			v = v.Index(0)
		}
	}
	if !v.IsValid() {
		return "", false
	}
	return kindForTypeName(v.Type().Name())
}

func kindForTypeName(name string) (domain.ResourceKind, bool) {
	switch name {
	case "ChannelID":
		return domain.ResourceChannel, true
	case "RoleID":
		return domain.ResourceRole, true
	case "UserID":
		return domain.ResourceMember, true
	case "MessageID":
		return domain.ResourceMessage, true
	case "WebhookID":
		return domain.ResourceWebhook, true
	case "InviteCode":
		return domain.ResourceInvite, true
	case "ApplicationID":
		return domain.ResourceApplication, true
	case "ApplicationCommandID":
		return domain.ResourceApplicationCommand, true
	case "ScheduledEventID":
		return domain.ResourceScheduledEvent, true
	case "AutoModerationRuleID":
		return domain.ResourceAutoModerationRule, true
	default:
		return "", false
	}
}

func stringValues(v reflect.Value) []string {
	if v.Kind() == reflect.Pointer {
		if v.IsNil() {
			return nil
		}
		return stringValues(v.Elem())
	}
	if v.Kind() == reflect.Slice || v.Kind() == reflect.Array {
		out := make([]string, 0, v.Len())
		for i := 0; i < v.Len(); i++ {
			out = append(out, stringValues(v.Index(i))...)
		}
		return out
	}
	if v.Kind() == reflect.String {
		return []string{v.String()}
	}
	return nil
}

func sanitizeCommandIntent(command domain.Command) map[string]string {
	intent := map[string]string{}
	sanitizeIntentValue(reflect.ValueOf(command), "", intent)
	return intent
}

func sanitizeIntentValue(v reflect.Value, prefix string, out map[string]string) {
	if !v.IsValid() {
		return
	}
	if v.Kind() == reflect.Pointer {
		if v.IsNil() {
			return
		}
		sanitizeIntentValue(v.Elem(), prefix, out)
		return
	}
	if v.Kind() != reflect.Struct {
		return
	}
	for i := 0; i < v.NumField(); i++ {
		field, value := v.Type().Field(i), v.Field(i)
		name := strings.ToLower(field.Name)
		if prefix != "" {
			name = prefix + "." + name
		}
		if field.Name == "CommandContext" || strings.Contains(name, "content") || strings.Contains(name, "name") || strings.Contains(name, "topic") || strings.Contains(name, "description") || strings.Contains(name, "reason") || strings.Contains(name, "reader") || strings.Contains(name, "url") || strings.Contains(name, "code") || strings.HasSuffix(field.Name, "ID") {
			continue
		}
		switch value.Kind() {
		case reflect.Bool:
			out[name] = strconv.FormatBool(value.Bool())
		case reflect.Slice, reflect.Array:
			out[name+"_count"] = strconv.Itoa(value.Len())
		case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
			out[name] = strconv.FormatInt(value.Int(), 10)
		case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
			out[name] = strconv.FormatUint(value.Uint(), 10)
		case reflect.Struct:
			sanitizeIntentValue(value, name, out)
		}
	}
}
