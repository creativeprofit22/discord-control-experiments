package discord

import (
	"encoding/json"
	"io"
	"testing"

	"github.com/ken-kai/discord-control-experiments/internal/domain"
)

func TestOnboardingRoutesAndCodec(t *testing.T) {
	context := domain.CommandContext{GuildID: "1"}
	getRoute, err := resolveRoute(domain.GetGuildOnboarding{CommandContext: context})
	if err != nil {
		t.Fatal(err)
	}
	if getRoute.method != "GET" || getRoute.path != "/guilds/1/onboarding" {
		t.Fatalf("GET route = %s %s", getRoute.method, getRoute.path)
	}
	modify := domain.ModifyGuildOnboarding{CommandContext: context, Enabled: true, Mode: 1, DefaultChannelIDs: []domain.ChannelID{"2"}, Prompts: []domain.OnboardingPromptInput{}}
	modifyRoute, err := resolveRoute(modify)
	if err != nil {
		t.Fatal(err)
	}
	if modifyRoute.method != "PUT" || modifyRoute.path != "/guilds/1/onboarding" {
		t.Fatalf("PUT route = %s %s", modifyRoute.method, modifyRoute.path)
	}
	body, _, err := encodeJSONCommand(modify)
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := io.ReadAll(body)
	if err != nil {
		t.Fatal(err)
	}
	var payload map[string]any
	if err := json.Unmarshal(encoded, &payload); err != nil {
		t.Fatal(err)
	}
	if payload["enabled"] != true || payload["mode"] != float64(1) {
		t.Fatalf("payload = %s", encoded)
	}
	result, err := decodeResult(domain.GetGuildOnboarding{CommandContext: context}, []byte(`{"guild_id":"1","prompts":[],"default_channel_ids":[],"enabled":true,"mode":1}`))
	if err != nil {
		t.Fatal(err)
	}
	if result.(domain.Onboarding).GuildID != "1" {
		t.Fatalf("result = %#v", result)
	}
}

func TestForumChannelPayloadIncludesCompleteMutableFields(t *testing.T) {
	name := "answer"
	command := domain.CreateChannel{CommandContext: domain.CommandContext{GuildID: "1"}, Name: "forum", Type: 15, AvailableTags: []domain.ForumTag{{Name: "help"}}, DefaultReactionEmoji: &domain.DefaultReaction{EmojiName: &name}, DefaultSortOrder: intPointer(1), DefaultForumLayout: intPointer(2)}
	body, _, err := encodeJSONCommand(command)
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := io.ReadAll(body)
	if err != nil {
		t.Fatal(err)
	}
	var payload map[string]any
	if err := json.Unmarshal(encoded, &payload); err != nil {
		t.Fatal(err)
	}
	for _, field := range []string{"available_tags", "default_reaction_emoji", "default_sort_order", "default_forum_layout"} {
		if _, ok := payload[field]; !ok {
			t.Fatalf("payload missing %s: %s", field, encoded)
		}
	}
}

func intPointer(value int) *int { return &value }
