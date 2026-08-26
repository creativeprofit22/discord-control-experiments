package cli

import (
	"errors"
	"fmt"
	"io"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/ken-kai/discord-control-experiments/internal/domain"
)

const APIVersion = "discord-control.cli/v1"

// Command is a parsed, side-effect-free invocation. Values preserves all sealed
// command flags for application dispatch; compatibility fields keep the original
// reconciliation API source-compatible.
type Command struct {
	Name, Path, Handler                          string
	ConfigPath                                   string
	GuildID                                      domain.GuildID
	ApplicationID                                domain.ApplicationID
	DesiredPath, InputPath, OutputPath, PlanPath string
	PlanHash                                     domain.PlanHash
	ApprovalID                                   domain.ApprovalID
	RunID                                        domain.RunID
	Format, APIVersion                           string
	Timeout                                      time.Duration
	NonInteractive, AllowDelete                  bool
	Definition                                   Definition
	Values                                       map[string]string
}

type ParseError struct{ Code, Detail string }

func (e *ParseError) Error() string        { return "invalid command input: " + e.Detail }
func (e *ParseError) Is(target error) bool { _, ok := target.(*ParseError); return ok }

func Parse(args []string) (Command, error) {
	if len(args) == 0 {
		return Command{}, usageError()
	}
	definition, consumed, ok := lookupDefinition(args)
	if !ok {
		return Command{}, usageError()
	}
	command := Command{Name: strings.Fields(definition.Path)[0], Path: definition.Path, Handler: definition.Handler, Format: "human", APIVersion: APIVersion, Timeout: DefaultTimeout(definition.TimeoutClass), Definition: definition, Values: map[string]string{}}
	allowed := map[string]FlagDefinition{}
	for _, flag := range globalFlags {
		allowed[flag.Name] = flag
	}
	for _, flag := range definition.Flags {
		allowed[flag.Name] = flag
	}
	seen := map[string]bool{}
	for index := consumed; index < len(args); {
		name := args[index]
		flag, exists := allowed[name]
		if !exists {
			return Command{}, &ParseError{Code: "unknown_flag", Detail: "unknown flag " + name}
		}
		if seen[name] {
			return Command{}, &ParseError{Code: "duplicate_flag", Detail: "duplicate flag " + name}
		}
		if index+1 >= len(args) || strings.HasPrefix(args[index+1], "--") {
			return Command{}, &ParseError{Code: "missing_value", Detail: "missing value for " + name}
		}
		value := args[index+1]
		if value == "" {
			return Command{}, &ParseError{Code: "missing_value", Detail: "empty value for " + name}
		}
		if err := validateFlagValue(flag, value); err != nil {
			return Command{}, err
		}
		seen[name], command.Values[name] = true, value
		index += 2
	}
	for _, flag := range definition.Flags {
		if flag.Required && !seen[flag.Name] {
			return Command{}, &ParseError{Code: "required_flag", Detail: "missing required flag " + flag.Name}
		}
	}
	if commandRequiresConfig(definition.Path) && !seen["--config"] {
		return Command{}, &ParseError{Code: "required_flag", Detail: "missing required flag --config"}
	}
	if err := assignCompatibilityFields(&command); err != nil {
		return Command{}, err
	}
	return command, nil
}

func lookupDefinition(args []string) (Definition, int, bool) {
	for _, count := range []int{2, 1} {
		if len(args) < count {
			continue
		}
		path := strings.Join(args[:count], " ")
		for _, definition := range commandDefinitions {
			if definition.Path == path {
				return definition, count, true
			}
			for _, alias := range definition.Aliases {
				if alias == path {
					return definition, count, true
				}
			}
		}
	}
	return Definition{}, 0, false
}

func commandRequiresConfig(path string) bool {
	return path != "version" && path != "help" && path != "state verify" && path != "state restore" && !strings.HasPrefix(path, "completion ")
}

func validateFlagValue(flag FlagDefinition, value string) error {
	if len(value) > 4096 {
		return &ParseError{Code: "value_too_large", Detail: "flag value exceeds limit for " + flag.Name}
	}
	switch flag.Value {
	case ValueBool:
		if _, err := strconv.ParseBool(value); err != nil {
			return &ParseError{Code: "invalid_boolean", Detail: "invalid boolean for " + flag.Name}
		}
	case ValueDuration:
		duration, err := time.ParseDuration(value)
		if err != nil || duration <= 0 || duration > 30*time.Minute {
			return &ParseError{Code: "invalid_timeout", Detail: "timeout must be positive and at most 30m"}
		}
	case ValueInput:
		if value == "" {
			return &ParseError{Code: "invalid_input", Detail: "input must be a contained file or -"}
		}
	}
	if len(flag.Enum) > 0 {
		for _, allowed := range flag.Enum {
			if value == allowed {
				return nil
			}
		}
		return &ParseError{Code: "invalid_enum", Detail: "unsupported value for " + flag.Name}
	}
	return nil
}

func assignCompatibilityFields(command *Command) error {
	v := command.Values
	command.ConfigPath, command.DesiredPath, command.InputPath, command.OutputPath, command.PlanPath = v["--config"], v["--desired"], v["--input"], v["--output"], v["--plan"]
	command.GuildID, command.ApplicationID = domain.GuildID(v["--guild"]), domain.ApplicationID(v["--application"])
	command.PlanHash, command.ApprovalID, command.RunID = domain.PlanHash(v["--plan-hash"]), domain.ApprovalID(v["--approval-id"]), domain.RunID(v["--run"])
	if value := v["--format"]; value != "" {
		command.Format = value
	}
	if value := v["--api-version"]; value != "" {
		command.APIVersion = value
	}
	if value := v["--timeout"]; value != "" {
		command.Timeout, _ = time.ParseDuration(value)
	}
	if value := v["--non-interactive"]; value != "" {
		command.NonInteractive, _ = strconv.ParseBool(value)
	}
	if value := v["--allow-delete"]; value != "" {
		command.AllowDelete, _ = strconv.ParseBool(value)
	}
	return nil
}

func usageError() error { return &ParseError{Code: "usage", Detail: "run 'discord-control help'"} }

func Help(path string) (string, error) {
	var builder strings.Builder
	if path == "" || path == "help" {
		builder.WriteString("discord-control — deterministic Discord operator CLI\n\nCommands:\n")
		for _, definition := range commandDefinitions {
			fmt.Fprintf(&builder, "  %-30s %s\n", definition.Path, definition.Summary)
		}
		builder.WriteString("\nGlobal flags:\n")
		flags := GlobalFlags()
		sort.Slice(flags, func(i, j int) bool { return flags[i].Name < flags[j].Name })
		for _, flag := range flags {
			fmt.Fprintf(&builder, "  %s\n", flag.Name)
		}
		return builder.String(), nil
	}
	for _, definition := range commandDefinitions {
		if definition.Path != path {
			continue
		}
		fmt.Fprintf(&builder, "Usage: discord-control %s", definition.Path)
		for _, flag := range definition.Flags {
			if flag.Required {
				fmt.Fprintf(&builder, " %s VALUE", flag.Name)
			} else {
				fmt.Fprintf(&builder, " [%s VALUE]", flag.Name)
			}
		}
		builder.WriteString("\n\n" + definition.Summary + "\n")
		return builder.String(), nil
	}
	return "", &ParseError{Code: "unknown_command", Detail: "unknown help path"}
}

func WriteVersion(w io.Writer, version, commit string) error {
	_, err := fmt.Fprintf(w, "%s (%s)\n", version, commit)
	return err
}

var ErrInterrupted = errors.New("operator interrupted")
