package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/ken-kai/discord-control-experiments/internal/application"
	"github.com/ken-kai/discord-control-experiments/internal/config"
	discordadapter "github.com/ken-kai/discord-control-experiments/internal/discord"
	"github.com/ken-kai/discord-control-experiments/internal/domain"
	jobsadapter "github.com/ken-kai/discord-control-experiments/internal/jobs"
	"github.com/ken-kai/discord-control-experiments/internal/state"
)

type automationTicker struct{ *time.Ticker }

func (t automationTicker) C() <-chan time.Time { return t.Ticker.C }

type automationClock struct{}

func (automationClock) Now() time.Time { return time.Now() }
func (automationClock) NewTicker(duration time.Duration) jobsadapter.SchedulerTicker {
	return automationTicker{time.NewTicker(duration)}
}

type automationSnapshotAdapter struct {
	service   *application.ReconciliationService
	principal domain.Principal
}

func (a automationSnapshotAdapter) Snapshot(ctx context.Context, guild domain.GuildID) error {
	_, err := a.service.Snapshot(ctx, application.SnapshotRequest{GuildID: guild, Principal: a.principal, CorrelationID: domain.CorrelationID("automation-snapshot-" + string(guild))})
	return err
}

func buildAutomationRuntime(configRoot string, resolved config.Resolved, paths state.Paths, discordClient *discordadapter.Client, discordService *application.DiscordService, reconciliation *application.ReconciliationService, approvals application.ApprovalStore, reviewIssuer application.ApprovalIssuer, policy application.PolicyEvaluator, audit application.AuditSink, telemetry application.WorkflowTelemetry) (*jobsadapter.Scheduler, *application.AutomationOperatorService, error) {
	workflows, err := loadAutomationWorkflows(configRoot, resolved.Automation)
	if err != nil {
		return nil, nil, err
	}
	store := state.NewAutomationStore(paths)
	principal := domain.Principal{Adapter: "automation", Subject: "local-scheduler", AuthenticationMethod: "os-process"}
	actorID, err := configuredActor(resolved.Policy, principal)
	if err != nil && resolved.AutomationEnabled {
		return nil, nil, fmt.Errorf("automation policy actor: %w", err)
	}
	catalog := application.DefaultAutomationCatalog()
	catalog.MaxSteps, catalog.MaxGuilds, catalog.MaxActions, catalog.MaxDestructive = resolved.Automation.MaxSteps, resolved.Automation.MaxGuildsPerRun, resolved.Automation.MaxActionsPerRun, resolved.Automation.MaxDestructivePerRun
	for _, source := range resolved.Automation.Sources {
		catalog.Sources[source.Name] = struct{}{}
	}
	for _, binding := range resolved.Automation.CleanupChannels {
		catalog.CleanupChannels[binding] = struct{}{}
	}
	for _, webhook := range resolved.Discord.Webhooks {
		if webhook.GuildID != "" {
			catalog.Webhooks[webhook.Name] = application.AutomationWebhookBinding{GuildID: webhook.GuildID, ChannelID: webhook.ChannelID}
		}
		secret, ok := resolved.DiscordWebhooks[webhook.Name]
		if ok && webhook.GuildID != "" {
			if err := discordClient.ConfigureAutomationWebhook(webhook.Name, domain.GuildID(webhook.GuildID), domain.ChannelID(webhook.ChannelID), discordadapter.TokenUse(secret.Use)); err != nil {
				return nil, nil, err
			}
		}
	}
	allowedGuilds := map[string]bool{}
	for _, actor := range resolved.Policy.Actors {
		if actor.ID == string(actorID) {
			for _, guild := range actor.Guilds {
				allowedGuilds[guild] = true
			}
		}
	}
	for _, workflow := range workflows {
		for _, guild := range workflow.Targets.GuildIDs {
			if !allowedGuilds[guild] {
				continue
			}
			binding := catalog.Guilds[guild]
			if binding.Channels == nil {
				binding.Channels = map[string]struct{}{}
			}
			if binding.Roles == nil {
				binding.Roles = map[string]struct{}{}
			}
			for _, step := range workflow.Steps {
				for _, condition := range step.Conditions {
					if condition.RoleID != "" {
						binding.Roles[condition.RoleID] = struct{}{}
					}
				}
				if step.Action.ChannelID != "" {
					binding.Channels[step.Action.ChannelID] = struct{}{}
				}
				if step.Action.RoleID != "" {
					binding.Roles[step.Action.RoleID] = struct{}{}
				}
			}
			catalog.Guilds[guild] = binding
		}
	}
	definitions := make([]jobsadapter.SourceDefinition, len(resolved.Automation.Sources))
	for i, source := range resolved.Automation.Sources {
		definitions[i] = jobsadapter.SourceDefinition{Name: source.Name, Kind: source.Kind, URL: source.URL, GitHubOwner: source.GitHubOwner, GitHubRepo: source.GitHubRepo, MaxBodyBytes: source.MaxBodyBytes}
	}
	sources, err := jobsadapter.NewSourceConnector(definitions, nil, nil)
	if err != nil {
		return nil, nil, err
	}
	var summarizer application.AutomationSummarizer
	if *resolved.Automation.Summarizer.Enabled {
		summarizer, err = jobsadapter.NewSummarizer(resolved.Automation.Summarizer.Model, resolved.AutomationSummarizerKey, sources)
		if err != nil {
			return nil, nil, err
		}
	}
	clock := automationClock{}
	actions := application.CoreAutomationExecutor{Discord: discordService, Snapshots: automationSnapshotAdapter{service: reconciliation, principal: principal}, Now: clock.Now}
	service := &application.AutomationService{Store: store, Journals: store, Actions: actions, Summarizer: summarizer, Snapshot: automationSnapshotAdapter{service: reconciliation, principal: principal}, Telemetry: telemetry, Clock: clock, Catalog: catalog, Principal: principal, ActorID: actorID, Approvals: approvals}
	authorizer, err := application.NewPolicyAutomationOperatorAuthorizer(policy, audit, clock)
	if err != nil {
		return nil, nil, err
	}
	operator, err := application.NewAutomationOperatorService(resolved.AutomationEnabled, store, service, authorizer, workflows)
	if err != nil {
		return nil, nil, err
	}
	operator.ReviewIssuer, operator.ReviewClock = reviewIssuer, clock
	operator.ReviewTTL, _ = time.ParseDuration(resolved.Approvals.MaxTTL)
	if resolved.AutomationEnabled {
		for _, workflow := range workflows {
			for _, step := range workflow.Steps {
				if !step.Action.RequireReview {
					continue
				}
				capability, destructive, authErr := application.AutomationActionAuthorization(step.Action)
				if authErr != nil {
					return nil, nil, authErr
				}
				for _, guild := range workflow.Targets.GuildIDs {
					if !configuredAutomationReviewer(context.Background(), resolved, policy, actorID, domain.GuildID(guild), capability, destructive) {
						return nil, nil, fmt.Errorf("workflow %s requires a policy-authorized separate CLI approver for guild %s", workflow.ID, guild)
					}
				}
			}
		}
	}
	if !resolved.AutomationEnabled {
		return nil, operator, nil
	}
	poll, _ := time.ParseDuration(resolved.Automation.PollInterval)
	lease, _ := time.ParseDuration(resolved.Automation.LeaseDuration)
	members := application.CoreAutomationMemberScanner{Discord: discordService, Principal: principal, ActorID: actorID}
	scheduler, err := jobsadapter.NewScheduler(store, service, sources, members, telemetry, clock, workflows, poll, lease, resolved.Automation.WorkerCount)
	return scheduler, operator, err
}

