//go:generate go run ./generate/cmd -root ../..

package cli

import (
	"sort"
	"time"
)

// Definition is one immutable node in the operator command tree. Callers receive
// copies through CommandDefinitions, so parser and generated documentation share
// one deterministic source without exposing mutable registry state.
type Definition struct {
	Path         string
	Aliases      []string
	Summary      string
	Handler      string
	Flags        []FlagDefinition
	Mutation     bool
	Destructive  bool
	Input        InputPolicy
	TimeoutClass TimeoutClass
	OutputKind   string
	Examples     []string
}

type FlagDefinition struct {
	Name      string
	Value     ValueKind
	Required  bool
	Default   string
	Enum      []string
	Sensitive bool
}

type ValueKind string
type InputPolicy string
type TimeoutClass string

const (
	ValueString   ValueKind = "string"
	ValueBool     ValueKind = "boolean"
	ValueDuration ValueKind = "duration"
	ValuePath     ValueKind = "path"
	ValueInput    ValueKind = "input"

	InputNone InputPolicy = "none"
	InputJSON InputPolicy = "strict-json"

	TimeoutFast     TimeoutClass = "fast"
	TimeoutRead     TimeoutClass = "read"
	TimeoutWrite    TimeoutClass = "write"
	TimeoutWorkflow TimeoutClass = "workflow"
)

var globalFlags = []FlagDefinition{
	{Name: "--api-version", Value: ValueString, Default: APIVersion, Enum: []string{APIVersion}},
	{Name: "--config", Value: ValuePath},
	{Name: "--format", Value: ValueString, Default: "human", Enum: []string{"human", "json"}},
	{Name: "--non-interactive", Value: ValueBool, Default: "false"},
	{Name: "--timeout", Value: ValueDuration},
}

func f(name string, kind ValueKind, required bool) FlagDefinition {
	return FlagDefinition{Name: name, Value: kind, Required: required}
}

var commandDefinitions = buildDefinitions()

