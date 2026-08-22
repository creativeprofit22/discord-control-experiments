package reconcile

import (
	"testing"
	"time"

	"github.com/ken-kai/discord-control-experiments/internal/domain"
)

func TestPermissionInheritanceNoopAndDeletionProtection(t *testing.T) {
	guildID := domain.GuildID("100")
	channelName := "chat"
	roleRef := domain.StableResourceRef{Kind: domain.ResourceRole, LogicalID: "member"}
	channelRef := domain.StableResourceRef{Kind: domain.ResourceChannel, LogicalID: "chat"}
	snapshot, err := NormalizeSnapshot(SnapshotInput{
		Guild: domain.Guild{ID: guildID}, CapturedAt: time.Unix(1, 0),
		Channels: []domain.Channel{{ID: "30", GuildID: &guildID, Type: 0, Name: &channelName, Position: 0, PermissionOverwrites: []domain.PermissionOverwrite{{ID: "20", Type: 0, Allow: "8", Deny: "0"}}}},
		Roles:    []domain.Role{{ID: "20", Name: "Member", Permissions: "0", Position: 1}},
		Bindings: []domain.ResourceBinding{{Ref: roleRef, RemoteID: "20", Fingerprint: "role"}, {Ref: channelRef, RemoteID: "30", Fingerprint: "channel"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	desired := domain.DesiredGuild{SchemaVersion: domain.DeclarativeSchemaV1, GuildID: guildID, Ownership: domain.OwnershipMetadata{Owner: "team"}, Categories: []domain.DesiredCategory{}, Roles: []domain.DesiredRole{{LogicalID: "member", Name: "Member", Permissions: "0", Position: 1}}, Channels: []domain.DesiredChannel{{LogicalID: "chat", Name: "chat", Type: 0, Position: 0, Overwrites: []domain.DesiredOverwrite{{Target: roleRef, Type: 0, Allow: "8", Deny: "0"}}}}, Webhooks: []domain.DesiredWebhook{}, ApplicationCommands: []domain.DesiredApplicationCommand{}}
	plan, err := BuildPlan(PlanRequest{Desired: desired, Snapshot: snapshot, ActorID: "actor", CreatedAt: time.Unix(2, 0)})
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Operations) != 0 {
		t.Fatalf("inherited permission no-op produced %#v", plan.Operations)
	}

	protected := snapshot
	protected.Roles[0].Protected = true
	protected.Channels[0].Protected = true
	protected.Metadata.CanonicalHash, _ = SnapshotHash(protected)
	desired.Roles = []domain.DesiredRole{}
	desired.Channels = []domain.DesiredChannel{}
	desired.Ownership.DeleteOrphans = true
	deletePlan, err := BuildPlan(PlanRequest{Desired: desired, Snapshot: protected, ActorID: "actor", CreatedAt: time.Unix(3, 0)})
	if err != nil {
		t.Fatal(err)
	}
	if len(deletePlan.Operations) != 0 || len(deletePlan.Conflicts) == 0 || deletePlan.Conflicts[0].Kind != domain.ConflictProtectedDelete {
		t.Fatalf("protected deletion plan=%#v", deletePlan)
	}
}