func configuredAutomationReviewer(ctx context.Context, resolved config.Resolved, policy application.PolicyEvaluator, automationActor domain.ActorID, guild domain.GuildID, capability domain.Capability, destructive domain.DestructiveClass) bool {
	for _, actor := range resolved.Policy.Actors {
		if actor.ID == string(automationActor) {
			continue
		}
		for _, configured := range actor.Principals {
			if configured.Adapter != "cli" {
				continue
			}
			principal := domain.Principal{Adapter: domain.Adapter(configured.Adapter), Subject: configured.Subject, AuthenticationMethod: configured.AuthenticationMethod}
			decision, err := policy.Decide(ctx, domain.PolicyRequest{Principal: principal, ActorID: domain.ActorID(actor.ID), GuildID: guild, Operation: "workflow.approve", Capability: capability, Mode: domain.ModeApply, DestructiveClass: destructive})
			if err == nil && decision.Allowed && decision.ActorID == domain.ActorID(actor.ID) && decision.EffectiveMode == domain.ModeApply {
				return true
			}
		}
	}
	return false
}

func loadAutomationWorkflows(root string, settings config.Automation) ([]domain.AutomationWorkflow, error) {
	root, err := filepath.Abs(root)
	if err != nil {
		return nil, err
	}
	resolvedRoot, err := filepath.EvalSymlinks(root)
	if err != nil {
		return nil, err
	}
	workflows := make([]domain.AutomationWorkflow, 0, len(settings.WorkflowPaths))
	seen := map[string]bool{}
	for _, relative := range settings.WorkflowPaths {
		candidate := filepath.Join(root, relative)
		resolvedPath, resolveErr := filepath.EvalSymlinks(candidate)
		if resolveErr != nil {
			return nil, fmt.Errorf("resolve workflow path: %w", resolveErr)
		}
		rel, relErr := filepath.Rel(resolvedRoot, resolvedPath)
		if relErr != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			return nil, errors.New("workflow path escaped configuration root")
		}
		file, openErr := os.Open(resolvedPath)
		if openErr != nil {
			return nil, fmt.Errorf("open workflow: %w", openErr)
		}
		workflow, decodeErr := domain.DecodeAutomationWorkflow(file, settings.MaxWorkflowBytes)
		closeErr := file.Close()
		if decodeErr != nil {
			return nil, decodeErr
		}
		if closeErr != nil {
			return nil, closeErr
		}
		if seen[workflow.ID] {
			return nil, errors.New("duplicate workflow ID")
		}
		seen[workflow.ID] = true
		workflows = append(workflows, workflow)
	}
	return workflows, nil
}
