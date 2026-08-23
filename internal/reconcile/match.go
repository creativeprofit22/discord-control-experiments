package reconcile

import (
	"fmt"
	"sort"

	"github.com/ken-kai/discord-control-experiments/internal/domain"
)

type ResourceMatch struct {
	Ref         domain.StableResourceRef `json:"ref"`
	RemoteID    domain.Snowflake         `json:"remote_id"`
	Fingerprint string                   `json:"fingerprint"`
	Adopted     bool                     `json:"adopted"`
}

type MatchResult struct {
	Matches   []ResourceMatch            `json:"matches"`
	Unmatched []domain.StableResourceRef `json:"unmatched"`
	Conflicts []domain.PlanConflict      `json:"conflicts"`
}

func MatchDesired(desired domain.DesiredGuild, snapshot domain.GuildSnapshot) MatchResult {
	resources := allSnapshotResources(snapshot)
	byRef := make(map[domain.StableResourceRef]domain.SnapshotResource)
	for _, resource := range resources {
		if resource.Managed {
			byRef[resource.Ref] = resource
		}
	}
	result := MatchResult{Matches: []ResourceMatch{}, Unmatched: []domain.StableResourceRef{}, Conflicts: []domain.PlanConflict{}}
	for _, candidate := range desiredCandidates(desired) {
		if bound, ok := byRef[candidate.ref]; ok {
			result.Matches = append(result.Matches, ResourceMatch{Ref: candidate.ref, RemoteID: bound.RemoteID, Fingerprint: bound.Fingerprint})
			continue
		}
		if !desired.Ownership.AdoptByName {
			result.Unmatched = append(result.Unmatched, candidate.ref)
			continue
		}
		matches := make([]domain.SnapshotResource, 0, 1)
		for _, resource := range resources {
			if !resource.Managed && resource.Ref.Kind == candidate.ref.Kind && resource.Name == candidate.name && resource.Type == candidate.resourceType {
				matches = append(matches, resource)
			}
		}
		switch len(matches) {
		case 0:
			result.Unmatched = append(result.Unmatched, candidate.ref)
		case 1:
			result.Matches = append(result.Matches, ResourceMatch{Ref: candidate.ref, RemoteID: matches[0].RemoteID, Fingerprint: matches[0].Fingerprint, Adopted: true})
		default:
			result.Conflicts = append(result.Conflicts, domain.PlanConflict{Kind: domain.ConflictAmbiguousMatch, Resource: candidate.ref, Message: fmt.Sprintf("%d unmanaged resources match the allowed adoption rule", len(matches))})
		}
	}
	sort.Slice(result.Matches, func(i, j int) bool { return refLess(result.Matches[i].Ref, result.Matches[j].Ref) })
	sort.Slice(result.Unmatched, func(i, j int) bool { return refLess(result.Unmatched[i], result.Unmatched[j]) })
	sort.Slice(result.Conflicts, func(i, j int) bool { return refLess(result.Conflicts[i].Resource, result.Conflicts[j].Resource) })
	return result
}

type desiredCandidate struct {
	ref          domain.StableResourceRef
	name         string
	resourceType int
}

func desiredCandidates(desired domain.DesiredGuild) []desiredCandidate {
	candidates := make([]desiredCandidate, 0, len(desired.Categories)+len(desired.Channels)+len(desired.Roles)+len(desired.Webhooks)+len(desired.ApplicationCommands)+len(desired.AutoModerationRules))
	for _, category := range desired.Categories {
		candidates = append(candidates, desiredCandidate{domain.StableResourceRef{Kind: domain.ResourceCategory, LogicalID: category.LogicalID}, category.Name, int(discordCategoryType)})
	}
	for _, channel := range desired.Channels {
		candidates = append(candidates, desiredCandidate{domain.StableResourceRef{Kind: domain.ResourceChannel, LogicalID: channel.LogicalID}, channel.Name, int(channel.Type)})
	}
	for _, role := range desired.Roles {
		candidates = append(candidates, desiredCandidate{domain.StableResourceRef{Kind: domain.ResourceRole, LogicalID: role.LogicalID}, role.Name, 0})
	}
	for _, webhook := range desired.Webhooks {
		candidates = append(candidates, desiredCandidate{domain.StableResourceRef{Kind: domain.ResourceWebhook, LogicalID: webhook.LogicalID}, webhook.Name, 1})
	}
	for _, command := range desired.ApplicationCommands {
		candidates = append(candidates, desiredCandidate{domain.StableResourceRef{Kind: domain.ResourceApplicationCommand, LogicalID: command.LogicalID}, command.Name, int(command.Type)})
	}
	for _, rule := range desired.AutoModerationRules {
		candidates = append(candidates, desiredCandidate{domain.StableResourceRef{Kind: domain.ResourceAutoModerationRule, LogicalID: rule.LogicalID}, rule.Name, int(rule.TriggerType)})
	}
	sort.Slice(candidates, func(i, j int) bool { return refLess(candidates[i].ref, candidates[j].ref) })
	return candidates
}

func allSnapshotResources(snapshot domain.GuildSnapshot) []domain.SnapshotResource {
	resources := make([]domain.SnapshotResource, 0, len(snapshot.Categories)+len(snapshot.Channels)+len(snapshot.Roles)+len(snapshot.Webhooks)+len(snapshot.ApplicationCommands)+len(snapshot.AutoModerationRules))
	resources = append(resources, snapshot.Categories...)
	resources = append(resources, snapshot.Channels...)
	resources = append(resources, snapshot.Roles...)
	resources = append(resources, snapshot.Webhooks...)
	resources = append(resources, snapshot.ApplicationCommands...)
	resources = append(resources, snapshot.AutoModerationRules...)
	return resources
}