func buildDefinitions() []Definition {
	defs := []Definition{
		{Path: "version", Summary: "Print build and CLI API version", Handler: "version", TimeoutClass: TimeoutFast, OutputKind: "Version"},
		{Path: "help", Summary: "Show deterministic command help", Handler: "help", TimeoutClass: TimeoutFast, OutputKind: "Help"},
		{Path: "inspect", Summary: "Inspect a guild", Handler: "inspect_guild", Flags: []FlagDefinition{f("--guild", ValueString, true)}, TimeoutClass: TimeoutRead, OutputKind: "Guild"},
		{Path: "snapshot", Summary: "Capture a declarative guild snapshot", Handler: "snapshot", Flags: []FlagDefinition{f("--guild", ValueString, true), f("--application", ValueString, true), f("--output", ValuePath, true)}, TimeoutClass: TimeoutWorkflow, OutputKind: "Snapshot"},
		{Path: "plan", Summary: "Build an immutable reconciliation plan", Handler: "plan", Flags: []FlagDefinition{f("--desired", ValuePath, true), f("--output", ValuePath, true)}, TimeoutClass: TimeoutWorkflow, OutputKind: "Plan"},
		{Path: "drift", Summary: "Compare current state with a saved plan", Handler: "drift", Flags: []FlagDefinition{f("--plan", ValuePath, true)}, TimeoutClass: TimeoutRead, OutputKind: "Drift"},
		{Path: "apply", Summary: "Apply an approved immutable plan", Handler: "apply", Flags: []FlagDefinition{f("--plan", ValuePath, true), f("--plan-hash", ValueString, true), f("--approval-id", ValueString, false), f("--allow-delete", ValueBool, false)}, Mutation: true, Destructive: true, TimeoutClass: TimeoutWorkflow, OutputKind: "Apply"},
		{Path: "resume", Summary: "Resume an interrupted reconciliation run", Handler: "resume", Flags: []FlagDefinition{f("--run", ValueString, true), f("--plan-hash", ValueString, true), f("--approval-id", ValueString, false), f("--allow-delete", ValueBool, false)}, Mutation: true, Destructive: true, TimeoutClass: TimeoutWorkflow, OutputKind: "Resume"},
		{Path: "run", Summary: "Run the MCP stdio service", Handler: "run", TimeoutClass: TimeoutWorkflow, OutputKind: "Runtime"},
		{Path: "policy check", Summary: "Check an effective capability decision", Handler: "policy.check", Flags: []FlagDefinition{f("--guild", ValueString, true), f("--capability", ValueString, true), f("--mode", ValueString, true)}, TimeoutClass: TimeoutFast, OutputKind: "PolicyDecision"},
		{Path: "policy explain", Summary: "Explain a sanitized policy decision", Handler: "policy.explain", Flags: []FlagDefinition{f("--guild", ValueString, true), f("--capability", ValueString, true), f("--mode", ValueString, true)}, TimeoutClass: TimeoutFast, OutputKind: "PolicyDecision"},
		{Path: "policy catalog", Summary: "List effective command capabilities", Handler: "policy.catalog", Flags: []FlagDefinition{f("--guild", ValueString, true), f("--mode", ValueString, true)}, TimeoutClass: TimeoutFast, OutputKind: "PolicyCatalog"},
		{Path: "health local", Summary: "Check local configuration and state without egress", Handler: "health.local", TimeoutClass: TimeoutFast, OutputKind: "Health"},
		{Path: "health discord", Summary: "Run a bounded read-only Discord check", Handler: "health.discord", Flags: []FlagDefinition{f("--guild", ValueString, true)}, TimeoutClass: TimeoutRead, OutputKind: "Health"},
		{Path: "diagnostics bundle", Summary: "Write a bounded sanitized diagnostic bundle", Handler: "diagnostics.bundle", Flags: []FlagDefinition{f("--output", ValuePath, true)}, TimeoutClass: TimeoutFast, OutputKind: "Diagnostics"},
		{Path: "job list", Summary: "List durable workflow jobs", Handler: "job.list", Flags: []FlagDefinition{f("--guild", ValueString, false)}, TimeoutClass: TimeoutFast, OutputKind: "JobList"},
		{Path: "job get", Summary: "Get bounded workflow job metadata", Handler: "job.get", Flags: []FlagDefinition{f("--run", ValueString, true)}, TimeoutClass: TimeoutFast, OutputKind: "Job"},
		{Path: "job cancel", Summary: "Persist cooperative job cancellation", Handler: "job.cancel", Flags: []FlagDefinition{f("--run", ValueString, true)}, Mutation: true, TimeoutClass: TimeoutWrite, OutputKind: "Job"},
		{Path: "job resume", Summary: "Resume a recoverable durable job", Handler: "job.resume", Flags: []FlagDefinition{f("--run", ValueString, true), f("--plan-hash", ValueString, true), f("--approval-id", ValueString, false)}, Mutation: true, Destructive: true, TimeoutClass: TimeoutWorkflow, OutputKind: "Job"},
		{Path: "case create", Summary: "Create a moderation case", Handler: "moderation.case.create", Flags: []FlagDefinition{f("--guild", ValueString, true), f("--input", ValueInput, true)}, Mutation: true, Input: InputJSON, TimeoutClass: TimeoutWrite, OutputKind: "ModerationCase"},
		{Path: "case get", Summary: "Get a moderation case", Handler: "moderation.case.get", Flags: []FlagDefinition{f("--guild", ValueString, true), f("--input", ValueInput, true)}, Input: InputJSON, TimeoutClass: TimeoutRead, OutputKind: "ModerationCase"},
		{Path: "case list", Summary: "List moderation cases", Handler: "moderation.case.list", Flags: []FlagDefinition{f("--guild", ValueString, true), f("--input", ValueInput, true)}, Input: InputJSON, TimeoutClass: TimeoutRead, OutputKind: "ModerationCases"},
		{Path: "evidence attach", Summary: "Attach a structured evidence locator", Handler: "moderation.evidence.attach", Flags: []FlagDefinition{f("--guild", ValueString, true), f("--input", ValueInput, true)}, Mutation: true, Input: InputJSON, TimeoutClass: TimeoutWrite, OutputKind: "ModerationEvidence"},
		{Path: "appeal export", Summary: "Export subject-visible appeal data", Handler: "moderation.appeal.export", Flags: []FlagDefinition{f("--guild", ValueString, true), f("--input", ValueInput, true)}, Input: InputJSON, TimeoutClass: TimeoutRead, OutputKind: "AppealExport"},
		{Path: "escalation evaluate", Summary: "Evaluate deterministic escalation", Handler: "moderation.escalation.evaluate", Flags: []FlagDefinition{f("--guild", ValueString, true), f("--input", ValueInput, true)}, Input: InputJSON, TimeoutClass: TimeoutRead, OutputKind: "Escalation"},
		{Path: "escalation apply", Summary: "Apply a reviewed escalation decision", Handler: "moderation.escalation.apply", Flags: []FlagDefinition{f("--guild", ValueString, true), f("--input", ValueInput, true)}, Mutation: true, Destructive: true, Input: InputJSON, TimeoutClass: TimeoutWrite, OutputKind: "ModerationAction"},
		{Path: "lockdown plan", Summary: "Build and persist lockdown/recovery plans", Handler: "moderation.lockdown.plan", Flags: []FlagDefinition{f("--guild", ValueString, true), f("--input", ValueInput, true)}, Input: InputJSON, TimeoutClass: TimeoutWorkflow, OutputKind: "LockdownPlan"},
		{Path: "lockdown apply", Summary: "Apply a reviewed lockdown plan", Handler: "moderation.lockdown.apply", Flags: []FlagDefinition{f("--guild", ValueString, true), f("--plan-hash", ValueString, true), f("--approval-id", ValueString, true)}, Mutation: true, Destructive: true, TimeoutClass: TimeoutWorkflow, OutputKind: "LockdownApply"},
		{Path: "lockdown recover", Summary: "Apply exact reviewed lockdown recovery", Handler: "moderation.lockdown.recover", Flags: []FlagDefinition{f("--guild", ValueString, true), f("--plan-hash", ValueString, true), f("--approval-id", ValueString, true)}, Mutation: true, Destructive: true, TimeoutClass: TimeoutWorkflow, OutputKind: "LockdownRecovery"},
	}
	for _, shell := range []string{"bash", "zsh", "fish", "powershell"} {
		defs = append(defs, Definition{Path: "completion " + shell, Summary: "Generate " + shell + " completion", Handler: "completion." + shell, TimeoutClass: TimeoutFast, OutputKind: "Completion"})
	}
	resource := []struct {
		path, handler         string
		mutation, destructive bool
	}{
		{"channel list", "list_guild_channels", false, false}, {"channel get", "get_channel", false, false}, {"channel create", "create_channel", true, false}, {"channel update", "modify_channel", true, false}, {"channel delete", "delete_channel", true, true}, {"channel reorder", "reorder_channels", true, true}, {"channel set-permission", "set_channel_permission", true, true}, {"channel delete-permission", "delete_channel_permission", true, true},
		{"role list", "list_guild_roles", false, false}, {"role create", "create_role", true, false}, {"role update", "modify_role", true, false}, {"role delete", "delete_role", true, true}, {"role reorder", "reorder_roles", true, false},
		{"member list", "list_members", false, false}, {"member get", "get_member", false, false}, {"member update", "modify_member", true, false}, {"member add-role", "add_member_role", true, true}, {"member remove-role", "remove_member_role", true, false},
		{"message list", "list_messages", false, false}, {"message get", "get_message", false, false}, {"message create", "create_message", true, false}, {"message update", "modify_message", true, false}, {"message delete", "delete_message", true, true}, {"message bulk-delete", "bulk_delete_messages", true, true}, {"message pin", "pin_message", true, false}, {"message unpin", "unpin_message", true, false},
		{"webhook list-guild", "list_guild_webhooks", false, false}, {"webhook list-channel", "list_channel_webhooks", false, false}, {"webhook get", "get_webhook", false, false}, {"webhook create", "create_webhook", true, true}, {"webhook update", "modify_webhook", true, false}, {"webhook delete", "delete_webhook", true, true},
		{"command list", "list_guild_application_commands", false, false}, {"command get", "get_guild_application_command", false, false}, {"command create", "create_guild_application_command", true, false}, {"command update", "modify_guild_application_command", true, false}, {"command delete", "delete_guild_application_command", true, true}, {"command replace", "bulk_overwrite_guild_application_commands", true, true},
		{"moderation kick", "remove_member", true, true}, {"moderation ban", "create_member_ban", true, true}, {"moderation unban", "delete_member_ban", true, false},
		{"case warn", "moderation.warn", true, false}, {"case timeout", "moderation.timeout", true, false}, {"case remove-timeout", "moderation.timeout_remove", true, false}, {"case kick", "moderation.kick", true, true}, {"case ban", "moderation.ban", true, true}, {"case unban", "moderation.unban", true, false}, {"case delete-message", "moderation.message_delete", true, true}, {"case bulk-delete", "moderation.bulk_message_delete", true, true}, {"case quarantine-add", "moderation.quarantine_add", true, true}, {"case quarantine-remove", "moderation.quarantine_remove", true, true}, {"invite list", "moderation.invite_list", false, false}, {"case invite-create", "moderation.invite_create", true, true}, {"case invite-revoke", "moderation.invite_revoke", true, true},

		{"automod list", "list_auto_moderation_rules", false, false}, {"automod get", "get_auto_moderation_rule", false, false}, {"automod create", "create_auto_moderation_rule", true, false}, {"automod update", "modify_auto_moderation_rule", true, false}, {"automod delete", "delete_auto_moderation_rule", true, true},
	}
	for _, item := range resource {
		flags := []FlagDefinition{f("--guild", ValueString, true)}
		flags = append(flags, operatorIdentifierFlags(item.handler)...)
		input := InputNone
		if operatorPayloadRequired(item.handler) {
			flags = append(flags, f("--input", ValueInput, true))
			input = InputJSON
		}
		defs = append(defs, Definition{Path: item.path, Summary: item.handler, Handler: item.handler, Flags: flags, Mutation: item.mutation, Destructive: item.destructive, Input: input, TimeoutClass: map[bool]TimeoutClass{true: TimeoutWrite, false: TimeoutRead}[item.mutation], OutputKind: "DiscordResult"})
	}
	sort.Slice(defs, func(i, j int) bool { return defs[i].Path < defs[j].Path })
	return defs
}

