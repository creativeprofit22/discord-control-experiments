package discord

import (
	"testing"

	"github.com/ken-kai/discord-control-experiments/internal/application"
)

func TestRegistrationRoutesAreFixedByScope(t *testing.T) {
	cases := []struct {
		scope application.CommandRegistrationScope
		path  string
	}{
		{application.CommandRegistrationScope{Kind: "global", ApplicationID: "123"}, "/applications/123/commands"},
		{application.CommandRegistrationScope{Kind: "guild", ApplicationID: "123", GuildID: "456"}, "/applications/123/guilds/456/commands"},
	}
	for _, item := range cases {
		path, _, _, err := registrationRoute(item.scope)
		if err != nil || path != item.path {
			t.Fatalf("route = %q %v", path, err)
		}
	}
	invalid := []application.CommandRegistrationScope{{Kind: "global", ApplicationID: "123", GuildID: "456"}, {Kind: "guild", ApplicationID: "123"}, {Kind: "other", ApplicationID: "123"}}
	for _, scope := range invalid {
		if _, _, _, err := registrationRoute(scope); err == nil {
			t.Fatalf("invalid scope accepted: %#v", scope)
		}
	}
}
