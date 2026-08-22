package reconcile

import (
	"testing"
	"time"

	"github.com/ken-kai/discord-control-experiments/internal/domain"
)

func TestNormalizeSnapshotStableOrderingAndDispositions(t *testing.T) {
	guildID := domain.GuildID("100")
	categoryName, channelName := "General", "chat"
	snapshot, err := NormalizeSnapshot(SnapshotInput{
		Guild: domain.Guild{ID: guildID, Name: "Guild"}, CapturedAt: time.Unix(100, 0),
		Channels: []domain.Channel{
			{ID: "30", GuildID: &guildID, Type: 0, Name: &channelName, Position: 1},
			{ID: "20", GuildID: &guildID, Type: discordCategoryType, Name: &categoryName, Position: 0},
			{ID: "10", GuildID: &guildID, Type: 0, Name: &channelName, Position: 1},
		},
		Roles:    []domain.Role{{ID: "200", Name: "B", Permissions: "0", Position: 1}, {ID: "100", Name: "A", Permissions: "0", Position: 1}},
		Bindings: []domain.ResourceBinding{{Ref: domain.StableResourceRef{Kind: domain.ResourceChannel, LogicalID: "chat"}, RemoteID: "10", Fingerprint: "old"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.Channels[0].RemoteID != "10" || snapshot.Channels[1].RemoteID != "30" {
		t.Fatalf("channel order = %s, %s", snapshot.Channels[0].RemoteID, snapshot.Channels[1].RemoteID)
	}
	if snapshot.Roles[0].RemoteID != "100" || snapshot.Roles[1].RemoteID != "200" {
		t.Fatalf("role order = %s, %s", snapshot.Roles[0].RemoteID, snapshot.Roles[1].RemoteID)
	}
	if !snapshot.Channels[0].Managed || snapshot.Channels[0].Fields[0].Disposition != domain.FieldManaged {
		t.Fatal("bound channel not marked managed")
	}
	if snapshot.Channels[1].Managed || snapshot.Channels[1].Fields[0].Disposition != domain.FieldObserved {
		t.Fatal("unbound channel not marked observed")
	}
	firstHash := snapshot.Metadata.CanonicalHash
	snapshot.Metadata.CapturedAt = time.Unix(999, 0)
	secondHash, err := SnapshotHash(snapshot)
	if err != nil {
		t.Fatal(err)
	}
	if firstHash != secondHash {
		t.Fatalf("capture time changed snapshot hash: %s != %s", firstHash, secondHash)
	}
}

func TestNormalizeSnapshotRejectsCrossGuildResources(t *testing.T) {
	guildID, otherGuildID := domain.GuildID("100"), domain.GuildID("200")
	name := "chat"
	_, err := NormalizeSnapshot(SnapshotInput{Guild: domain.Guild{ID: guildID}, Channels: []domain.Channel{{ID: "1", GuildID: &otherGuildID, Name: &name}}})
	if err == nil {
		t.Fatal("cross-guild channel accepted")
	}
}

func TestMatchDesiredUsesBindingsBeforeAdoptionAndRejectsAmbiguity(t *testing.T) {
	desired := domain.DesiredGuild{
		Ownership: domain.OwnershipMetadata{AdoptByName: true},
		Channels:  []domain.DesiredChannel{{LogicalID: "chat", Name: "chat", Type: 0}, {LogicalID: "news", Name: "news", Type: 0}},
	}
	snapshot := domain.GuildSnapshot{Channels: []domain.SnapshotResource{
		{Ref: domain.StableResourceRef{Kind: domain.ResourceChannel, LogicalID: "chat"}, RemoteID: "1", Name: "renamed", Type: 0, Managed: true},
		{Ref: domain.StableResourceRef{Kind: domain.ResourceChannel, LogicalID: "observed-2"}, RemoteID: "2", Name: "news", Type: 0},
		{Ref: domain.StableResourceRef{Kind: domain.ResourceChannel, LogicalID: "observed-3"}, RemoteID: "3", Name: "news", Type: 0},
	}}
	result := MatchDesired(desired, snapshot)
	if len(result.Matches) != 1 || result.Matches[0].RemoteID != "1" {
		t.Fatalf("matches = %#v", result.Matches)
	}
	if len(result.Conflicts) != 1 || result.Conflicts[0].Kind != domain.ConflictAmbiguousMatch {
		t.Fatalf("conflicts = %#v", result.Conflicts)
	}
}
