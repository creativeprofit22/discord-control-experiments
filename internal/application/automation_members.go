package application

import (
	"context"

	"github.com/ken-kai/discord-control-experiments/internal/domain"
)

type CoreAutomationMemberScanner struct {
	Discord   *DiscordService
	Principal domain.Principal
	ActorID   domain.ActorID
}

func (s CoreAutomationMemberScanner) ListAutomationMembers(ctx context.Context, guild domain.GuildID, after string, limit int) ([]domain.Member, error) {
	if limit > 1000 {
		limit = 1000
	}
	invocation := Invocation{Principal: s.Principal, ActorID: s.ActorID, RequestedMode: domain.ModeApply, CorrelationID: domain.CorrelationID("automation-member-scan-" + string(guild))}
	var afterID *domain.UserID
	if after != "" {
		value := domain.UserID(after)
		afterID = &value
	}
	result, err := ExecuteAs(ctx, s.Discord, invocation, domain.ListMembers{CommandContext: domain.CommandContext{GuildID: guild}, After: afterID, Limit: &limit})
	return result.Result, err
}