func operatorIdentifierFlags(handler string) []FlagDefinition {
	var flags []FlagDefinition
	add := func(name string) { flags = append(flags, f(name, ValueString, true)) }
	switch handler {
	case "get_channel", "modify_channel", "delete_channel", "set_channel_permission", "delete_channel_permission", "list_messages", "get_message", "create_message", "modify_message", "delete_message", "bulk_delete_messages", "pin_message", "unpin_message", "list_channel_webhooks", "create_webhook":
		add("--channel")
	}
	switch handler {
	case "modify_role", "delete_role", "add_member_role", "remove_member_role":
		add("--role")
	}
	switch handler {
	case "get_member", "modify_member", "add_member_role", "remove_member_role", "remove_member", "create_member_ban", "delete_member_ban":
		add("--user")
	}
	switch handler {
	case "get_message", "modify_message", "delete_message", "pin_message", "unpin_message":
		add("--message")
	}
	switch handler {
	case "get_webhook", "modify_webhook", "delete_webhook":
		add("--webhook")
	}
	switch handler {
	case "list_guild_application_commands", "get_guild_application_command", "create_guild_application_command", "modify_guild_application_command", "delete_guild_application_command", "bulk_overwrite_guild_application_commands":
		add("--application")
	}
	switch handler {
	case "get_guild_application_command", "modify_guild_application_command", "delete_guild_application_command":
		add("--command")
	}
	switch handler {
	case "get_auto_moderation_rule", "modify_auto_moderation_rule", "delete_auto_moderation_rule":
		add("--rule")
	}
	if handler == "delete_channel_permission" {
		add("--overwrite")
	}
	return flags
}

