package domain

import (
	"testing"
	"time"
)

func TestEvidenceReferenceRejectsContentBearingAndWrongShapes(t *testing.T) {
	base := EvidenceReference{ID: "e1", CaseID: "c1", GuildID: "1", SubjectUserID: "2", ActorID: "actor", CorrelationID: "corr", CreatedAt: time.Unix(1, 0)}
	tests := []EvidenceReference{
		func() EvidenceReference {
			value := base
			value.Kind = EvidenceDiscordMessage
			value.ChannelID = "3"
			value.MessageID = "4"
			return value
		}(),
		func() EvidenceReference {
			value := base
			value.Kind = EvidenceDiscordAuditEvent
			value.AuditEventID = "5"
			return value
		}(),
		func() EvidenceReference {
			value := base
			value.Kind = EvidenceExternalReference
			value.ExternalSystem = "tickets"
			value.ReferenceID = "opaque-123"
			return value
		}(),
	}
	for _, evidence := range tests {
		if err := evidence.Validate(); err != nil {
			t.Fatalf("Validate(%s): %v", evidence.Kind, err)
		}
	}
	unsafe := base
	unsafe.Kind, unsafe.ExternalSystem, unsafe.ReferenceID = EvidenceExternalReference, "tickets", "https://example.invalid/evidence"
	if unsafe.Validate() == nil {
		t.Fatal("expected URL-like evidence reference rejection")
	}
}

func TestEvaluateEscalationIsStableAndBounded(t *testing.T) {
	now := time.Unix(10_000, 0).UTC()
	policy := EscalationPolicy{Version: "policy-1", Rules: []EscalationRule{
		{ID: "second", Priority: 20, EligiblePriorKinds: []ActionKind{ActionWarn}, Lookback: time.Hour, MinimumCount: 1, Action: ActionBan, MaximumApplications: 1},
		{ID: "first", Priority: 10, EligiblePriorKinds: []ActionKind{ActionWarn}, Lookback: time.Hour, MinimumCount: 2, Action: ActionTimeout, Duration: time.Hour, MaximumApplications: 1},
	}}
	history := []ModerationAction{
		{ID: "a2", Kind: ActionWarn, Status: ActionCompleted, CreatedAt: now.Add(-time.Minute)},
		{ID: "a1", Kind: ActionWarn, Status: ActionCompleted, CreatedAt: now.Add(-2 * time.Minute)},
	}
	first, err := EvaluateEscalation(policy, history, now)
	if err != nil {
		t.Fatal(err)
	}
	second, err := EvaluateEscalation(policy, []ModerationAction{history[1], history[0]}, now)
	if err != nil {
		t.Fatal(err)
	}
	if first.RuleID != "first" || first.Action != ActionTimeout || first.PolicyHash != second.PolicyHash || first.HistoryHash != second.HistoryHash {
		t.Fatalf("unstable recommendation: %#v %#v", first, second)
	}
	history = append(history, ModerationAction{ID: "a3", Kind: ActionTimeout, Status: ActionCompleted, CreatedAt: now.Add(-time.Second)})
	capped, err := EvaluateEscalation(policy, history, now)
	if err != nil {
		t.Fatal(err)
	}
	if capped.RuleID != "second" {
		t.Fatalf("maximum applications did not cap first rule: %#v", capped)
	}
}

func TestModerationActionTimeoutBounds(t *testing.T) {
	created := time.Unix(1, 0)
	expires := created.Add(MaxDiscordTimeout + time.Second)
	action := ModerationAction{ID: "a", CaseID: "c", GuildID: "1", SubjectUserID: "2", ActorID: "actor", CorrelationID: "corr", IdempotencyKey: "key", ReasonCode: "spam", Kind: ActionTimeout, Status: ActionPending, CreatedAt: created, ExpiresAt: &expires}
	if action.Validate() == nil {
		t.Fatal("expected timeout over Discord limit to fail")
	}
}
