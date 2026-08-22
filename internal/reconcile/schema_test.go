package reconcile

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/ken-kai/discord-control-experiments/internal/domain"
)

func TestParseDesiredStrictAndCanonical(t *testing.T) {
	input := `{
		"schema_version":"discord-control/v1",
		"guild_id":"123",
		"application_id":"456",
		"ownership":{"owner":"team","protect_deletes":true},
		"categories":[{"logical_id":"general","name":"General","position":0}],
		"channels":[{"logical_id":"chat","name":"chat","type":0,"category":{"kind":"category","logical_id":"general"},"position":0,"permission_overwrites":[{"target":{"kind":"role","logical_id":"member"},"type":0,"allow":"0008","deny":"0"}]}],
		"roles":[{"logical_id":"member","name":"Member","permissions":"0008","position":1}],
		"webhooks":[],
		"application_commands":[]
	}`
	desired, err := ParseDesired([]byte(input))
	if err != nil {
		t.Fatalf("ParseDesired() error = %v", err)
	}
	if desired.Roles[0].Permissions != "8" {
		t.Fatalf("permission = %q, want 8", desired.Roles[0].Permissions)
	}
	if desired.Channels[0].Overwrites[0].Allow != "8" {
		t.Fatalf("overwrite allow = %q, want 8", desired.Channels[0].Overwrites[0].Allow)
	}
	first, err := CanonicalDesired(desired)
	if err != nil {
		t.Fatal(err)
	}
	secondDesired, err := ParseDesired(first)
	if err != nil {
		t.Fatal(err)
	}
	second, err := CanonicalDesired(secondDesired)
	if err != nil {
		t.Fatal(err)
	}
	if string(first) != string(second) {
		t.Fatalf("canonical round trip changed\nfirst: %s\nsecond: %s", first, second)
	}
}

func TestParseDesiredRejectsHostileShapes(t *testing.T) {
	base := `{"schema_version":"discord-control/v1","guild_id":"123","ownership":{"owner":"team","protect_deletes":true},"categories":[],"channels":[],"roles":[],"webhooks":[],"application_commands":[]}`
	tests := map[string]string{
		"unknown field":       strings.Replace(base, `"categories":[]`, `"unknown":true,"categories":[]`, 1),
		"duplicate key":       strings.Replace(base, `"guild_id":"123"`, `"guild_id":"123","guild_id":"456"`, 1),
		"secret key":          strings.Replace(base, `"categories":[]`, `"webhook_url":"https://example.invalid","categories":[]`, 1),
		"negative permission": strings.Replace(base, `"roles":[]`, `"roles":[{"logical_id":"member","name":"Member","permissions":"-1","position":1}]`, 1),
		"unresolved ref":      strings.Replace(base, `"channels":[]`, `"channels":[{"logical_id":"chat","name":"chat","type":0,"category":{"kind":"category","logical_id":"missing"},"position":0,"permission_overwrites":[]}]`, 1),
	}
	for name, input := range tests {
		t.Run(name, func(t *testing.T) {
			if _, err := ParseDesired([]byte(input)); err == nil {
				t.Fatal("ParseDesired() error = nil")
			}
		})
	}
}

func TestCanonicalDesiredStableAcrossInputOrder(t *testing.T) {
	makeDesired := func(roleOrder []string) domain.DesiredGuild {
		desired := domain.DesiredGuild{
			SchemaVersion: domain.DeclarativeSchemaV1,
			GuildID:       "123",
			Ownership:     domain.OwnershipMetadata{Owner: "team", ProtectDeletes: true},
			Categories:    []domain.DesiredCategory{}, Channels: []domain.DesiredChannel{}, Webhooks: []domain.DesiredWebhook{}, ApplicationCommands: []domain.DesiredApplicationCommand{},
		}
		for _, id := range roleOrder {
			desired.Roles = append(desired.Roles, domain.DesiredRole{LogicalID: id, Name: strings.ToUpper(id), Permissions: "0", Position: 1})
		}
		return desired
	}
	left, err := DesiredHash(makeDesired([]string{"beta", "alpha"}))
	if err != nil {
		t.Fatal(err)
	}
	right, err := DesiredHash(makeDesired([]string{"alpha", "beta"}))
	if err != nil {
		t.Fatal(err)
	}
	if left != right {
		t.Fatalf("hashes differ: %s != %s", left, right)
	}
}

func TestParseDesiredRejectsExcessiveDepthAndBytes(t *testing.T) {
	deep := strings.Repeat(`{"x":`, MaxJSONDepth+1) + `0` + strings.Repeat(`}`, MaxJSONDepth+1)
	if _, err := ParseDesired([]byte(deep)); err == nil {
		t.Fatal("deep JSON accepted")
	}
	if _, err := ParseDesired(make([]byte, MaxDesiredBytes+1)); err == nil {
		t.Fatal("oversize desired accepted")
	}
}

func TestCanonicalJSONMapKeysAreStable(t *testing.T) {
	value := map[string]any{"z": 1, "a": []string{}}
	encoded, err := CanonicalJSON(value)
	if err != nil {
		t.Fatal(err)
	}
	if !json.Valid(encoded) || string(encoded) != `{"a":[],"z":1}` {
		t.Fatalf("CanonicalJSON() = %s", encoded)
	}
}
