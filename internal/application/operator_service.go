package application

import (
	"context"
	"errors"
	"reflect"
	"sort"

	"github.com/ken-kai/discord-control-experiments/internal/domain"
)

type OperatorRequest struct {
	Handler    string
	Invocation Invocation
	Command    domain.Command
	Values     map[string]string
}

type OperatorResult struct {
	Kind     string
	Value    any
	Plan     *domain.OperationPlan
	Partial  *domain.PartialResult
	Attempts int
	Replay   domain.ReplayState
}

type OperatorHandler func(context.Context, OperatorRequest) (OperatorResult, error)

type OperatorService struct {
	discord  *DiscordService
	handlers map[string]OperatorHandler
}

func NewOperatorService(discord *DiscordService, handlers map[string]OperatorHandler) (*OperatorService, error) {
	if discord == nil {
		return nil, errors.New("operator service requires secured Discord service")
	}
	copyHandlers := make(map[string]OperatorHandler, len(handlers))
	for name, handler := range handlers {
		if name == "" || handler == nil {
			return nil, errors.New("operator handler is invalid")
		}
		if _, discordCommand := LookupCommandSpec(name); discordCommand {
			return nil, errors.New("operator handler cannot replace Discord command")
		}
		copyHandlers[name] = handler
	}
	return &OperatorService{discord: discord, handlers: copyHandlers}, nil
}

func (s *OperatorService) Execute(ctx context.Context, request OperatorRequest) (OperatorResult, error) {
	if s == nil || ctx == nil || request.Handler == "" {
		return OperatorResult{}, errors.New("invalid operator request")
	}
	if spec, ok := LookupCommandSpec(request.Handler); ok {
		if request.Command == nil || request.Command.CommandName() != spec.Name {
			return OperatorResult{}, &domain.InvariantError{Detail: "operator command does not match sealed handler"}
		}
		execution, err := s.discord.dispatchDiscord(ctx, request.Invocation, request.Command)
		return OperatorResult{Kind: "DiscordResult", Value: execution.Result, Plan: execution.Plan, Partial: execution.Partial, Attempts: execution.Attempts, Replay: execution.Replay}, err
	}
	handler, ok := s.handlers[request.Handler]
	if !ok {
		return OperatorResult{}, &domain.AuthorizationError{Reason: domain.PolicyUnknownCommand}
	}
	return handler(ctx, request)
}

func (s *OperatorService) HandlerCatalog() []string {
	result := make([]string, 0, len(s.handlers)+len(CommandCatalog()))
	for name := range s.handlers {
		result = append(result, name)
	}
	for _, spec := range CommandCatalog() {
		result = append(result, spec.Name)
	}
	sort.Strings(result)
	return result
}

// NewOperatorDiscordCommand allocates only a sealed domain command and binds its
// guild scope before strict CLI JSON decoding. No caller-controlled route/method
// reaches the transport.
func NewOperatorDiscordCommand(name string, guildID domain.GuildID) (domain.Command, error) {
	return NewContractCommand(name, guildID)
}

func BindOperatorCommand(command domain.Command, guildID domain.GuildID, values map[string]string) error {
	if command == nil || guildID == "" {
		return &domain.InvariantError{Detail: "operator command scope is invalid"}
	}
	value := reflect.ValueOf(command)
	if value.Kind() != reflect.Pointer || value.IsNil() {
		return &domain.InvariantError{Detail: "operator command must be mutable"}
	}
	value = value.Elem()
	contextField := value.FieldByName("CommandContext")
	if !contextField.IsValid() || !contextField.CanSet() {
		return &domain.InvariantError{Detail: "command context unavailable"}
	}
	current, _ := contextField.Interface().(domain.CommandContext)
	current.GuildID = guildID // Rebind after JSON decoding; payloads cannot switch guild ownership.
	contextField.Set(reflect.ValueOf(current))
	fieldByFlag := map[string]string{"--application": "ApplicationID", "--channel": "ChannelID", "--role": "RoleID", "--user": "UserID", "--message": "MessageID", "--webhook": "WebhookID", "--command": "ApplicationCommandID", "--rule": "RuleID", "--overwrite": "OverwriteID"}
	for flag, fieldName := range fieldByFlag {
		raw := values[flag]
		if raw == "" {
			continue
		}
		field := value.FieldByName(fieldName)
		if !field.IsValid() || !field.CanSet() {
			continue
		}
		replacement := reflect.ValueOf(raw)
		if replacement.Type().ConvertibleTo(field.Type()) {
			field.Set(replacement.Convert(field.Type()))
		}
	}
	return nil
}

func BindOperatorInvocation(command domain.Command, correlationID domain.CorrelationID) error {
	return BindOperatorInvocationFor(command, correlationID, "cli")
}

func BindOperatorInvocationFor(command domain.Command, correlationID domain.CorrelationID, adapter string) error {
	if command == nil || correlationID == "" || adapter == "" {
		return &domain.InvariantError{Detail: "operator correlation is invalid"}
	}
	value := reflect.ValueOf(command)
	if value.Kind() != reflect.Pointer || value.IsNil() {
		return &domain.InvariantError{Detail: "operator command must be mutable"}
	}
	field := value.Elem().FieldByName("CommandContext")
	if !field.IsValid() || !field.CanSet() {
		return &domain.InvariantError{Detail: "command context unavailable"}
	}
	current := field.Interface().(domain.CommandContext)
	if current.IdempotencyKey == "" {
		current.IdempotencyKey = domain.IdempotencyKey(adapter + "-" + string(correlationID))
	}
	if current.ReconciliationKey == "" {
		current.ReconciliationKey = domain.ReconciliationKey(adapter + "-" + string(correlationID))
	}
	field.Set(reflect.ValueOf(current))
	return nil
}

