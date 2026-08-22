package reconcile

import (
	"strings"
	"testing"
	"time"

	"github.com/ken-kai/discord-control-experiments/internal/domain"
)

func TestBuildPlanNoopAndRenameVersusRecreate(t *testing.T) {
	guildID := domain.GuildID("100")
	name := "chat"
	snapshot, err := NormalizeSnapshot(SnapshotInput{
		Guild: domain.Guild{ID: guildID}, CapturedAt: time.Unix(1, 0),
		Channels: []domain.Channel{{ID: "10", GuildID: &guildID, Type: 0, Name: &name, Position: 0}},
		Bindings: []domain.ResourceBinding{{Ref: domain.StableResourceRef{Kind: domain.ResourceChannel, LogicalID: "chat"}, RemoteID: "10"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	desired := domain.DesiredGuild{SchemaVersion: domain.DeclarativeSchemaV1, GuildID: guildID, Ownership: domain.OwnershipMetadata{Owner: "team"}, Categories: []domain.DesiredCategory{}, Roles: []domain.DesiredRole{}, Webhooks: []domain.DesiredWebhook{}, ApplicationCommands: []domain.DesiredApplicationCommand{}, Channels: []domain.DesiredChannel{{LogicalID: "chat", Name: "chat", Type: 0, Position: 0, Overwrites: []domain.DesiredOverwrite{}}}}
	plan, err := BuildPlan(PlanRequest{Desired: desired, Snapshot: snapshot, ActorID: "actor", CreatedAt: time.Unix(2, 0)})
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Operations) != 0 {
		t.Fatalf("no-op plan operations = %#v", plan.Operations)
	}
	if err := ValidatePlan(plan); err != nil {
		t.Fatalf("ValidatePlan() = %v", err)
	}

	desired.Channels[0].Name = "renamed"
	renamePlan, err := BuildPlan(PlanRequest{Desired: desired, Snapshot: snapshot, ActorID: "actor", CreatedAt: time.Unix(3, 0)})
	if err != nil {
		t.Fatal(err)
	}
	if len(renamePlan.Operations) != 1 || renamePlan.Operations[0].Kind != domain.OperationUpdate {
		t.Fatalf("rename operations = %#v", renamePlan.Operations)
	}

	desired.Channels[0].Type = 2
	recreatePlan, err := BuildPlan(PlanRequest{Desired: desired, Snapshot: snapshot, ActorID: "actor", CreatedAt: time.Unix(4, 0)})
	if err != nil {
		t.Fatal(err)
	}
	if len(recreatePlan.Operations) != 1 || recreatePlan.Operations[0].Kind != domain.OperationReplace || recreatePlan.Operations[0].Risk != domain.RiskReplacement || !recreatePlan.Operations[0].RequiresApproval {
		t.Fatalf("recreate operations = %#v", recreatePlan.Operations)
	}
}

func TestBuildPlanDAGOrdersDependenciesAndStableHash(t *testing.T) {
	guildID := domain.GuildID("100")
	snapshot, err := NormalizeSnapshot(SnapshotInput{Guild: domain.Guild{ID: guildID}, CapturedAt: time.Unix(1, 0)})
	if err != nil {
		t.Fatal(err)
	}
	categoryRef := domain.StableResourceRef{Kind: domain.ResourceCategory, LogicalID: "general"}
	desired := domain.DesiredGuild{
		SchemaVersion: domain.DeclarativeSchemaV1, GuildID: guildID, Ownership: domain.OwnershipMetadata{Owner: "team"},
		Categories: []domain.DesiredCategory{{LogicalID: "general", Name: "General", Position: 0}},
		Channels:   []domain.DesiredChannel{{LogicalID: "chat", Name: "chat", Type: 0, Category: &categoryRef, Position: 0, Overwrites: []domain.DesiredOverwrite{}}},
		Roles:      []domain.DesiredRole{}, Webhooks: []domain.DesiredWebhook{}, ApplicationCommands: []domain.DesiredApplicationCommand{},
	}
	first, err := BuildPlan(PlanRequest{Desired: desired, Snapshot: snapshot, ActorID: "actor", CreatedAt: time.Unix(2, 0)})
	if err != nil {
		t.Fatal(err)
	}
	second, err := BuildPlan(PlanRequest{Desired: desired, Snapshot: snapshot, ActorID: "actor", CreatedAt: time.Unix(999, 0)})
	if err != nil {
		t.Fatal(err)
	}
	if first.Hash != second.Hash {
		t.Fatalf("presentation time changed hash: %s != %s", first.Hash, second.Hash)
	}
	if len(first.Operations) != 2 || first.Operations[0].Resource.Kind != domain.ResourceCategory || first.Operations[1].Resource.Kind != domain.ResourceChannel || len(first.Operations[1].Dependencies) != 1 {
		t.Fatalf("operations = %#v", first.Operations)
	}
	jsonPlan, err := RenderPlanJSON(first)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(jsonPlan), string(first.Hash)) || !strings.Contains(first.Summary, "Operations: 2") {
		t.Fatalf("render output missing deterministic summary")
	}
}

func TestValidatePlanRejectsTamperedRiskAndHash(t *testing.T) {
	operation := domain.PlanOperation{Kind: domain.OperationDelete, Resource: domain.StableResourceRef{Kind: domain.ResourceChannel, LogicalID: "chat"}, PayloadFingerprint: SHA256Hex(nil)}
	operations, err := BuildDAG([]domain.PlanOperation{operation})
	if err != nil {
		t.Fatal(err)
	}
	plan := domain.Plan{SchemaVersion: domain.PlanSchemaV1, Binding: domain.PlanBinding{GuildID: "1", ActorID: "actor", DesiredHash: "a", SnapshotHash: "b"}, Operations: operations}
	hash, err := PlanContentHash(plan)
	if err != nil {
		t.Fatal(err)
	}
	plan.Hash = domain.PlanHash(hash)
	if err := ValidatePlan(plan); err != nil {
		t.Fatalf("valid plan rejected: %v", err)
	}
	plan.Operations[0].Risk = domain.RiskReadOnly
	if err := ValidatePlan(plan); err == nil {
		t.Fatal("tampered risk accepted")
	}
}
