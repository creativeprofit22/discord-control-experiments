package config

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"testing"
)

const (
	canary = "canary-secret-token-4d29"
	valid  = `{"version":1,"discord":{"token":{"source":"env","name":"DISCORD_BOT_TOKEN"}},"mcp":{"enabled":true},"observability":{"exporter":"none"}}`
)

type resolver map[string]string

func (r resolver) ResolveEnvironment(name string) (string, bool) {
	value, ok := r[name]
	return value, ok
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
		{"missing enabled", []byte(strings.Replace(valid, `"mcp":{"enabled":true}`, `"mcp":{}`, 1)), resolver{"DISCORD_BOT_TOKEN": canary}},
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
