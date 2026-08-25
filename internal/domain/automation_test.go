package domain

import (
	"strings"
	"testing"
)

func validAutomationWorkflow() AutomationWorkflow {
	return AutomationWorkflow{
		Version: 1, ID: "daily-news", Enabled: true,
		Trigger: AutomationTrigger{Kind: TriggerNews, Source: "releases", Limit: 10},
		Targets: AutomationTargets{GuildIDs: []string{"123456789012345678"}},
		Retry:   AutomationRetry{MaxAttempts: 3, Initial: "1s", Maximum: "1m"},
		Steps:   []AutomationStep{{Transforms: []AutomationTransform{{Kind: TransformRender, Template: "release"}}, Action: AutomationAction{Kind: ActionMessage, ChannelID: "223456789012345678", Content: "news"}}},
	}
}

func TestAutomationStableHash(t *testing.T) {
	workflow := validAutomationWorkflow()
	first, err := workflow.Hash()
	if err != nil {
		t.Fatal(err)
	}
	second, err := workflow.Hash()
	if err != nil || first != second || len(first) != 64 {
		t.Fatalf("hashes = %q %q, %v", first, second, err)
	}
	workflow.Steps[0].Action.Content = "changed"
	third, err := workflow.Hash()
	if err != nil || third == first {
		t.Fatalf("changed hash = %q, %v", third, err)
	}
}

func TestAutomationStrictDecodeAndClosedKinds(t *testing.T) {
	valid := `{"version":1,"id":"manual-post","enabled":true,"trigger":{"kind":"manual"},"targets":{"guildIds":["123"]},"retry":{"maxAttempts":1,"initial":"1s","maximum":"1s"},"steps":[{"action":{"kind":"message","channelId":"456","content":"hello"}}]}`
	if _, err := DecodeAutomationWorkflow(strings.NewReader(valid), 4096); err != nil {
		t.Fatal(err)
	}
	for _, input := range []string{
		strings.Replace(valid, `"manual"`, `"shell"`, 1),
		strings.Replace(valid, `"message"`, `"raw_http"`, 1),
		strings.Replace(valid, `"content":"hello"`, `"content":"hello","script":"echo no"`, 1),
		strings.Replace(valid, `"id":"manual-post"`, `"id":"manual-post","route":"/api"`, 1),
		strings.Replace(valid, `"enabled":true`, `"enabled":true,"enabled":false`, 1),
	} {
		if _, err := DecodeAutomationWorkflow(strings.NewReader(input), 4096); err == nil {
			t.Fatalf("accepted unsafe workflow: %s", input)
		}
	}
}

func TestAutomationApprovedKindsAndTransitions(t *testing.T) {
	workflow := validAutomationWorkflow()
	triggers := []AutomationTrigger{{Kind: TriggerManual}, {Kind: TriggerOnce, At: "2026-01-01T00:00:00Z"}, {Kind: TriggerInterval, Every: "5m"}, {Kind: TriggerCalendar, Time: "12:00", Weekdays: []string{"monday"}}, {Kind: TriggerNews, Source: "feed", Limit: 2}, {Kind: TriggerMemberScan, ScanEvery: "10m", Limit: 10}}
	for _, trigger := range triggers {
		workflow.Trigger = trigger
		if err := workflow.Validate(100, 16); err != nil {
			t.Fatalf("trigger %s: %v", trigger.Kind, err)
		}
	}
	actions := []AutomationAction{
		{Kind: ActionMessage, ChannelID: "456"}, {Kind: ActionWebhook, Webhook: "alerts"}, {Kind: ActionForumThread, ChannelID: "456", Title: "Forum"}, {Kind: ActionMediaThread, ChannelID: "456", Title: "Media"},
		{Kind: ActionPoll, ChannelID: "456", Title: "Choose", Choices: []string{"yes", "no"}}, {Kind: ActionWelcome, ChannelID: "456"}, {Kind: ActionRoleAdd, RoleID: "789", UserID: "987"}, {Kind: ActionRoleRemove, RoleID: "789", UserID: "987"},
		{Kind: ActionCleanup, ChannelID: "456", Age: "24h", Limit: 10}, {Kind: ActionSnapshot},
	}
	workflow.Trigger = AutomationTrigger{Kind: TriggerManual}
	for _, action := range actions {
		workflow.Steps[0].Action = action
		if err := workflow.Validate(100, 16); err != nil {
			t.Fatalf("action %s: %v", action.Kind, err)
		}
	}
	if !ValidAutomationTransition(AutomationRunQueued, AutomationRunLeased) || ValidAutomationTransition(AutomationRunSucceeded, AutomationRunQueued) || ValidAutomationTransition(AutomationRunRunning, AutomationRunQueued) {
		t.Fatal("unexpected transition legality")
	}
}

func TestAutomationLimitsAndStaticTargets(t *testing.T) {
	workflow := validAutomationWorkflow()
	workflow.Targets.GuildIDs = []string{"not-an-id"}
	if err := workflow.Validate(100, 16); err == nil {
		t.Fatal("accepted dynamic guild target")
	}
	workflow = validAutomationWorkflow()
	workflow.Steps = append(workflow.Steps, workflow.Steps[0])
	if err := workflow.Validate(1, 16); err == nil {
		t.Fatal("accepted step limit overflow")
	}
}
