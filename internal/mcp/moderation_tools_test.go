package mcp

import (
	"testing"

	"github.com/ken-kai/discord-control-experiments/internal/application"
)

func TestModerationToolInventoryIsTypedAndReadOnlyDefaultsExcludeMutations(t *testing.T) {
	options := testOptions(&fakeOperator{})
	options.Toolsets = []application.Toolset{application.ToolsetModeration}
	options.ReadOnly = true
	server, err := New(options)
	if err != nil {
		t.Fatal(err)
	}
	seen := map[string]RegisteredTool{}
	for _, tool := range server.Inventory() {
		seen[tool.Contract.Name] = tool
		if tool.Contract.Spec.MutationExposed {
			t.Fatalf("read-only inventory exposed %s", tool.Contract.Name)
		}
	}
	for _, name := range []string{"moderation_case_get", "moderation_case_list", "moderation_appeal_export", "moderation_escalation_evaluate", "moderation_lockdown_plan"} {
		tool, ok := seen[name]
		if !ok {
			t.Fatalf("missing %s", name)
		}
		if tool.LocalHandler == "" || tool.Input.Document["additionalProperties"] != false {
			t.Fatalf("unbounded local tool %#v", tool)
		}
	}
	if _, ok := seen["moderation_ban"]; ok {
		t.Fatal("mutation exposed by read-only inventory")
	}
}
