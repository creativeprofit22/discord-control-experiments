package cli

import "testing"

func TestParseModerationAndLockdownCommands(t *testing.T) {
	tests := [][]string{{"case", "warn", "--config", "config.json", "--guild", "1", "--input", "action.json"}, {"lockdown", "apply", "--config", "config.json", "--guild", "1", "--plan-hash", "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", "--approval-id", "approved"}}
	for _, args := range tests {
		command, err := Parse(args)
		if err != nil {
			t.Fatalf("Parse(%v): %v", args, err)
		}
		if command.GuildID != "1" {
			t.Fatalf("guild=%s", command.GuildID)
		}
		if command.Path == "lockdown apply" && (command.PlanHash == "" || command.ApprovalID == "") {
			t.Fatalf("lockdown handoff=%#v", command)
		}
	}
}

func TestParseReconciliationCommands(t *testing.T) {
	tests := []struct {
		args        []string
		name        string
		format      string
		allowDelete bool
	}{
		{[]string{"snapshot", "--config", "config.json", "--guild", "1", "--application", "2", "--output", "snapshot.json"}, "snapshot", "human", false},
		{[]string{"plan", "--config", "config.json", "--desired", "desired.json", "--output", "plan.json", "--format", "json"}, "plan", "json", false},
		{[]string{"drift", "--config", "config.json", "--plan", "plan.json"}, "drift", "human", false},
		{[]string{"apply", "--config", "config.json", "--plan", "plan.json", "--plan-hash", "abc", "--approval-id", "approval", "--allow-delete", "true"}, "apply", "human", true},
		{[]string{"resume", "--config", "config.json", "--run", "run-id", "--plan-hash", "abc", "--allow-delete", "false"}, "resume", "human", false},
	}
	for _, test := range tests {
		command, err := Parse(test.args)
		if err != nil {
			t.Fatalf("Parse(%v): %v", test.args, err)
		}
		if command.Name != test.name || command.Format != test.format || command.AllowDelete != test.allowDelete {
			t.Fatalf("Parse(%v)=%#v", test.args, command)
		}
	}
}

func TestParseRejectsUnknownDuplicateAndMalformedFlags(t *testing.T) {
	inputs := [][]string{
		{"plan", "--config", "c", "--desired", "d", "--output", "o", "--format", "yaml"},
		{"apply", "--config", "c", "--plan", "p", "--plan-hash", "h", "--plan-hash", "other"},
		{"resume", "--config", "c", "--run", "r"},
		{"snapshot", "--config", "c", "--guild", "1", "--application", "2", "--output"},
	}
	for _, input := range inputs {
		if _, err := Parse(input); err == nil {
			t.Fatalf("Parse(%v) error=nil", input)
		}
	}
}