var operatorCommandTypes = map[string]reflect.Type{
	"inspect_guild":       reflect.TypeOf(domain.InspectGuild{}),
	"list_guild_channels": reflect.TypeOf(domain.ListGuildChannels{}), "get_channel": reflect.TypeOf(domain.GetChannel{}), "create_channel": reflect.TypeOf(domain.CreateChannel{}), "modify_channel": reflect.TypeOf(domain.ModifyChannel{}), "delete_channel": reflect.TypeOf(domain.DeleteChannel{}), "reorder_channels": reflect.TypeOf(domain.ReorderChannels{}),
	"list_guild_roles": reflect.TypeOf(domain.ListGuildRoles{}), "create_role": reflect.TypeOf(domain.CreateRole{}), "modify_role": reflect.TypeOf(domain.ModifyRole{}), "delete_role": reflect.TypeOf(domain.DeleteRole{}), "reorder_roles": reflect.TypeOf(domain.ReorderRoles{}),
	"set_channel_permission": reflect.TypeOf(domain.SetChannelPermission{}), "delete_channel_permission": reflect.TypeOf(domain.DeleteChannelPermission{}),
	"list_members": reflect.TypeOf(domain.ListMembers{}), "get_member": reflect.TypeOf(domain.GetMember{}), "modify_member": reflect.TypeOf(domain.ModifyMember{}), "add_member_role": reflect.TypeOf(domain.AddMemberRole{}), "remove_member_role": reflect.TypeOf(domain.RemoveMemberRole{}), "remove_member": reflect.TypeOf(domain.RemoveMember{}), "create_member_ban": reflect.TypeOf(domain.CreateMemberBan{}), "delete_member_ban": reflect.TypeOf(domain.DeleteMemberBan{}),
	"list_messages": reflect.TypeOf(domain.ListMessages{}), "get_message": reflect.TypeOf(domain.GetMessage{}), "create_message": reflect.TypeOf(domain.CreateMessage{}), "modify_message": reflect.TypeOf(domain.ModifyMessage{}), "delete_message": reflect.TypeOf(domain.DeleteMessage{}), "bulk_delete_messages": reflect.TypeOf(domain.BulkDeleteMessages{}), "pin_message": reflect.TypeOf(domain.PinMessage{}), "unpin_message": reflect.TypeOf(domain.UnpinMessage{}),
	"list_guild_webhooks": reflect.TypeOf(domain.ListGuildWebhooks{}), "list_channel_webhooks": reflect.TypeOf(domain.ListChannelWebhooks{}), "create_webhook": reflect.TypeOf(domain.CreateWebhook{}), "get_webhook": reflect.TypeOf(domain.GetWebhook{}), "modify_webhook": reflect.TypeOf(domain.ModifyWebhook{}), "delete_webhook": reflect.TypeOf(domain.DeleteWebhook{}),
	"list_guild_application_commands": reflect.TypeOf(domain.ListGuildApplicationCommands{}), "get_guild_application_command": reflect.TypeOf(domain.GetGuildApplicationCommand{}), "create_guild_application_command": reflect.TypeOf(domain.CreateGuildApplicationCommand{}), "modify_guild_application_command": reflect.TypeOf(domain.ModifyGuildApplicationCommand{}), "delete_guild_application_command": reflect.TypeOf(domain.DeleteGuildApplicationCommand{}), "bulk_overwrite_guild_application_commands": reflect.TypeOf(domain.BulkOverwriteGuildApplicationCommands{}),
	"get_guild_onboarding": reflect.TypeOf(domain.GetGuildOnboarding{}), "modify_guild_onboarding": reflect.TypeOf(domain.ModifyGuildOnboarding{}),
	"list_guild_invites": reflect.TypeOf(domain.ListGuildInvites{}), "list_channel_invites": reflect.TypeOf(domain.ListChannelInvites{}), "get_invite": reflect.TypeOf(domain.GetInvite{}), "create_invite": reflect.TypeOf(domain.CreateInvite{}), "delete_invite": reflect.TypeOf(domain.DeleteInvite{}),
	"list_scheduled_events": reflect.TypeOf(domain.ListScheduledEvents{}), "get_scheduled_event": reflect.TypeOf(domain.GetScheduledEvent{}), "create_scheduled_event": reflect.TypeOf(domain.CreateScheduledEvent{}), "modify_scheduled_event": reflect.TypeOf(domain.ModifyScheduledEvent{}), "delete_scheduled_event": reflect.TypeOf(domain.DeleteScheduledEvent{}),
	"list_auto_moderation_rules": reflect.TypeOf(domain.ListAutoModerationRules{}), "get_auto_moderation_rule": reflect.TypeOf(domain.GetAutoModerationRule{}), "create_auto_moderation_rule": reflect.TypeOf(domain.CreateAutoModerationRule{}), "modify_auto_moderation_rule": reflect.TypeOf(domain.ModifyAutoModerationRule{}), "delete_auto_moderation_rule": reflect.TypeOf(domain.DeleteAutoModerationRule{}),
}
