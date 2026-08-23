package reconcile

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"

	"github.com/ken-kai/discord-control-experiments/internal/domain"
)

func CanonicalJSON(value any) ([]byte, error) {
	encoded, err := json.Marshal(value)
	if err != nil {
		return nil, fmt.Errorf("marshal canonical JSON: %w", err)
	}
	return encoded, nil
}

func SHA256Hex(data []byte) string {
	digest := sha256.Sum256(data)
	return hex.EncodeToString(digest[:])
}

func HashCanonical(value any) (string, error) {
	encoded, err := CanonicalJSON(value)
	if err != nil {
		return "", err
	}
	return SHA256Hex(encoded), nil
}

func NormalizeDesired(desired domain.DesiredGuild) domain.DesiredGuild {
	if desired.SchemaVersion == domain.DeclarativeSchemaV1 {
		desired.SchemaVersion = domain.DeclarativeSchemaV2
		desired.AutoModerationRules = []domain.DesiredAutoModerationRule{}
	}
	if desired.Categories == nil {
		desired.Categories = []domain.DesiredCategory{}
	}
	if desired.Channels == nil {
		desired.Channels = []domain.DesiredChannel{}
	}
	if desired.Roles == nil {
		desired.Roles = []domain.DesiredRole{}
	}
	if desired.Webhooks == nil {
		desired.Webhooks = []domain.DesiredWebhook{}
	}
	if desired.ApplicationCommands == nil {
		desired.ApplicationCommands = []domain.DesiredApplicationCommand{}
	}
	if desired.AutoModerationRules == nil {
		desired.AutoModerationRules = []domain.DesiredAutoModerationRule{}
	}

	for index := range desired.Categories {
		desired.Categories[index].Mode = defaultMode(desired.Categories[index].Mode)
	}
	for index := range desired.Channels {
		channel := &desired.Channels[index]
		channel.Mode = defaultMode(channel.Mode)
		if channel.Overwrites == nil {
			channel.Overwrites = []domain.DesiredOverwrite{}
		}
		for overwriteIndex := range channel.Overwrites {
			channel.Overwrites[overwriteIndex].Allow, _ = CanonicalPermission(channel.Overwrites[overwriteIndex].Allow)
			channel.Overwrites[overwriteIndex].Deny, _ = CanonicalPermission(channel.Overwrites[overwriteIndex].Deny)
		}
		sort.Slice(channel.Overwrites, func(i, j int) bool { return refLess(channel.Overwrites[i].Target, channel.Overwrites[j].Target) })
		if channel.Forum != nil && channel.Forum.AvailableTags == nil {
			channel.Forum.AvailableTags = []domain.DesiredForumTag{}
		}
	}
	for index := range desired.Roles {
		desired.Roles[index].Mode = defaultMode(desired.Roles[index].Mode)
		desired.Roles[index].Permissions, _ = CanonicalPermission(desired.Roles[index].Permissions)
	}
	for index := range desired.Webhooks {
		desired.Webhooks[index].Mode = defaultMode(desired.Webhooks[index].Mode)
	}
	for index := range desired.AutoModerationRules {
		desired.AutoModerationRules[index].Mode = defaultMode(desired.AutoModerationRules[index].Mode)
		desired.AutoModerationRules[index] = domain.NormalizeDesiredAutoModerationRule(desired.AutoModerationRules[index])
	}
	for index := range desired.ApplicationCommands {
		command := &desired.ApplicationCommands[index]
		command.Mode = defaultMode(command.Mode)
		if command.Options == nil {
			command.Options = []domain.ApplicationCommandOption{}
		}
		if command.DefaultMemberPermissions != nil {
			canonical, _ := CanonicalPermission(*command.DefaultMemberPermissions)
			command.DefaultMemberPermissions = &canonical
		}
	}
	if desired.Onboarding != nil {
		if desired.Onboarding.DefaultChannelIDs == nil {
			desired.Onboarding.DefaultChannelIDs = []domain.StableResourceRef{}
		}
		if desired.Onboarding.Prompts == nil {
			desired.Onboarding.Prompts = []domain.DesiredOnboardingPrompt{}
		}
		for index := range desired.Onboarding.Prompts {
			prompt := &desired.Onboarding.Prompts[index]
			if prompt.Options == nil {
				prompt.Options = []domain.DesiredOnboardingOption{}
			}
			for optionIndex := range prompt.Options {
				option := &prompt.Options[optionIndex]
				if option.Channels == nil {
					option.Channels = []domain.StableResourceRef{}
				}
				if option.Roles == nil {
					option.Roles = []domain.StableResourceRef{}
				}
				sort.Slice(option.Channels, func(i, j int) bool { return refLess(option.Channels[i], option.Channels[j]) })
				sort.Slice(option.Roles, func(i, j int) bool { return refLess(option.Roles[i], option.Roles[j]) })
			}
			sort.Slice(prompt.Options, func(i, j int) bool { return prompt.Options[i].LogicalID < prompt.Options[j].LogicalID })
		}
		sort.Slice(desired.Onboarding.DefaultChannelIDs, func(i, j int) bool {
			return refLess(desired.Onboarding.DefaultChannelIDs[i], desired.Onboarding.DefaultChannelIDs[j])
		})
		sort.Slice(desired.Onboarding.Prompts, func(i, j int) bool {
			return desired.Onboarding.Prompts[i].LogicalID < desired.Onboarding.Prompts[j].LogicalID
		})
	}

	sort.Slice(desired.Categories, func(i, j int) bool { return desired.Categories[i].LogicalID < desired.Categories[j].LogicalID })
	sort.Slice(desired.Channels, func(i, j int) bool { return desired.Channels[i].LogicalID < desired.Channels[j].LogicalID })
	sort.Slice(desired.Roles, func(i, j int) bool { return desired.Roles[i].LogicalID < desired.Roles[j].LogicalID })
	sort.Slice(desired.Webhooks, func(i, j int) bool { return desired.Webhooks[i].LogicalID < desired.Webhooks[j].LogicalID })
	sort.Slice(desired.ApplicationCommands, func(i, j int) bool {
		return desired.ApplicationCommands[i].LogicalID < desired.ApplicationCommands[j].LogicalID
	})
	sort.Slice(desired.AutoModerationRules, func(i, j int) bool {
		return desired.AutoModerationRules[i].LogicalID < desired.AutoModerationRules[j].LogicalID
	})
	return desired
}

func CanonicalDesired(desired domain.DesiredGuild) ([]byte, error) {
	desired = NormalizeDesired(desired)
	if err := ValidateDesired(desired); err != nil {
		return nil, err
	}
	return CanonicalJSON(desired)
}

func DesiredHash(desired domain.DesiredGuild) (string, error) {
	encoded, err := CanonicalDesired(desired)
	if err != nil {
		return "", err
	}
	return SHA256Hex(encoded), nil
}

func defaultMode(mode domain.ManagementMode) domain.ManagementMode {
	if mode == "" {
		return domain.ManagementModeManaged
	}
	return mode
}

func refLess(left, right domain.StableResourceRef) bool {
	if left.Kind != right.Kind {
		return left.Kind < right.Kind
	}
	return left.LogicalID < right.LogicalID
}
