package application

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"sort"
	"strings"
	"time"

	"github.com/ken-kai/discord-control-experiments/internal/domain"
)

type PolicyView struct {
	Allowed     bool                 `json:"allowed"`
	ActorID     domain.ActorID       `json:"actorId,omitempty"`
	GuildID     domain.GuildID       `json:"guildId"`
	Capability  domain.Capability    `json:"capability"`
	Mode        domain.ExecutionMode `json:"mode"`
	Reason      domain.PolicyReason  `json:"reason"`
	Preapproved bool                 `json:"preapproved,omitempty"`
}
type CapabilityView struct {
	Operation   domain.OperationName    `json:"operation"`
	Capability  domain.Capability       `json:"capability"`
	Mutation    bool                    `json:"mutation"`
	Destructive domain.DestructiveClass `json:"destructiveClass"`
}
type PolicyFacade struct{ policy PolicyEvaluator }

func NewPolicyFacade(policy PolicyEvaluator) (*PolicyFacade, error) {
	if policy == nil {
		return nil, errors.New("policy facade requires evaluator")
	}
	return &PolicyFacade{policy: policy}, nil
}
func (s *PolicyFacade) Check(ctx context.Context, invocation Invocation, guild domain.GuildID, capability domain.Capability, mode domain.ExecutionMode) (PolicyView, error) {
	decision, err := s.policy.Decide(ctx, domain.PolicyRequest{Principal: invocation.Principal, ActorID: invocation.ActorID, GuildID: guild, Operation: "policy_check", Capability: capability, Mode: mode, DestructiveClass: domain.DestructiveNone, CorrelationID: invocation.CorrelationID})
	if err != nil {
		return PolicyView{}, err
	}
	return PolicyView{Allowed: decision.Allowed, ActorID: decision.ActorID, GuildID: guild, Capability: capability, Mode: decision.EffectiveMode, Reason: decision.Reason, Preapproved: decision.Preapproved}, nil
}
func (s *PolicyFacade) Catalog() []CapabilityView {
	specs := CommandCatalog()
	result := make([]CapabilityView, 0, len(specs))
	for _, spec := range specs {
		result = append(result, CapabilityView{Operation: domain.OperationName(spec.Name), Capability: spec.Capability, Mutation: spec.MutationExposed, Destructive: spec.DestructiveClass})
	}
	sort.Slice(result, func(i, j int) bool { return result[i].Operation < result[j].Operation })
	return result
}

type HealthCheck struct {
	Name   string `json:"name"`
	Status string `json:"status"`
	Detail string `json:"detail,omitempty"`
}
type HealthResult struct {
	Status string        `json:"status"`
	Checks []HealthCheck `json:"checks"`
}
type HealthService struct {
	discord *DiscordService
	local   func(context.Context) []HealthCheck
}

func NewHealthService(discord *DiscordService, local func(context.Context) []HealthCheck) (*HealthService, error) {
	if discord == nil || local == nil {
		return nil, errors.New("health service dependencies are required")
	}
	return &HealthService{discord: discord, local: local}, nil
}
func (s *HealthService) Local(ctx context.Context) HealthResult {
	checks := s.local(ctx)
	sort.Slice(checks, func(i, j int) bool { return checks[i].Name < checks[j].Name })
	status := "ok"
	for _, check := range checks {
		if check.Status != "ok" {
			status = "degraded"
		}
	}
	return HealthResult{Status: status, Checks: checks}
}
func (s *HealthService) Discord(ctx context.Context, invocation Invocation, guild domain.GuildID) (HealthResult, error) {
	invocation.RequestedMode = domain.ModeReadOnly
	execution, err := s.discord.dispatchDiscord(ctx, invocation, domain.ListGuildChannels{CommandContext: domain.CommandContext{GuildID: guild}})
	if err != nil {
		return HealthResult{Status: "unavailable", Checks: []HealthCheck{{Name: "discord", Status: "failed"}}}, err
	}
	return HealthResult{Status: "ok", Checks: []HealthCheck{{Name: "discord", Status: "ok", Detail: attemptDetail(execution.Attempts)}}}, nil
}
func attemptDetail(attempts int) string {
	if attempts > 1 {
		return "retried"
	}
	return "reachable"
}

type DiagnosticInput struct {
	Build          map[string]string `json:"build"`
	ConfigChecks   []HealthCheck     `json:"configChecks"`
	Health         HealthResult      `json:"health"`
	Jobs           []JobRecord       `json:"jobs"`
	FailureClasses map[string]int    `json:"failureClasses"`
}
type DiagnosticBundleService struct{}