func operatorPayloadRequired(handler string) bool {
	switch handler {
	case "create_channel", "modify_channel", "reorder_channels", "set_channel_permission", "create_role", "modify_role", "reorder_roles", "modify_member", "create_message", "modify_message", "bulk_delete_messages", "create_webhook", "modify_webhook", "create_guild_application_command", "modify_guild_application_command", "bulk_overwrite_guild_application_commands", "create_member_ban", "create_auto_moderation_rule", "modify_auto_moderation_rule", "moderation.warn", "moderation.timeout", "moderation.timeout_remove", "moderation.kick", "moderation.ban", "moderation.unban", "moderation.message_delete", "moderation.bulk_message_delete", "moderation.quarantine_add", "moderation.quarantine_remove", "moderation.invite_create", "moderation.invite_revoke":
		return true
	default:
		return false
	}
}

func CommandDefinitions() []Definition {
	out := make([]Definition, len(commandDefinitions))
	for i, definition := range commandDefinitions {
		out[i] = definition
		out[i].Aliases = append([]string(nil), definition.Aliases...)
		out[i].Flags = append([]FlagDefinition(nil), definition.Flags...)
		out[i].Examples = append([]string(nil), definition.Examples...)
	}
	return out
}

func GlobalFlags() []FlagDefinition { return append([]FlagDefinition(nil), globalFlags...) }

func DefaultTimeout(class TimeoutClass) time.Duration {
	switch class {
	case TimeoutFast:
		return 5 * time.Second
	case TimeoutRead:
		return 30 * time.Second
	case TimeoutWrite:
		return 45 * time.Second
	case TimeoutWorkflow:
		return 5 * time.Minute
	default:
		return 30 * time.Second
	}
}
