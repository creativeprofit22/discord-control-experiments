package upgrade

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/ken-kai/discord-control-experiments/internal/audit"
	"github.com/ken-kai/discord-control-experiments/internal/domain"
)

type environment map[string]string

func (e environment) ResolveEnvironment(name string) (string, bool) {
	value, ok := e[name]
	return value, ok
}

func TestValidateChecksConfigSecretsAuditStateAndBackupCompatibility(t *testing.T) {
	root := t.TempDir()
	configPath := filepath.Join(root, "config.json")
	configData := `{"version":1,"discord":{"token":{"source":"env","name":"DISCORD_BOT_TOKEN"}},"mcp":{"enabled":true,"guildId":"123456789012345678"},"observability":{"exporter":"none"}}`
	if err := os.WriteFile(configPath, []byte(configData), 0o600); err != nil {
		t.Fatal(err)
	}
	sink, err := audit.Open(root, "audit/events.jsonl", 64<<20, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := sink.Append(context.Background(), domain.AuditEvent{Timestamp: time.Unix(1, 0).UTC(), CorrelationID: "test", ActorID: "test", GuildID: "123456789012345678", Operation: "validate", Decision: true, Result: "ok"}); err != nil {
		t.Fatal(err)
	}
	if err := sink.Close(); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(root, "state"), 0o700); err != nil {
		t.Fatal(err)
	}
	report, err := Validate(Options{ConfigPath: configPath, Resolver: environment{"DISCORD_BOT_TOKEN": "secret-canary"}})
	if err != nil || report.ConfigVersion != 1 {
		t.Fatalf("report=%#v err=%v", report, err)
	}
	if _, err := Validate(Options{ConfigPath: configPath, Resolver: environment{}}); err == nil {
		t.Fatal("missing secret reference was accepted")
	}
	if err := os.WriteFile(filepath.Join(root, "state", "bad.json"), []byte("{"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Validate(Options{ConfigPath: configPath, Resolver: environment{"DISCORD_BOT_TOKEN": "secret-canary"}}); err == nil {
		t.Fatal("malformed state was accepted")
	}
}