func NewDiagnosticBundleService() *DiagnosticBundleService { return &DiagnosticBundleService{} }
func (*DiagnosticBundleService) Build(input DiagnosticInput) ([]byte, error) {
	input = sanitizeDiagnosticInput(input)
	entries := map[string]any{"build.json": input.Build, "config-diagnostics.json": input.ConfigChecks, "failure-summary.json": input.FailureClasses, "health.json": input.Health, "jobs.json": input.Jobs, "manifest.json": map[string]any{"schemaVersion": "discord-control.diagnostics/v1", "entries": []string{"build.json", "config-diagnostics.json", "failure-summary.json", "health.json", "jobs.json", "manifest.json"}}}
	names := make([]string, 0, len(entries))
	for name := range entries {
		names = append(names, name)
	}
	sort.Strings(names)
	var buffer bytes.Buffer
	writer := zip.NewWriter(&buffer)
	fixed := time.Date(1980, 1, 1, 0, 0, 0, 0, time.UTC)
	for _, name := range names {
		payload, err := json.Marshal(entries[name])
		if err != nil {
			return nil, err
		}
		if len(payload) > 256<<10 {
			return nil, errors.New("diagnostic entry exceeds limit")
		}
		header := &zip.FileHeader{Name: name, Method: zip.Deflate, Modified: fixed}
		header.SetMode(0o600)
		entry, err := writer.CreateHeader(header)
		if err != nil {
			return nil, err
		}
		if _, err = entry.Write(payload); err != nil {
			return nil, err
		}
	}
	if err := writer.Close(); err != nil {
		return nil, err
	}
	if buffer.Len() > 4<<20 {
		return nil, errors.New("diagnostic bundle exceeds limit")
	}
	return buffer.Bytes(), nil
}

func sanitizeDiagnosticInput(input DiagnosticInput) DiagnosticInput {
	build := map[string]string{"version": "unknown", "commit": "unknown"}
	if value := input.Build["version"]; len(value) <= 32 && (strings.HasPrefix(value, "v") || value >= "0" && value <= "9") {
		build["version"] = value
	}
	if value := input.Build["commit"]; len(value) >= 7 && len(value) <= 64 && hexadecimal(value) {
		build["commit"] = value
	}
	checks := make([]HealthCheck, 0, len(input.ConfigChecks))
	for _, check := range input.ConfigChecks {
		if knownHealthName(check.Name) && (check.Status == "ok" || check.Status == "failed" || check.Status == "degraded") {
			if check.Detail != "reachable" && check.Detail != "retried" {
				check.Detail = ""
			}
			checks = append(checks, check)
		}
	}
	health := input.Health
	health.Checks = nil
	for _, check := range input.Health.Checks {
		if knownHealthName(check.Name) && (check.Status == "ok" || check.Status == "failed" || check.Status == "degraded") {
			if check.Detail != "reachable" && check.Detail != "retried" {
				check.Detail = ""
			}
			health.Checks = append(health.Checks, check)
		}
	}
	jobs := make([]JobRecord, 0, len(input.Jobs))
	for _, job := range input.Jobs {
		if len(job.RunID) == 32 && hexadecimal(string(job.RunID)) && decimalString(string(job.GuildID)) && len(job.PlanHashPrefix) <= 12 && hexadecimal(job.PlanHashPrefix) {
			job.PlanHash = ""
			if !knownJobStatus(job.Status) {
				job.Status = JobFailed
			}
			if job.FailureClass != "step_failed" && job.FailureClass != "unknown" {
				job.FailureClass = ""
			}
			jobs = append(jobs, job)
		}
	}
	failures := map[string]int{}
	for key, value := range input.FailureClasses {
		if (key == "parse" || key == "policy" || key == "discord" || key == "reconciliation" || key == "timeout" || key == "internal" || key == "interrupted") && value >= 0 {
			failures[key] = value
		}
	}
	return DiagnosticInput{Build: build, ConfigChecks: checks, Health: health, Jobs: jobs, FailureClasses: failures}
}
func hexadecimal(value string) bool {
	if value == "" {
		return false
	}
	for _, r := range value {
		if !(r >= '0' && r <= '9' || r >= 'a' && r <= 'f' || r >= 'A' && r <= 'F') {
			return false
		}
	}
	return true
}
func decimalString(value string) bool {
	if value == "" {
		return false
	}
	for _, r := range value {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}
func knownHealthName(value string) bool {
	return value == "build" || value == "configuration" || value == "audit" || value == "state" || value == "discord"
}
func knownJobStatus(value JobStatus) bool {
	switch value {
	case JobPending, JobRunning, JobCompleted, JobFailed, JobPartial, JobCancelRequested:
		return true
	}
	return false
}
