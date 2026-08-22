package main

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ken-kai/discord-control-experiments/internal/cli"
	"github.com/ken-kai/discord-control-experiments/internal/config"
	"github.com/ken-kai/discord-control-experiments/internal/domain"
)

func TestCrashOutputIsGeneric(t *testing.T) {
	var stderr bytes.Buffer
	secret := "crash-secret-canary"
	code := guardedMain(&stderr, func() error { panic(secret) })
	if code != 1 || strings.Contains(stderr.String(), secret) || stderr.String() != "discord-control terminated unexpectedly\n" {
		t.Fatalf("crash output = %q, code=%d", stderr.String(), code)
	}
}

func TestExampleMCPConfigResolvesActorAndGuild(t *testing.T) {
	t.Setenv("DISCORD_BOT_TOKEN", "example-config-test-token")
	example, err := os.Open(filepath.Join("..", "..", "config", "example.v1.json"))
	if err != nil {
		t.Fatal(err)
	}
	defer example.Close()

	resolved, err := config.Load(example, config.EnvironmentResolver{Lookup: os.LookupEnv})
	if err != nil {
		t.Fatalf("load example config: %v", err)
	}
	principal := domain.Principal{Adapter: "mcp", Subject: "local-stdio", AuthenticationMethod: "os-process"}
	actorID, err := configuredActor(resolved.Policy, principal)
	if err != nil {
		t.Fatalf("resolve MCP actor: %v", err)
	}
	if actorID != "mcp-reader" {
		t.Fatalf("MCP actor = %q, want %q", actorID, "mcp-reader")
	}
	if guildID := configuredGuildForActor(resolved.Policy, actorID); guildID != "123456789012345678" {
		t.Fatalf("MCP guild = %q, want %q", guildID, "123456789012345678")
	}
}

func TestRunMCPOnlyActorStartup(t *testing.T) {
	t.Setenv("DISCORD_BOT_TOKEN", "composition-test-token")
	configPath := filepath.Join(t.TempDir(), "config.json")
	configJSON := `{"version":1,"discord":{"token":{"source":"env","name":"DISCORD_BOT_TOKEN"}},"mcp":{"enabled":true,"guildId":"123456789012345678","mode":"read_only","toolsets":["diagnostic"],"readOnly":true,"maxMessageBytes":1048576,"maxConcurrentCalls":4,"callTimeout":"30s"},"observability":{"exporter":"none"},"policy":{"mode":"read_only","actors":[{"id":"mcp-reader","principals":[{"adapter":"mcp","subject":"local-stdio","authenticationMethod":"os-process"}],"guilds":["123456789012345678"],"capabilities":["guild.inspect"],"mode":"read_only"}]}}`
	if err := os.WriteFile(configPath, []byte(configJSON), 0o600); err != nil {
		t.Fatal(err)
	}
	command, err := cli.Parse([]string{"run", "--config", configPath})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	result, err := run(ctx, command, &bytes.Buffer{}, &bytes.Buffer{}, "mcp-startup-test")
	if err != nil {
		t.Fatalf("MCP-only startup failed: %v", err)
	}
	if result.Kind != "Runtime" || result.Value.(map[string]string)["status"] != "stopped" {
		t.Fatalf("startup result = %#v", result)
	}
}

func TestConfiguredActorErrorNamesRequestedAdapter(t *testing.T) {
	_, err := configuredActor(config.Policy{}, domain.Principal{Adapter: "mcp", Subject: "local-stdio", AuthenticationMethod: "os-process"})
	if err == nil || err.Error() != "mcp principal is not configured in policy" {
		t.Fatalf("configuredActor error = %v", err)
	}
}
