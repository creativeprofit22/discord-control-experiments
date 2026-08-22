package application

import (
	"errors"
	"strings"
	"testing"

	"github.com/ken-kai/discord-control-experiments/internal/domain"
)

func TestAutoModRegistryCompleteness(t *testing.T) {
	for _, name := range []string{"list_auto_moderation_rules", "get_auto_moderation_rule", "create_auto_moderation_rule", "modify_auto_moderation_rule", "delete_auto_moderation_rule"} {
		spec, ok := LookupCommandSpec(name)
		if !ok || spec.Capability == "" || !strings.Contains(spec.RouteTemplate, "/auto-moderation/rules") {
			t.Fatalf("incomplete AutoMod spec %s: %#v", name, spec)
		}
	}
}

func TestAutoModValidationBoundaries(t *testing.T) {
	base := domain.CreateAutoModerationRule{CommandContext: domain.CommandContext{GuildID: "1", IdempotencyKey: "key", ReconciliationKey: "rec"}, Name: "rule", EventType: domain.AutoModerationEventMessageSend, TriggerType: domain.AutoModerationTriggerKeyword, TriggerMetadata: &domain.AutoModerationTriggerMetadata{KeywordFilter: []string{"blocked"}}, Actions: []domain.AutoModerationAction{{Type: domain.AutoModerationActionBlockMessage}}}
	if err := validateCommand(base); err != nil {
		t.Fatal(err)
	}
	base.TriggerMetadata.RegexPatterns = []string{strings.Repeat("x", 261)}
	var validationError *domain.ValidationError
	if err := validateCommand(base); !errors.As(err, &validationError) {
		t.Fatalf("expected typed validation error, got %v", err)
	}
}
