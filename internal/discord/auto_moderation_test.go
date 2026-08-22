package discord

import (
	"testing"

	"github.com/ken-kai/discord-control-experiments/internal/domain"
)

func TestAutoModRoutesAndCodecs(t *testing.T) {
	commands := []struct {
		command      domain.Command
		method, path string
	}{
		{domain.ListAutoModerationRules{CommandContext: domain.CommandContext{GuildID: "1"}}, "GET", "/guilds/1/auto-moderation/rules"},
		{domain.GetAutoModerationRule{CommandContext: domain.CommandContext{GuildID: "1"}, RuleID: "2"}, "GET", "/guilds/1/auto-moderation/rules/2"},
		{domain.CreateAutoModerationRule{CommandContext: domain.CommandContext{GuildID: "1"}}, "POST", "/guilds/1/auto-moderation/rules"},
		{domain.ModifyAutoModerationRule{CommandContext: domain.CommandContext{GuildID: "1"}, RuleID: "2"}, "PATCH", "/guilds/1/auto-moderation/rules/2"},
		{domain.DeleteAutoModerationRule{CommandContext: domain.CommandContext{GuildID: "1"}, RuleID: "2"}, "DELETE", "/guilds/1/auto-moderation/rules/2"},
	}
	for _, fixture := range commands {
		route, err := resolveRoute(fixture.command)
		if err != nil || route.method != fixture.method || route.path != fixture.path {
			t.Fatalf("route=%#v err=%v", route, err)
		}
	}
	result, err := decodeResult(commands[0].command, []byte(`[{"id":"2","guild_id":"1","name":"rule"}]`))
	if err != nil || len(result.([]domain.AutoModerationRule)) != 1 {
		t.Fatalf("result=%#v err=%v", result, err)
	}
}
