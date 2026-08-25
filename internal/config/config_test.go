package config

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"testing"
)

const (
	canary = "canary-secret-token-4d29"
	valid  = `{"version":1,"discord":{"token":{"source":"env","name":"DISCORD_BOT_TOKEN"}},"mcp":{"enabled":true,"guildId":"123456789012345678"},"observability":{"exporter":"none"}}`
)

func TestAutomationConfigDefaultsAndBoundaries(t *testing.T) {
	resolved, err := Load(strings.NewReader(valid), resolver{"DISCORD_BOT_TOKEN": canary})
	if err != nil {
		t.Fatal(err)
	}
	if resolved.AutomationEnabled || resolved.Automation.WorkerCount != 2 || resolved.Automation.MaxSteps != 100 {
		t.Fatalf("automation defaults = %#v", resolved.Automation)
	}

	explicit := `{"version":1,"discord":{"token":{"source":"env","name":"DISCORD_BOT_TOKEN"}},"mcp":{"enabled":false},"observability":{"exporter":"none"},"secretEnvironmentAllowlist":["OPENAI_API_KEY"],"automation":{"enabled":true,"workflowPaths":["workflows/news.json"],"sources":[{"name":"releases","kind":"github_releases","githubOwner":"octocat","githubRepo":"hello-world"}],"summarizer":{"enabled":true,"model":"gpt-5-mini","apiKey":{"source":"env","name":"OPENAI_API_KEY"}}}}`
	resolved, err = Load(strings.NewReader(explicit), resolver{"DISCORD_BOT_TOKEN": canary, "OPENAI_API_KEY": "summary-canary"})
	if err != nil || !resolved.AutomationEnabled {
		t.Fatalf("enabled automation: %#v, %v", resolved.Automation, err)
	}
	for _, mutation := range []string{
		strings.Replace(explicit, `"workflows/news.json"`, `"../news.json"`, 1),
		strings.Replace(explicit, `"github_releases"`, `"raw_http"`, 1),
		strings.Replace(explicit, `"enabled":true,"workflowPaths"`, `"enabled":true,"unknownLimit":1,"workflowPaths"`, 1),
		strings.Replace(explicit, `"OPENAI_API_KEY"`, `"UNLISTED_KEY"`, 2),
	} {
		if _, err := Load(strings.NewReader(mutation), resolver{"DISCORD_BOT_TOKEN": canary, "OPENAI_API_KEY": "summary-canary"}); err == nil {
			t.Fatalf("accepted invalid automation config: %s", mutation)
		}
	}
}

type resolver map[string]string

func (r resolver) ResolveEnvironment(name string) (string, bool) {
	value, ok := r[name]
	return value, ok
}

type mutableResolver struct {
	mu     sync.RWMutex
	values map[string]string
}

func (r *mutableResolver) ResolveEnvironment(name string) (string, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	value, ok := r.values[name]
	return value, ok
}
func (r *mutableResolver) set(name, value string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.values[name] = value
}

type captureRegistrar struct {
	mu     sync.Mutex
	values []string
}

func (r *captureRegistrar) RegisterSecret(value string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.values = append(r.values, value)
}

func TestSecretReferencesRotateWithoutExposure(t *testing.T) {
	first, second := "bot-token-first-canary", "bot-token-second-canary"
	resolver := &mutableResolver{values: map[string]string{"DISCORD_BOT_TOKEN": first}}
	resolved, err := Load(strings.NewReader(valid), resolver)
	if err != nil {
		t.Fatal(err)
	}
	registrar := &captureRegistrar{}
	resolved.SecretProvider.SetRegistrar(registrar)
	used := ""
	if err := resolved.DiscordToken.Use(func(value string) error { used = value; return nil }); err != nil || used != first {
		t.Fatalf("first use: %q %v", used, err)
	}
	resolver.set("DISCORD_BOT_TOKEN", second)
	if err := resolved.DiscordToken.Use(func(value string) error { used = value; return nil }); err != nil || used != second {
		t.Fatalf("rotated use: %q %v", used, err)
	}
	outputs := []any{resolved.DiscordToken, []Secret{resolved.DiscordToken}, map[string]Secret{"token": resolved.DiscordToken}, fmt.Errorf("wrapped %v", resolved.DiscordToken)}
	for _, output := range outputs {
		encoded, _ := json.Marshal(output)
		snapshot := fmt.Sprintf("%v %#v %s", output, output, encoded)
		if strings.Contains(snapshot, first) || strings.Contains(snapshot, second) {
			t.Fatalf("secret snapshot leaked: %s", snapshot)
		}
	}
	if len(registrar.values) != 2 || registrar.values[0] != first || registrar.values[1] != second {
		t.Fatalf("registration order = %#v", registrar.values)
	}
}

