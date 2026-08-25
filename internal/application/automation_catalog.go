package application

import "github.com/ken-kai/discord-control-experiments/internal/domain"

type AutomationCatalog struct {
	Sources         map[string]struct{}
	Webhooks        map[string]AutomationWebhookBinding
	Guilds          map[string]AutomationGuildBinding
	CleanupChannels map[string]struct{}
	MaxSteps        int
	MaxGuilds       int
	MaxActions      int
	MaxDestructive  int
}

type AutomationWebhookBinding struct{ GuildID, ChannelID string }
type AutomationGuildBinding struct{ Channels, Roles map[string]struct{} }

func DefaultAutomationCatalog() AutomationCatalog {
	return AutomationCatalog{Sources: map[string]struct{}{}, Webhooks: map[string]AutomationWebhookBinding{}, Guilds: map[string]AutomationGuildBinding{}, CleanupChannels: map[string]struct{}{}, MaxSteps: 100, MaxGuilds: 16, MaxActions: 500, MaxDestructive: 25}
}

func destructiveAutomationAction(kind string) bool {
	return kind == domain.ActionCleanup || kind == domain.ActionRoleRemove
}
