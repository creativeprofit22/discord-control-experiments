package interactions

//go:generate go run ../../cmd/generate-interactions -root ../..

import (
	"encoding/json"
	"errors"
	"regexp"

	"github.com/ken-kai/discord-control-experiments/internal/application"
)

type ApplicationCommand struct {
	Type                     int                        `json:"type"`
	Name                     string                     `json:"name"`
	Description              string                     `json:"description"`
	Options                  []ApplicationCommandOption `json:"options"`
	DefaultMemberPermissions string                     `json:"default_member_permissions"`
	DMPermission             bool                       `json:"dm_permission"`
	IntegrationTypes         []int                      `json:"integration_types"`
	Contexts                 []int                      `json:"contexts"`
}
type ApplicationCommandOption struct {
	Type        int                        `json:"type"`
	Name        string                     `json:"name"`
	Description string                     `json:"description"`
	Required    bool                       `json:"required,omitempty"`
	Options     []ApplicationCommandOption `json:"options,omitempty"`
	Choices     []Choice                   `json:"choices,omitempty"`
}

func CommandCatalog() []ApplicationCommand {
	return []ApplicationCommand{{Type: 1, Name: "discord-control", Description: "Operate Discord control-plane jobs", DefaultMemberPermissions: "32", DMPermission: false, IntegrationTypes: []int{0}, Contexts: []int{0}, Options: []ApplicationCommandOption{
		{Type: 1, Name: "health", Description: "Check local and Discord health"},
		{Type: 1, Name: "policy", Description: "Show your effective capability decision", Options: []ApplicationCommandOption{{Type: 3, Name: "capability", Description: "Capability to evaluate", Required: true, Choices: []Choice{{Name: "Guild inspect", Value: "guild.inspect"}, {Name: "Job read", Value: "job.read"}, {Name: "Job cancel", Value: "job.cancel"}, {Name: "Job resume", Value: "job.resume"}}}}},
		{Type: 2, Name: "jobs", Description: "Inspect or control durable jobs", Options: []ApplicationCommandOption{
			{Type: 1, Name: "list", Description: "List jobs in this server"},
			{Type: 1, Name: "status", Description: "Show one job", Options: []ApplicationCommandOption{{Type: 3, Name: "run_id", Description: "Durable job run ID", Required: true}}},
			{Type: 1, Name: "cancel", Description: "Prepare job cancellation", Options: []ApplicationCommandOption{{Type: 3, Name: "run_id", Description: "Durable job run ID", Required: true}}},
			{Type: 1, Name: "resume", Description: "Review and resume a recoverable job", Options: []ApplicationCommandOption{{Type: 3, Name: "run_id", Description: "Durable job run ID", Required: true}}},
		}},
	}}}
}

var commandNamePattern = regexp.MustCompile(`^[a-z0-9_-]{1,32}$`)

func ValidateCatalog(catalog []ApplicationCommand) error {
	if len(catalog) != 1 || catalog[0].Name != "discord-control" || catalog[0].Type != 1 {
		return errors.New("catalog must contain exactly /discord-control")
	}
	var validateOptions func([]ApplicationCommandOption, int) error
	validateOptions = func(options []ApplicationCommandOption, depth int) error {
		if depth > 2 || len(options) > 25 {
			return errors.New("command option limits exceeded")
		}
		seen := map[string]bool{}
		for _, option := range options {
			if !commandNamePattern.MatchString(option.Name) || len(option.Description) < 1 || len(option.Description) > 100 || seen[option.Name] {
				return errors.New("invalid or duplicate command option")
			}
			seen[option.Name] = true
			if len(option.Options) > 0 {
				if err := validateOptions(option.Options, depth+1); err != nil {
					return err
				}
			}
		}
		return nil
	}
	if !commandNamePattern.MatchString(catalog[0].Name) || len(catalog[0].Description) > 100 {
		return errors.New("invalid command")
	}
	return validateOptions(catalog[0].Options, 0)
}

func ApplicationManifest() []application.ApplicationCommandDefinition {
	catalog := CommandCatalog()
	result := make([]application.ApplicationCommandDefinition, 0, len(catalog))
	var convertOptions func([]ApplicationCommandOption) []application.ApplicationCommandOptionDefinition
	convertOptions = func(options []ApplicationCommandOption) []application.ApplicationCommandOptionDefinition {
		converted := make([]application.ApplicationCommandOptionDefinition, 0, len(options))
		for _, option := range options {
			choices := make([]application.ApplicationCommandChoiceDefinition, 0, len(option.Choices))
			for _, choice := range option.Choices {
				choices = append(choices, application.ApplicationCommandChoiceDefinition{Name: choice.Name, Value: choice.Value})
			}
			converted = append(converted, application.ApplicationCommandOptionDefinition{Type: option.Type, Name: option.Name, Description: option.Description, Required: option.Required, Options: convertOptions(option.Options), Choices: choices})
		}
		return converted
	}
	for _, command := range catalog {
		result = append(result, application.ApplicationCommandDefinition{Type: command.Type, Name: command.Name, Description: command.Description, Options: convertOptions(command.Options), DefaultMemberPermissions: command.DefaultMemberPermissions, DMPermission: command.DMPermission, IntegrationTypes: append([]int(nil), command.IntegrationTypes...), Contexts: append([]int(nil), command.Contexts...)})
	}
	return result
}

func CanonicalCatalogJSON() ([]byte, error) {
	catalog := CommandCatalog()
	if err := ValidateCatalog(catalog); err != nil {
		return nil, err
	}
	return json.MarshalIndent(catalog, "", "  ")
}