func TestLoadValidAndSecretNeverSerializes(t *testing.T) {
	resolved, err := Load(strings.NewReader(valid), resolver{"DISCORD_BOT_TOKEN": canary})
	if err != nil {
		t.Fatal(err)
	}
	if resolved.Version != 1 || !resolved.MCPEnabled {
		t.Fatalf("unexpected resolved config: %#v", resolved.Diagnostics())
	}

	var logs bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&logs, nil))
	logger.Info("config", "secret", resolved.DiscordToken, "diagnostics", resolved.Diagnostics())
	jsonSecret, err := json.Marshal(resolved.DiscordToken)
	if err != nil {
		t.Fatal(err)
	}
	outputs := []string{resolved.DiscordToken.String(), fmt.Sprint(resolved.DiscordToken), fmt.Sprintf("%v %#v %q", resolved.DiscordToken, resolved.DiscordToken, resolved.DiscordToken), string(jsonSecret), logs.String()}
	for _, output := range outputs {
		if strings.Contains(output, canary) {
			t.Fatalf("secret leaked: %q", output)
		}
	}
}

func TestPolicyConfigDefaultsAndStrictValidation(t *testing.T) {
	resolved, err := Load(strings.NewReader(valid), resolver{"DISCORD_BOT_TOKEN": canary})
	if err != nil {
		t.Fatal(err)
	}
	if resolved.Policy.Mode != "read_only" || len(resolved.Policy.Actors) != 0 || resolved.Audit.Path == "" {
		t.Fatalf("defaults = %#v", resolved)
	}
	explicit := `{"version":1,"discord":{"token":{"source":"env","name":"DISCORD_BOT_TOKEN"},"webhooks":[{"name":"alerts","url":{"source":"env","name":"DISCORD_ALERT_WEBHOOK"}}]},"mcp":{"enabled":true,"guildId":"123456789012345678"},"observability":{"exporter":"none"},"secretEnvironmentAllowlist":["DISCORD_ALERT_WEBHOOK"],"policy":{"mode":"apply","actors":[{"id":"operator","principals":[{"adapter":"mcp","subject":"local-user","authenticationMethod":"os-process"}],"guilds":["123"],"capabilities":["channel.read"],"mode":"read_only"}]},"approvals":{"maxTtl":"10m"},"audit":{"path":"audit/events.jsonl","retentionDays":30,"maxBytes":1048576},"securityAlerts":{"denialThreshold":5,"denialWindow":"1m","destructiveThreshold":2,"destructiveWindow":"1m"}}`
	if _, err := Load(strings.NewReader(explicit), resolver{"DISCORD_BOT_TOKEN": canary, "DISCORD_ALERT_WEBHOOK": "https://discord.com/api/webhooks/canary"}); err != nil {
		t.Fatal(err)
	}
	rejections := []string{
		strings.Replace(explicit, `"channel.read"`, `"*"`, 1),
		strings.Replace(explicit, `"channel.read"`, `"unknown.capability"`, 1),
		strings.Replace(explicit, `"guilds":["123"]`, `"guilds":["123","123"]`, 1),
		strings.Replace(explicit, `"path":"audit/events.jsonl"`, `"path":"../events.jsonl"`, 1),
		strings.Replace(explicit, `"denialThreshold":5`, `"denialThreshold":0`, 1),
	}
	for _, input := range rejections {
		if _, err := Load(strings.NewReader(input), resolver{"DISCORD_BOT_TOKEN": canary, "DISCORD_ALERT_WEBHOOK": "https://discord.com/api/webhooks/canary"}); err == nil {
			t.Fatalf("expected rejection: %s", input)
		}
	}
}

