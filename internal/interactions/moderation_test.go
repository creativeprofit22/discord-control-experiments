package interactions

import (
	"encoding/json"
	"testing"

	"github.com/ken-kai/discord-control-experiments/internal/application"
	"github.com/ken-kai/discord-control-experiments/internal/domain"
)

func TestModerationCommandParsingHasNoCallerGuildOrActorAndRendersEphemeral(t *testing.T) {
	action := CommandOption{Name: "warn", Options: []CommandOption{
		{Type: 6, Name: "user", Value: json.RawMessage(`"22"`)},
		{Type: 3, Name: "case_id", Value: json.RawMessage(`"case"`)},
		{Type: 3, Name: "reason_code", Value: json.RawMessage(`"spam"`)},
		{Type: 3, Name: "idempotency_key", Value: json.RawMessage(`"key"`)},
	}}
	data := &CommandData{Name: "discord-control", Options: []CommandOption{{Name: "moderation", Options: []CommandOption{action}}}}
	route, payload, ok := parseCommand(data)
	if !ok || route != "moderation.warn" {
		t.Fatalf("route=%s payload=%s ok=%v", route, payload, ok)
	}
	var decoded map[string]any
	if json.Unmarshal([]byte(payload), &decoded) != nil {
		t.Fatal("payload decode")
	}
	if decoded["guild_id"] != nil || decoded["actor_id"] != nil {
		t.Fatalf("untrusted identity fields=%#v", decoded)
	}
	rendered := (&Renderer{}).Render(DeferredResult{Kind: "moderation_action", Outcome: "ok", Value: application.ModerationActionResult{Action: domain.ModerationAction{Kind: domain.ActionWarn, Status: domain.ActionCompleted}}})
	if rendered.Flags != EphemeralFlag {
		t.Fatalf("flags=%d", rendered.Flags)
	}
}

func TestLockdownScopeRejectsNonSnowflakeAndDuplicates(t *testing.T) {
	if _, ok := parseChannelScope("12,not-id"); ok {
		t.Fatal("non-snowflake accepted")
	}
	if _, ok := parseChannelScope("12,12"); ok {
		t.Fatal("duplicate accepted")
	}
}
