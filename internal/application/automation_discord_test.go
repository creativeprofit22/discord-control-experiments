package application

import (
	"context"
	"testing"
	"time"

	"github.com/ken-kai/discord-control-experiments/internal/domain"
)

func TestCoreAutomationExecutorCleanupCandidatesCallsNow(t *testing.T) {
	fakeNow := time.Now().Add(24 * time.Hour)
	calls := 0
	port := &scriptedPort{results: []DiscordPortResult{{Value: []domain.Message{{ID: "message", Timestamp: fakeNow.Add(-2 * time.Hour)}}}}}
	ownership := NewInMemoryResourceOwnershipStore()
	ownership.Record(ResourceChannel, "2", "1")
	service := newTestDiscordService(port, NewInMemoryIdempotencyStore(), ownership, nil)
	executor := CoreAutomationExecutor{Discord: service, Now: func() time.Time {
		calls++
		return fakeNow
	}}

	messages, err := executor.cleanupCandidates(context.Background(), service.defaultInvocation, domain.CommandContext{GuildID: "1"}, domain.AutomationAction{ChannelID: "2", Age: "1h", Limit: 1})
	if err != nil {
		t.Fatalf("cleanupCandidates() error = %v", err)
	}
	if calls != 1 {
		t.Fatalf("Now() calls = %d, want 1", calls)
	}
	if len(messages) != 1 || messages[0].ID != "message" {
		t.Fatalf("cleanupCandidates() = %#v, want the message selected using injected time", messages)
	}
}