func TestModerationConfigDefaultsAndRejectsOverlappingEscalation(t *testing.T) {
	resolved, err := Load(strings.NewReader(valid), resolver{"DISCORD_BOT_TOKEN": canary})
	if err != nil {
		t.Fatal(err)
	}
	if resolved.Moderation.PolicyVersion != "disabled-v1" || len(resolved.Moderation.ReasonCodes) != 0 || resolved.Moderation.LockdownPlanTTL != "15m" {
		t.Fatalf("moderation defaults = %#v", resolved.Moderation)
	}
	moderation := `,"moderation":{"policyVersion":"policy-1","retention":"720h","maxCasesPerGuild":100,"maxActionsPerCase":10,"maxActionsPerSubjectWindow":10,"maxActionsPerGuildWindow":100,"reasonCodes":["spam"],"quarantineRoleId":"123","protectedOperatorRoleIds":["456"],"maxLockdownOperations":20,"maxLockdownDestructiveOperations":10,"lockdownPlanTtl":"10m","escalationRules":[{"id":"a","priority":1,"eligiblePriorKinds":["warn"],"lookback":"24h","minimumCount":2,"action":"timeout","duration":"1h","maximumApplications":1},{"id":"b","priority":1,"eligiblePriorKinds":["warn","kick"],"lookback":"24h","minimumCount":3,"action":"ban","maximumApplications":1}]}`
	input := strings.TrimSuffix(valid, "}") + moderation + "}"
	if _, err := Load(strings.NewReader(input), resolver{"DISCORD_BOT_TOKEN": canary}); err == nil || !strings.Contains(err.Error(), "equal-priority") {
		t.Fatalf("expected overlapping escalation rejection, got %v", err)
	}
	input = strings.Replace(input, `"priority":1,"eligiblePriorKinds":["warn","kick"]`, `"priority":2,"eligiblePriorKinds":["warn","kick"]`, 1)
	if _, err := Load(strings.NewReader(input), resolver{"DISCORD_BOT_TOKEN": canary}); err != nil {
		t.Fatal(err)
	}
}

func TestInteractionJobCapabilitiesLoadAndUnknownFailsClosed(t *testing.T) {
	const policyConfig = `{"version":1,"discord":{"token":{"source":"env","name":"DISCORD_BOT_TOKEN"}},"mcp":{"enabled":false},"observability":{"exporter":"none"},"policy":{"mode":"apply","actors":[{"id":"interaction-operator","principals":[{"adapter":"interactions","subject":"234567890123456789","authenticationMethod":"discord-ed25519"}],"guilds":["123456789012345678"],"capabilities":[%q],"mode":"apply"}]}}`

	for _, capability := range []string{"job.read", "job.cancel", "job.resume"} {
		t.Run(capability, func(t *testing.T) {
			resolved, err := Load(strings.NewReader(fmt.Sprintf(policyConfig, capability)), resolver{"DISCORD_BOT_TOKEN": canary})
			if err != nil {
				t.Fatalf("load %s capability: %v", capability, err)
			}
			if got := resolved.Policy.Actors[0].Capabilities; len(got) != 1 || got[0] != capability {
				t.Fatalf("loaded capabilities = %#v, want [%q]", got, capability)
			}
		})
	}

	if _, err := Load(strings.NewReader(fmt.Sprintf(policyConfig, "job.unknown")), resolver{"DISCORD_BOT_TOKEN": canary}); err == nil {
		t.Fatal("unknown job capability must be rejected")
	}
}

func FuzzConfigParser(f *testing.F) {
	f.Add([]byte(valid))
	f.Add([]byte(`{"version":1}`))
	f.Fuzz(func(t *testing.T, input []byte) {
		if len(input) > MaxBytes+1 {
			return
		}
		_, _ = Load(bytes.NewReader(input), resolver{"DISCORD_BOT_TOKEN": canary})
	})
}

