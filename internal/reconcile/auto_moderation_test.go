package reconcile

import (
	"strings"
	"testing"
	"time"

	"github.com/ken-kai/discord-control-experiments/internal/domain"
)

func TestParseDesiredMigratesV1ToV2WithEmptyAutoModeration(t *testing.T) {
	input := `{"schema_version":"discord-control/v1","guild_id":"1","ownership":{"owner":"test","protect_deletes":true},"categories":[],"channels":[],"roles":[],"webhooks":[],"application_commands":[]}`
	desired, err := ParseDesired([]byte(input))
	if err != nil {
		t.Fatal(err)
	}
	if desired.SchemaVersion != domain.DeclarativeSchemaV2 || desired.AutoModerationRules == nil || len(desired.AutoModerationRules) != 0 {
		t.Fatalf("migration = %#v", desired)
	}
}

func TestAutoModerationPlanCreatesDisabledBeforeEnableAndReconciles(t *testing.T) {
	desired := autoModerationDesired(true)
	snapshot, err := NormalizeSnapshot(SnapshotInput{Guild: domain.Guild{ID: "1"}, CapturedAt: time.Unix(1, 0)})
	if err != nil {
		t.Fatal(err)
	}
	plan, err := BuildPlan(PlanRequest{Desired: desired, Snapshot: snapshot, ActorID: "actor", CreatedAt: time.Unix(2, 0)})
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Operations) != 2 || plan.Operations[0].Kind != domain.OperationCreate || plan.Operations[0].Payload.AutoModerationRule.Enabled || plan.Operations[1].Kind != domain.OperationUpdate || !plan.Operations[1].Payload.AutoModerationRule.Enabled || len(plan.Operations[1].Dependencies) != 1 {
		t.Fatalf("enabled create ordering = %#v", plan.Operations)
	}
	for _, operation := range plan.Operations {
		if operation.Risk != domain.RiskPermissionSensitive || !operation.RequiresApproval {
			t.Fatalf("risk = %#v", operation)
		}
	}

	observed := domain.AutoModerationRule{ID: "9", GuildID: "1", Name: "Keyword filter", EventType: domain.AutoModerationEventMessageSend, TriggerType: domain.AutoModerationTriggerKeyword, TriggerMetadata: domain.AutoModerationTriggerMetadata{KeywordFilter: []string{"bad"}}, Actions: []domain.AutoModerationAction{{Type: domain.AutoModerationActionBlockMessage}}, Enabled: true}
	binding := domain.ResourceBinding{Ref: domain.StableResourceRef{Kind: domain.ResourceAutoModerationRule, LogicalID: "keyword"}, RemoteID: "9"}
	snapshot, err = NormalizeSnapshot(SnapshotInput{Guild: domain.Guild{ID: "1"}, AutoModerationRules: []domain.AutoModerationRule{observed}, Bindings: []domain.ResourceBinding{binding}, CapturedAt: time.Unix(3, 0)})
	if err != nil {
		t.Fatal(err)
	}
	plan, err = BuildPlan(PlanRequest{Desired: desired, Snapshot: snapshot, ActorID: "actor", CreatedAt: time.Unix(4, 0)})
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Operations) != 0 {
		t.Fatalf("reconciled plan = %#v", plan.Operations)
	}
}

func TestAutoModerationAdoptionRequiresUniqueNameAndTrigger(t *testing.T) {
	desired := autoModerationDesired(false)
	desired.Ownership.AdoptByName = true
	rules := []domain.AutoModerationRule{
		{ID: "8", GuildID: "1", Name: "Keyword filter", EventType: domain.AutoModerationEventMessageSend, TriggerType: domain.AutoModerationTriggerKeyword},
		{ID: "9", GuildID: "1", Name: "Keyword filter", EventType: domain.AutoModerationEventMessageSend, TriggerType: domain.AutoModerationTriggerKeyword},
	}
	snapshot, err := NormalizeSnapshot(SnapshotInput{Guild: domain.Guild{ID: "1"}, AutoModerationRules: rules, CapturedAt: time.Unix(1, 0)})
	if err != nil {
		t.Fatal(err)
	}
	plan, err := BuildPlan(PlanRequest{Desired: desired, Snapshot: snapshot, ActorID: "actor", CreatedAt: time.Unix(2, 0)})
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Conflicts) != 1 || !strings.Contains(plan.Conflicts[0].Message, "2 unmanaged") {
		t.Fatalf("conflicts = %#v", plan.Conflicts)
	}
}

func autoModerationDesired(enabled bool) domain.DesiredGuild {
	return domain.DesiredGuild{SchemaVersion: domain.DeclarativeSchemaV2, GuildID: "1", Ownership: domain.OwnershipMetadata{Owner: "test"}, Categories: []domain.DesiredCategory{}, Channels: []domain.DesiredChannel{}, Roles: []domain.DesiredRole{}, Webhooks: []domain.DesiredWebhook{}, ApplicationCommands: []domain.DesiredApplicationCommand{}, AutoModerationRules: []domain.DesiredAutoModerationRule{{LogicalID: "keyword", Name: "Keyword filter", EventType: domain.AutoModerationEventMessageSend, TriggerType: domain.AutoModerationTriggerKeyword, TriggerMetadata: domain.AutoModerationTriggerMetadata{KeywordFilter: []string{"bad"}}, Actions: []domain.DesiredAutoModerationAction{{Type: domain.AutoModerationActionBlockMessage}}, Enabled: enabled}}}
}
