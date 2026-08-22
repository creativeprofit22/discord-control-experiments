package application

import (
	"errors"
	"testing"

	"github.com/ken-kai/discord-control-experiments/internal/domain"
)

func TestValidateDiscordResultOwnershipRejectsCrossGuildBeforeBinding(t *testing.T) {
	guild := domain.GuildID("1")
	other := domain.GuildID("2")
	command := domain.ListGuildChannels{CommandContext: domain.CommandContext{GuildID: guild}}
	err := validateDiscordResultOwnership(command, []domain.Channel{{ID: "3", GuildID: &other}})
	var ownership *domain.OwnershipError
	if !errors.As(err, &ownership) {
		t.Fatalf("error = %v, want OwnershipError", err)
	}
}

func TestValidateDiscordResultOwnershipChecksApplicationAndOnboarding(t *testing.T) {
	guild := domain.GuildID("1")
	application := domain.ApplicationID("2")
	command := domain.InspectGuild{CommandContext: domain.CommandContext{GuildID: guild}, ApplicationID: &application}
	inspection := domain.GuildInspection{Guild: domain.Guild{ID: guild}, ApplicationCommands: []domain.ApplicationCommand{{ID: "3", ApplicationID: "9", GuildID: &guild}}, Onboarding: &domain.Onboarding{GuildID: guild}}
	if err := validateDiscordResultOwnership(command, inspection); err == nil {
		t.Fatal("wrong application ID accepted")
	}
	inspection.ApplicationCommands[0].ApplicationID = application
	inspection.Onboarding.GuildID = "9"
	if err := validateDiscordResultOwnership(command, inspection); err == nil {
		t.Fatal("wrong onboarding guild accepted")
	}
}