func TestLoadRejectsInvalidInputWithoutCanary(t *testing.T) {
	oversized := append([]byte(valid), bytes.Repeat([]byte(" "), MaxBytes+1)...)
	tests := []struct {
		name     string
		data     []byte
		resolver SecretResolver
	}{
		{"unknown field", []byte(strings.Replace(valid, `"version":1`, `"version":1,"extra":true`, 1)), resolver{"DISCORD_BOT_TOKEN": canary}},
		{"duplicate key", []byte(strings.Replace(valid, `"version":1`, `"version":1,"version":1`, 1)), resolver{"DISCORD_BOT_TOKEN": canary}},
		{"trailing JSON", []byte(valid + `{}`), resolver{"DISCORD_BOT_TOKEN": canary}},
		{"unsupported version", []byte(strings.Replace(valid, `"version":1`, `"version":2`, 1)), resolver{"DISCORD_BOT_TOKEN": canary}},
		{"missing version", []byte(strings.Replace(valid, `"version":1,`, ``, 1)), resolver{"DISCORD_BOT_TOKEN": canary}},
		{"inline source", []byte(strings.Replace(valid, `"source":"env"`, `"source":"inline"`, 1)), resolver{"DISCORD_BOT_TOKEN": canary}},
		{"file source", []byte(strings.Replace(valid, `"source":"env"`, `"source":"file"`, 1)), resolver{"DISCORD_BOT_TOKEN": canary}},
		{"command source", []byte(strings.Replace(valid, `"source":"env"`, `"source":"command"`, 1)), resolver{"DISCORD_BOT_TOKEN": canary}},
		{"remote source", []byte(strings.Replace(valid, `"source":"env"`, `"source":"https"`, 1)), resolver{"DISCORD_BOT_TOKEN": canary}},
		{"unallowlisted env", []byte(strings.Replace(valid, "DISCORD_BOT_TOKEN", "OTHER_SECRET", 1)), resolver{"OTHER_SECRET": canary}},
		{"missing env", []byte(valid), resolver{}},
		{"empty env", []byte(valid), resolver{"DISCORD_BOT_TOKEN": ""}},
		{"missing enabled", []byte(strings.Replace(valid, `"enabled":true,`, ``, 1)), resolver{"DISCORD_BOT_TOKEN": canary}},
		{"enabled without guild", []byte(strings.Replace(valid, `,"guildId":"123456789012345678"`, ``, 1)), resolver{"DISCORD_BOT_TOKEN": canary}},
		{"unknown toolset", []byte(strings.Replace(valid, `"guildId":"123456789012345678"`, `"guildId":"123456789012345678","toolsets":["raw"]`, 1)), resolver{"DISCORD_BOT_TOKEN": canary}},
		{"invalid message bound", []byte(strings.Replace(valid, `"guildId":"123456789012345678"`, `"guildId":"123456789012345678","maxMessageBytes":1024`, 1)), resolver{"DISCORD_BOT_TOKEN": canary}},
		{"invalid exporter", []byte(strings.Replace(valid, `"exporter":"none"`, `"exporter":"socket"`, 1)), resolver{"DISCORD_BOT_TOKEN": canary}},
		{"endpoint on none", []byte(strings.Replace(valid, `"exporter":"none"`, `"exporter":"none","endpoint":"https://localhost"`, 1)), resolver{"DISCORD_BOT_TOKEN": canary}},
		{"insecure OTLP", []byte(strings.Replace(valid, `"exporter":"none"`, `"exporter":"otlp-http","endpoint":"http://localhost"`, 1)), resolver{"DISCORD_BOT_TOKEN": canary}},
		{"invalid UTF-8", []byte{0xff}, resolver{"DISCORD_BOT_TOKEN": canary}},
		{"oversized", oversized, resolver{"DISCORD_BOT_TOKEN": canary}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := Load(bytes.NewReader(test.data), test.resolver)
			if err == nil {
				t.Fatal("expected rejection")
			}
			if strings.Contains(err.Error(), canary) {
				t.Fatalf("secret leaked in error: %v", err)
			}
		})
	}
}
