package main

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ken-kai/discord-control-experiments/internal/application"
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

func TestRunAutomationOnlyActorStartup(t *testing.T) {
	t.Setenv("DISCORD_BOT_TOKEN", "composition-test-token")
	root := t.TempDir()
	workflow := `{"version":1,"id":"future-post","enabled":true,"trigger":{"kind":"once","at":"2099-01-01T00:00:00Z"},"targets":{"guildIds":["123456789012345678"]},"retry":{"maxAttempts":1,"initial":"1s","maximum":"1s"},"steps":[{"action":{"kind":"message","channelId":"223456789012345678","content":"hello"}}]}`
	if err := os.WriteFile(filepath.Join(root, "workflow.json"), []byte(workflow), 0o600); err != nil {
		t.Fatal(err)
	}
	configPath := filepath.Join(root, "config.json")
	configJSON := `{"version":1,"discord":{"token":{"source":"env","name":"DISCORD_BOT_TOKEN"}},"mcp":{"enabled":false},"automation":{"enabled":true,"workflowPaths":["workflow.json"]},"observability":{"exporter":"none"},"policy":{"mode":"apply","actors":[{"id":"automation-actor","principals":[{"adapter":"automation","subject":"local-scheduler","authenticationMethod":"os-process"}],"guilds":["123456789012345678"],"capabilities":["message.manage"],"mode":"apply"}]}}`
	if err := os.WriteFile(configPath, []byte(configJSON), 0o600); err != nil {
		t.Fatal(err)
	}
	command, err := cli.Parse([]string{"run", "--config", configPath})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	result, err := run(ctx, command, &bytes.Buffer{}, &bytes.Buffer{}, "automation-startup-test")
	if err != nil {
		t.Fatalf("automation-only startup failed: %v", err)
	}
	if result.Kind != "Runtime" {
		t.Fatalf("startup result = %#v", result)
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

type runtimeEventLog struct{ events []string }

type runtimeLifecycle struct {
	name domain.Adapter
	log  *runtimeEventLog
}

func (lifecycle *runtimeLifecycle) Name() domain.Adapter { return lifecycle.name }
func (lifecycle *runtimeLifecycle) Start(context.Context) error {
	lifecycle.log.events = append(lifecycle.log.events, "start "+string(lifecycle.name))
	return nil
}
func (lifecycle *runtimeLifecycle) Stop(context.Context) error {
	lifecycle.log.events = append(lifecycle.log.events, "stop "+string(lifecycle.name))
	return nil
}

type runtimeTelemetry struct{}

func (runtimeTelemetry) Observe(ctx context.Context, _ domain.Operation, operation func(context.Context) error) error {
	return operation(ctx)
}

type runtimeClock struct{}

func (runtimeClock) Now() time.Time { return time.Unix(1_800_000_000, 0) }

func TestRuntimeCompositionStartsAndStopsInteractionsModesWithoutGateway(t *testing.T) {
	tests := []struct {
		name       string
		mcpEnabled bool
		wantEvents []string
	}{
		{name: "interactions_only", wantEvents: []string{"start discord", "start interactions", "stop interactions", "stop discord"}},
		{name: "mcp_and_interactions", mcpEnabled: true, wantEvents: []string{"start discord", "start mcp", "start interactions", "stop interactions", "stop mcp", "stop discord"}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			log := &runtimeEventLog{}
			discord := &runtimeLifecycle{name: "discord", log: log}
			interactions := &runtimeLifecycle{name: "interactions", log: log}
			var mcp application.Lifecycle
			if test.mcpEnabled {
				mcp = &runtimeLifecycle{name: "mcp", log: log}
			}
			lifecycles, err := runtimeLifecycles(discord, test.mcpEnabled, mcp, true, interactions)
			if err != nil {
				t.Fatal(err)
			}
			app, err := application.NewWithLifecycles(lifecycles, runtimeTelemetry{}, runtimeClock{}, func() (domain.CorrelationID, error) { return "runtime-composition", nil })
			if err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithCancel(context.Background())
			cancel()
			if err := app.Run(ctx); err != nil {
				t.Fatal(err)
			}
			if got, want := strings.Join(log.events, ","), strings.Join(test.wantEvents, ","); got != want {
				t.Fatalf("lifecycle events = %q, want %q", got, want)
			}
			if strings.Contains(strings.Join(log.events, ","), "gateway") {
				t.Fatal("runtime composition activated Gateway lifecycle")
			}
		})
	}
}

func TestRuntimeCompositionRejectsDroppedEnabledTransport(t *testing.T) {
	log := &runtimeEventLog{}
	_, err := runtimeLifecycles(&runtimeLifecycle{name: "discord", log: log}, false, nil, true, nil)
	if err == nil {
		t.Fatal("missing enabled interactions lifecycle was accepted")
	}
}

func TestConfiguredActorErrorNamesRequestedAdapter(t *testing.T) {
	_, err := configuredActor(config.Policy{}, domain.Principal{Adapter: "mcp", Subject: "local-stdio", AuthenticationMethod: "os-process"})
	if err == nil || err.Error() != "mcp principal is not configured in policy" {
		t.Fatalf("configuredActor error = %v", err)
	}
}
