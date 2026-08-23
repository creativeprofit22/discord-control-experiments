package domain

import "testing"

func TestValidateDesiredAutoModerationRuleSupportsAllTriggers(t *testing.T) {
	mentionLimit, timeout := 5, 60
	tests := []DesiredAutoModerationRule{
		{LogicalID: "keyword", Name: "Keyword", EventType: AutoModerationEventMessageSend, TriggerType: AutoModerationTriggerKeyword, TriggerMetadata: AutoModerationTriggerMetadata{KeywordFilter: []string{"bad"}}, Actions: []DesiredAutoModerationAction{{Type: AutoModerationActionBlockMessage}}},
		{LogicalID: "preset", Name: "Preset", EventType: AutoModerationEventMessageSend, TriggerType: AutoModerationTriggerKeywordPreset, TriggerMetadata: AutoModerationTriggerMetadata{Presets: []AutoModerationPresetType{1}}, Actions: []DesiredAutoModerationAction{{Type: AutoModerationActionBlockMessage}}},
		{LogicalID: "spam", Name: "Spam", EventType: AutoModerationEventMessageSend, TriggerType: AutoModerationTriggerSpam, Actions: []DesiredAutoModerationAction{{Type: AutoModerationActionBlockMessage}}},
		{LogicalID: "mention", Name: "Mention", EventType: AutoModerationEventMessageSend, TriggerType: AutoModerationTriggerMentionSpam, TriggerMetadata: AutoModerationTriggerMetadata{MentionTotalLimit: &mentionLimit}, Actions: []DesiredAutoModerationAction{{Type: AutoModerationActionBlockMessage}}},
		{LogicalID: "profile", Name: "Profile", EventType: AutoModerationEventMemberUpdate, TriggerType: AutoModerationTriggerMemberProfile, TriggerMetadata: AutoModerationTriggerMetadata{RegexPatterns: []string{"word"}}, Actions: []DesiredAutoModerationAction{{Type: AutoModerationActionBlockInteraction}}},
	}
	for _, rule := range tests {
		if err := ValidateDesiredAutoModerationRule(rule); err != nil {
			t.Fatalf("%s: %v", rule.LogicalID, err)
		}
	}
	invalidSpamTimeout := tests[2]
	invalidSpamTimeout.Actions = []DesiredAutoModerationAction{{Type: AutoModerationActionTimeout, DurationSeconds: &timeout}}
	if ValidateDesiredAutoModerationRule(invalidSpamTimeout) == nil {
		t.Fatal("spam timeout action accepted")
	}
}

func TestNormalizeDesiredAutoModerationRuleIsStable(t *testing.T) {
	rule := DesiredAutoModerationRule{TriggerMetadata: AutoModerationTriggerMetadata{KeywordFilter: []string{"z", "a"}}, Actions: []DesiredAutoModerationAction{{Type: AutoModerationActionSendAlert}, {Type: AutoModerationActionBlockMessage}}}
	normalized := NormalizeDesiredAutoModerationRule(rule)
	if normalized.TriggerMetadata.KeywordFilter[0] != "a" || normalized.Actions[0].Type != AutoModerationActionBlockMessage {
		t.Fatalf("not normalized: %#v", normalized)
	}
	if rule.TriggerMetadata.KeywordFilter[0] != "z" {
		t.Fatal("normalization mutated caller input")
	}
}
