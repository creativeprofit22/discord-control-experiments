package cli

import (
	"bytes"
	"testing"

	"github.com/ken-kai/discord-control-experiments/internal/application"
)

func TestCommandTreeCompleteness(t *testing.T) {
	required := []string{"version", "inspect", "snapshot", "plan", "drift", "apply", "resume", "channel list", "role list", "member list", "message list", "webhook list-guild", "command list", "moderation ban", "automod list", "job list", "policy check", "health local", "diagnostics bundle", "completion powershell"}
	definitions := map[string]Definition{}
	for _, definition := range CommandDefinitions() {
		if _, duplicate := definitions[definition.Path]; duplicate {
			t.Fatalf("duplicate command %s", definition.Path)
		}
		definitions[definition.Path] = definition
	}
	for _, path := range required {
		if _, ok := definitions[path]; !ok {
			t.Errorf("missing command %s", path)
		}
	}
}
func TestOperatorDispatchCatalog(t *testing.T) {
	for _, definition := range CommandDefinitions() {
		if _, ok := application.LookupCommandSpec(definition.Handler); ok {
			command, err := application.NewOperatorDiscordCommand(definition.Handler, "1")
			if err != nil || command.CommandName() != definition.Handler {
				t.Fatalf("handler %s: %v", definition.Handler, err)
			}
		}
	}
}
func TestDiagnosticBundleSanitization(t *testing.T) {
	const canary = "token-webhook-content-canary"
	bundle, err := application.NewDiagnosticBundleService().Build(application.DiagnosticInput{Build: map[string]string{"version": canary, "commit": canary}, FailureClasses: map[string]int{canary: 1}})
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(bundle, []byte(canary)) {
		t.Fatal("diagnostic canary leaked")
	}
}
