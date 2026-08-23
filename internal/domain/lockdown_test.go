package domain

import (
	"testing"
	"time"
)

func TestLockdownPlanHashRejectsTamperingAndOutOfScopeChanges(t *testing.T) {
	before := PermissionOverwrite{ID: "1", Type: 0, Allow: "0", Deny: "0"}
	after := PermissionOverwrite{ID: "1", Type: 0, Allow: "0", Deny: "2048"}
	plan := LockdownPlan{SchemaVersion: LockdownPlanSchemaV1, Kind: LockdownApplyPlan, GuildID: "1", ActorID: "actor", SnapshotHash: "snapshot", ProtectedRoleIDs: []RoleID{"2"}, Scope: LockdownScope{ChannelIDs: []ChannelID{"3"}}, Operations: []LockdownOperation{{ID: "op", ChannelID: "3", Changes: []LockdownOverwriteChange{{ChannelID: "3", TargetID: "1", Type: 0, Before: &before, After: &after}}}}, CreatedAt: time.Unix(1, 0), ExpiresAt: time.Unix(61, 0)}
	hash, err := plan.CanonicalHash()
	if err != nil {
		t.Fatal(err)
	}
	plan.Hash = hash
	if err := plan.VerifyHash(); err != nil {
		t.Fatal(err)
	}
	plan.Operations[0].Changes[0].After.Deny = "0"
	if plan.VerifyHash() == nil {
		t.Fatal("expected tampered plan rejection")
	}
	plan.Operations[0].ChannelID = "4"
	if plan.Validate() == nil {
		t.Fatal("expected out-of-scope operation rejection")
	}
}
