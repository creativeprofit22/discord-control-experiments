package application

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/ken-kai/discord-control-experiments/internal/domain"
)

type CommandRegistrationScope struct {
	Kind          string
	ApplicationID string
	GuildID       string
}
type ApplicationCommandDefinition struct {
	Type                     int                                  `json:"type"`
	Name                     string                               `json:"name"`
	Description              string                               `json:"description"`
	Options                  []ApplicationCommandOptionDefinition `json:"options"`
	DefaultMemberPermissions string                               `json:"default_member_permissions"`
	DMPermission             bool                                 `json:"dm_permission"`
	IntegrationTypes         []int                                `json:"integration_types"`
	Contexts                 []int                                `json:"contexts"`
}
type ApplicationCommandOptionDefinition struct {
	Type        int                                  `json:"type"`
	Name        string                               `json:"name"`
	Description string                               `json:"description"`
	Required    bool                                 `json:"required,omitempty"`
	Options     []ApplicationCommandOptionDefinition `json:"options,omitempty"`
	Choices     []ApplicationCommandChoiceDefinition `json:"choices,omitempty"`
}
type ApplicationCommandChoiceDefinition struct {
	Name  string `json:"name"`
	Value string `json:"value"`
}
type ApplicationCommandRegistry interface {
	ListApplicationCommands(context.Context, CommandRegistrationScope) ([]ApplicationCommandDefinition, error)
	BulkOverwriteApplicationCommands(context.Context, CommandRegistrationScope, []ApplicationCommandDefinition) ([]ApplicationCommandDefinition, error)
}
type InteractionRegistrationTelemetry interface{ RecordRegistration(context.Context, string) }
type noopInteractionRegistrationTelemetry struct{}

func (noopInteractionRegistrationTelemetry) RecordRegistration(context.Context, string) {}

type CommandRegistrationService struct {
	registry  ApplicationCommandRegistry
	audit     AuditSink
	clock     Clock
	telemetry InteractionRegistrationTelemetry
}

func NewCommandRegistrationService(registry ApplicationCommandRegistry, audit AuditSink, clock Clock, supplied ...InteractionRegistrationTelemetry) (*CommandRegistrationService, error) {
	if registry == nil || audit == nil || clock == nil {
		return nil, errors.New("command registration dependencies are required")
	}
	if len(supplied) > 1 {
		return nil, errors.New("at most one registration telemetry provider is allowed")
	}
	telemetry := InteractionRegistrationTelemetry(noopInteractionRegistrationTelemetry{})
	if len(supplied) == 1 {
		if supplied[0] == nil {
			return nil, errors.New("registration telemetry is required when supplied")
		}
		telemetry = supplied[0]
	}
	return &CommandRegistrationService{registry: registry, audit: audit, clock: clock, telemetry: telemetry}, nil
}
func (s *CommandRegistrationService) Reconcile(ctx context.Context, scope CommandRegistrationScope, manifest []ApplicationCommandDefinition, correlation domain.CorrelationID) error {
	if correlation == "" || !validRegistrationScope(scope) || len(manifest) != 1 || manifest[0].Name != "discord-control" {
		return errors.New("invalid command registration request")
	}
	current, err := s.registry.ListApplicationCommands(ctx, scope)
	if err != nil {
		s.telemetry.RecordRegistration(ctx, "failed")
		return err
	}
	if semanticCommandsEqual(current, manifest) {
		s.telemetry.RecordRegistration(ctx, "noop")
		return nil
	}
	scopeClass := scope.Kind
	if err := s.audit.Append(ctx, domain.AuditEvent{Kind: "precommit", Timestamp: s.clock.Now(), Adapter: "interactions", Operation: "application_command_registration", Capability: domain.CapabilityApplicationCommandManage, Mode: domain.ModeApply, DestructiveClass: domain.DestructiveBulkOverwrite, Decision: true, Result: scopeClass, CorrelationID: correlation}); err != nil {
		s.telemetry.RecordRegistration(ctx, "failed")
		return &domain.AuditError{Stage: "precommit"}
	}
	result, err := s.registry.BulkOverwriteApplicationCommands(ctx, scope, manifest)
	outcome := "failed_" + scopeClass
	if err == nil && semanticCommandsEqual(result, manifest) {
		outcome = "completed_" + scopeClass
	} else if err == nil {
		err = errors.New("Discord command registration response mismatch")
	}
	_ = s.audit.Append(context.WithoutCancel(ctx), domain.AuditEvent{Kind: "completion", Timestamp: s.clock.Now(), Adapter: "interactions", Operation: "application_command_registration", Capability: domain.CapabilityApplicationCommandManage, Mode: domain.ModeApply, DestructiveClass: domain.DestructiveBulkOverwrite, Decision: err == nil, Result: outcome, CorrelationID: correlation})
	if err != nil {
		s.telemetry.RecordRegistration(ctx, "failed")
	} else {
		s.telemetry.RecordRegistration(ctx, "updated")
	}
	return err
}
func validRegistrationScope(scope CommandRegistrationScope) bool {
	if scope.ApplicationID == "" {
		return false
	}
	return (scope.Kind == "guild" && scope.GuildID != "") || (scope.Kind == "global" && scope.GuildID == "")
}
func semanticCommandsEqual(left, right []ApplicationCommandDefinition) bool {
	leftJSON, e1 := json.Marshal(left)
	rightJSON, e2 := json.Marshal(right)
	return e1 == nil && e2 == nil && string(leftJSON) == string(rightJSON)
}
