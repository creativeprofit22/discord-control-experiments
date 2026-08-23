package application

import (
	"errors"
	"reflect"
	"sort"
	"strings"

	"github.com/ken-kai/discord-control-experiments/internal/domain"
)

type Toolset string

const (
	ToolsetBuild      Toolset = "build"
	ToolsetModeration Toolset = "moderation"
	ToolsetNews       Toolset = "news"
	ToolsetDiagnostic Toolset = "diagnostic"
)

type CommandContract struct {
	Name        string
	Toolsets    []Toolset
	Description string
	InputType   reflect.Type
	ResultType  reflect.Type
	Spec        CommandSpec
}

var commandContracts = buildCommandContracts()

func CommandContracts() []CommandContract {
	result := make([]CommandContract, 0, len(commandContracts))
	for _, contract := range commandContracts {
		copyContract := contract
		copyContract.Toolsets = append([]Toolset(nil), contract.Toolsets...)
		result = append(result, copyContract)
	}
	sort.Slice(result, func(i, j int) bool { return result[i].Name < result[j].Name })
	return result
}

func LookupCommandContract(name string) (CommandContract, bool) {
	contract, ok := commandContracts[name]
	if ok {
		contract.Toolsets = append([]Toolset(nil), contract.Toolsets...)
	}
	return contract, ok
}

func NewContractCommand(name string, guildID domain.GuildID) (domain.Command, error) {
	contract, ok := LookupCommandContract(name)
	if !ok || contract.InputType.Kind() != reflect.Struct {
		return nil, &domain.AuthorizationError{Reason: domain.PolicyUnknownCommand}
	}
	value := reflect.New(contract.InputType)
	contextField := value.Elem().FieldByName("CommandContext")
	if !contextField.IsValid() || !contextField.CanSet() {
		return nil, &domain.InvariantError{Detail: "command context unavailable"}
	}
	contextField.Set(reflect.ValueOf(domain.CommandContext{GuildID: guildID}))
	command, ok := value.Interface().(domain.Command)
	if !ok {
		return nil, &domain.InvariantError{Detail: "sealed command type invalid"}
	}
	return command, nil
}

func buildCommandContracts() map[string]CommandContract {
	contracts := make(map[string]CommandContract, len(operatorCommandTypes))
	for name, input := range operatorCommandTypes {
		spec, ok := LookupCommandSpec(name)
		if !ok {
			panic("operator command has no command spec: " + name)
		}
		result, err := commandResultType(name)
		if err != nil {
			panic(err)
		}
		sets := toolsetsForCommand(spec)
		contracts[name] = CommandContract{
			Name: name, Toolsets: sets, InputType: input, ResultType: result, Spec: spec,
			Description: describeContract(spec),
		}
	}
	if len(contracts) != len(commandRegistry) {
		panic("Discord command contract registry is incomplete")
	}
	return contracts
}

func describeContract(spec CommandSpec) string {
	class := "read-only"
	if spec.MutationExposed {
		class = "mutation"
	}
	approval := "no destructive approval"
	if spec.DestructiveClass != domain.DestructiveNone {
		approval = "destructive approval required for apply"
	} else if field := conditionalDestructiveField(spec.Name); field != "" {
		approval = "destructive approval required for apply when " + field + " is provided"
	}
	return string(spec.Capability) + "; " + class + "; " + approval
}

func conditionalDestructiveField(name string) string {
	switch name {
	case "create_role", "modify_role":
		return "permissions"
	case "modify_member":
		return "roles"
	default:
		return ""
	}
}

func MayRequireDestructiveApproval(spec CommandSpec) bool {
	return spec.DestructiveClass != domain.DestructiveNone || conditionalDestructiveField(spec.Name) != ""
}

func toolsetsForCommand(spec CommandSpec) []Toolset {
	sets := []Toolset{}
	if spec.Mutation == MutationRead {
		sets = append(sets, ToolsetDiagnostic)
	}
	name := spec.Name
	switch {
	case strings.Contains(name, "member"), strings.Contains(name, "ban"), strings.Contains(name, "auto_moderation"), strings.Contains(name, "delete_message"), strings.Contains(name, "pin_message"), strings.Contains(name, "unpin_message"):
		sets = append(sets, ToolsetModeration)
	case strings.Contains(name, "invite"), strings.Contains(name, "scheduled_event"), strings.Contains(name, "message"):
		sets = append(sets, ToolsetNews)
	default:
		sets = append(sets, ToolsetBuild)
	}
	sort.Slice(sets, func(i, j int) bool { return sets[i] < sets[j] })
	return sets
}

func commandResultType(name string) (reflect.Type, error) {
	var value any
	switch name {
	case "get_current_user":
		value = domain.User{}
	case "inspect_guild":
		value = domain.GuildInspection{}
	case "list_guild_channels", "reorder_channels":
		value = []domain.Channel{}
	case "get_channel", "create_channel", "modify_channel":
		value = domain.Channel{}
	case "delete_channel", "delete_role":
		value = domain.DeleteResult{}
	case "list_guild_roles", "reorder_roles":
		value = []domain.Role{}
	case "create_role", "modify_role":
		value = domain.Role{}
	case "list_members":
		value = []domain.Member{}
	case "get_member", "modify_member":
		value = domain.Member{}
	case "list_messages":
		value = []domain.Message{}
	case "get_message", "create_message", "modify_message":
		value = domain.Message{}
	case "bulk_delete_messages":
		value = domain.BulkDeleteResult{}
	case "list_guild_webhooks", "list_channel_webhooks":
		value = []domain.Webhook{}
	case "create_webhook", "get_webhook", "modify_webhook":
		value = domain.Webhook{}
	case "list_guild_application_commands", "bulk_overwrite_guild_application_commands":
		value = []domain.ApplicationCommand{}
	case "get_guild_application_command", "create_guild_application_command", "modify_guild_application_command":
		value = domain.ApplicationCommand{}
	case "get_guild_onboarding", "modify_guild_onboarding":
		value = domain.Onboarding{}
	case "list_guild_invites", "list_channel_invites":
		value = []domain.Invite{}
	case "get_invite", "create_invite", "delete_invite":
		value = domain.Invite{}
	case "list_scheduled_events":
		value = []domain.ScheduledEvent{}
	case "get_scheduled_event", "create_scheduled_event", "modify_scheduled_event":
		value = domain.ScheduledEvent{}
	case "list_auto_moderation_rules":
		value = []domain.AutoModerationRule{}
	case "get_auto_moderation_rule", "create_auto_moderation_rule", "modify_auto_moderation_rule":
		value = domain.AutoModerationRule{}
	case "set_channel_permission", "delete_channel_permission", "add_member_role", "remove_member_role", "remove_member", "create_member_ban", "delete_member_ban", "delete_message", "pin_message", "unpin_message", "delete_webhook", "delete_guild_application_command", "delete_scheduled_event", "delete_auto_moderation_rule":
		value = domain.EmptyResult{}
	default:
		return nil, errors.New("Discord command result contract is missing: " + name)
	}
	return reflect.TypeOf(value), nil
}
