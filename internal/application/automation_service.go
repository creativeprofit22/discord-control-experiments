package application

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"slices"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/ken-kai/discord-control-experiments/internal/domain"
)

type AutomationService struct {
	Store      AutomationStore
	Journals   AutomationJournalStore
	Actions    AutomationActionExecutor
	Summarizer AutomationSummarizer
	Snapshot   AutomationSnapshot
	Telemetry  WorkflowTelemetry
	Clock      AutomationClock
	Catalog    AutomationCatalog
	Principal  domain.Principal
	ActorID    domain.ActorID
	Approvals  ApprovalStore
}

type AutomationRunRequest struct {
	Workflow      domain.AutomationWorkflow
	TriggerID     string
	Item          *SourceItem
	MemberID      string
	MemberRoleIDs []string
	ApprovalID    domain.ApprovalID
	ReviewedHash  string
	RetryFailed   bool
	Recovery      bool
	LeaseOwner    string
	LeaseToken    string
	LeaseDuration time.Duration
}

func (s *AutomationService) Validate(workflow domain.AutomationWorkflow) error {
	if s == nil || s.Store == nil || s.Journals == nil || s.Actions == nil || s.Clock == nil {
		return errors.New("automation service dependencies are required")
	}
	if s.Principal.Adapter == "" || s.Principal.Subject == "" || s.Principal.AuthenticationMethod == "" {
		return errors.New("automation principal is required")
	}
	return ValidateAutomationWorkflow(workflow, s.Catalog)
}

func automationExecutionLease(request AutomationRunRequest) (string, string, time.Duration, error) {
	owner, token, duration := request.LeaseOwner, request.LeaseToken, request.LeaseDuration
	if owner == "" && token == "" {
		identity := make([]byte, 16)
		if _, err := rand.Read(identity); err != nil {
			return "", "", 0, errors.New("generate automation execution lease")
		}
		owner, token = "interpreter", hex.EncodeToString(identity)
		if duration <= 0 {
			duration = 5 * time.Minute
		}
	}
	if owner == "" || token == "" || duration <= 0 {
		return "", "", 0, errors.New("automation execution lease identity is incomplete")
	}
	return owner, token, duration, nil
}

func (s *AutomationService) Run(ctx context.Context, request AutomationRunRequest) (domain.AutomationRun, error) {
	if err := s.Validate(request.Workflow); err != nil {
		return domain.AutomationRun{}, err
	}
	if err := validateAutomationTriggerInput(request); err != nil {
		return domain.AutomationRun{}, err
	}
	leaseOwner, leaseToken, leaseDuration, err := automationExecutionLease(request)
	if err != nil {
		return domain.AutomationRun{}, err
	}
	workflowHash, err := request.Workflow.Hash()
	if err != nil {
		return domain.AutomationRun{}, err
	}
	if _, err := s.Store.SaveDefinition(ctx, request.Workflow); err != nil {
		return domain.AutomationRun{}, err
	}
	triggerID := request.TriggerID
	if request.Item != nil && !strings.Contains(triggerID, request.Item.ID) {
		triggerID += ":" + request.Item.ID
	}
	runID := automationID("run", workflowHash, triggerID)
	run := domain.AutomationRun{ID: runID, WorkflowHash: workflowHash, IdempotencyKey: automationID("idem", workflowHash, triggerID), TriggerMemberID: request.MemberID, TriggerMemberRoleIDs: append([]string(nil), request.MemberRoleIDs...), Provenance: domain.AutomationProvenance{WorkflowID: request.Workflow.ID, Definition: workflowHash, TriggerID: triggerID}}
	if request.Item != nil {
		run.Provenance.SourceID, run.Provenance.SourceURL, run.Provenance.SourceTime = request.Item.ID, request.Item.Link, request.Item.Published
		run.SourcePayload = domain.AutomationSourcePayload{ID: request.Item.ID, Kind: request.Item.Kind, Title: request.Item.Title, Text: request.Item.Text, Link: request.Item.Link, Published: request.Item.Published}
	}
	created, err := s.Store.EnqueueRun(ctx, run)
	if err != nil {
		return run, err
	}
	if !created {
		s.observe(ctx, run, "", "", "deduplicated", "existing")
	}
	run, err = s.Store.LoadRun(runID)
	if err != nil {
		return run, err
	}
	saveRun := func(next domain.AutomationRun) error {
		if run.State == domain.AutomationRunLeased || run.State == domain.AutomationRunRunning {
			return s.Store.CompareAndSwapOwnedRun(ctx, run.Revision, next, leaseOwner, leaseToken, s.Clock.Now())
		}
		return s.Store.CompareAndSwapRun(ctx, run.Revision, next)
	}
	item := request.Item
	if item == nil && run.SourcePayload.ID != "" {
		payload := run.SourcePayload
		item = &SourceItem{ID: payload.ID, Kind: payload.Kind, Title: payload.Title, Text: payload.Text, Link: payload.Link, Published: payload.Published}
	}
	memberID := request.MemberID
	if memberID == "" {
		memberID = run.TriggerMemberID
	}
	memberRoleIDs := request.MemberRoleIDs
	if len(memberRoleIDs) == 0 {
		memberRoleIDs = run.TriggerMemberRoleIDs
	}
	if run.State == domain.AutomationRunSucceeded || run.State == domain.AutomationRunUncertain || run.State == domain.AutomationRunDeadLetter {
		return run, nil
	}
	if run.State == domain.AutomationRunRetryWaiting {
		if run.NextAttempt.After(s.Clock.Now()) {
			return run, nil
		}
		next := run
		next.State = domain.AutomationRunQueued
		if err := saveRun(next); err != nil {
			return run, err
		}
		run, _ = s.Store.LoadRun(runID)
	}
	if run.State == domain.AutomationRunFailed {
		if !request.RetryFailed {
			return run, nil
		}
		next := run
		next.State, next.Error = domain.AutomationRunQueued, ""
		if err := saveRun(next); err != nil {
			return run, err
		}
		run, _ = s.Store.LoadRun(runID)
	}
	var reviewGrant domain.ApprovalGrant
	if run.State == domain.AutomationRunWaitingApproval {
		if request.ReviewedHash == "" || request.ReviewedHash != run.RenderedOperationHash || request.ApprovalID == "" {
			return run, nil
		}
		guild := ""
		for _, child := range run.Children {
			if child.State != domain.AutomationRunSucceeded {
				guild = child.GuildID
				break
			}
		}
		if guild == "" {
			return run, errors.New("approval target guild is missing")
		}
		if s.Approvals != nil {
			reviewGrant, err = s.Approvals.ConsumeOperation(ctx, request.ApprovalID, s.ActorID, domain.GuildID(guild), run.RenderedOperationHash, s.Clock.Now())
			if err != nil {
				return run, &domain.ApprovalError{Reason: "workflow review approval is invalid"}
			}
		} else {
			if _, production := s.Actions.(CoreAutomationExecutor); production {
				return run, errors.New("workflow review approval store is unavailable")
			}
		}
		next := run
		next.State = domain.AutomationRunQueued
		if err := saveRun(next); err != nil {
			return run, err
		}
		run, _ = s.Store.LoadRun(runID)
	}
	if run.State == domain.AutomationRunQueued {
		claimed, claimErr := s.Store.ClaimRun(ctx, run.ID, leaseOwner, leaseToken, s.Clock.Now(), leaseDuration)
		if claimErr != nil {
			s.observe(ctx, run, "", "", "lease_contention", "contended")
			latest, loadErr := s.Store.LoadRun(run.ID)
			if loadErr == nil {
				return latest, nil
			}
			return run, claimErr
		}
		run = claimed
	}
	if (run.State == domain.AutomationRunLeased || run.State == domain.AutomationRunRunning) && (run.Lease.Owner != leaseOwner || run.Lease.Token != leaseToken) {
		if run.Lease.Expires.After(s.Clock.Now()) {
			return run, nil
		}
		claimed, claimErr := s.Store.ClaimRun(ctx, run.ID, leaseOwner, leaseToken, s.Clock.Now(), leaseDuration)
		if claimErr != nil {
			return run, claimErr
		}
		run = claimed
	}
	if run.State == domain.AutomationRunLeased {
		next := run
		next.State = domain.AutomationRunRunning
		if err := saveRun(next); err != nil {
			return run, err
		}
		run, _ = s.Store.LoadRun(runID)
	}
	if run.State != domain.AutomationRunRunning || run.Lease.Owner != leaseOwner || run.Lease.Token != leaseToken || !run.Lease.Expires.After(s.Clock.Now()) {
		return run, errors.New("automation run is not executable")
	}
	journal, events, err := s.Journals.OpenAutomationJournal(run.ID, run.WorkflowHash)
	if err != nil {
		return run, err
	}
	defer journal.Close()
	if len(events) > 0 && events[len(events)-1].State == "dispatching" {
		run.State = domain.AutomationRunUncertain
		run.Error = "previous mutation outcome is uncertain"
		if err := saveRun(run); err != nil {
			return run, err
		}
		return s.Store.LoadRun(run.ID)
	}

	failures := 0
	recovering := request.Recovery
	for _, guild := range request.Workflow.Targets.GuildIDs {
		childIndex := automationChildIndex(run.Children, guild)
		if childIndex < 0 {
			run.Children = append(run.Children, domain.AutomationChildResult{GuildID: guild, State: domain.AutomationRunRunning})
			if err := saveRun(run); err != nil {
				return run, err
			}
			run, _ = s.Store.LoadRun(run.ID)
			childIndex = automationChildIndex(run.Children, guild)
		}
		child := &run.Children[childIndex]
		if child.State == domain.AutomationRunSucceeded {
			continue
		}
		for actionIndex := child.CompletedStep; actionIndex < len(request.Workflow.Steps); actionIndex++ {
			step := request.Workflow.Steps[actionIndex]
			resumeRendered := recovering || (request.ReviewedHash != "" && request.ReviewedHash == run.RenderedOperationHash)
			memberRoles := memberRoleIDs
			if len(memberRoles) == 0 && resumeRendered {
				memberRoles = run.PendingRoleIDs
			}
			if !resumeRendered && !automationConditionsMatch(step.Conditions, item, memberRoles) {
				child.CompletedStep++
				if err := saveRun(run); err != nil {
					return run, err
				}
				run, _ = s.Store.LoadRun(run.ID)
				childIndex = automationChildIndex(run.Children, guild)
				child = &run.Children[childIndex]
				continue
			}
			if err := s.Store.Heartbeat(ctx, run.ID, leaseOwner, leaseToken, s.Clock.Now(), leaseDuration); err != nil {
				return run, err
			}
			run, _ = s.Store.LoadRun(run.ID)
			childIndex = automationChildIndex(run.Children, guild)
			child = &run.Children[childIndex]
			content, transformErr := s.transform(ctx, step, item)
			if resumeRendered {
				content, transformErr = run.RenderedContent, nil
			}
			if transformErr != nil {
				failures++
				child.State, child.Error = domain.AutomationRunFailed, boundedAutomationError(transformErr)
				break
			}
			stepID := automationID("step", workflowHash, triggerID, guild, fmt.Sprint(actionIndex))
			action := step.Action
			if action.UserID == "" {
				action.UserID = memberID
				if action.UserID == "" {
					action.UserID = run.PendingUserID
				}
			}
			if action.Kind == domain.ActionCleanup && !resumeRendered {
				previewer, ok := s.Actions.(AutomationActionPreviewer)
				if !ok {
					failures++
					child.State, child.Error = domain.AutomationRunFailed, "cleanup preview is unavailable"
					break
				}
				previewInvocation := Invocation{Principal: s.Principal, ActorID: s.ActorID, RequestedMode: domain.ModeApply, CorrelationID: domain.CorrelationID(automationID("preview", run.ID, guild, stepID))}
				content, transformErr = previewer.PreviewAutomationAction(ctx, previewInvocation, domain.GuildID(guild), action)
				if transformErr != nil {
					var retryableRead *automationRetryableReadError
					if errors.As(transformErr, &retryableRead) {
						run.Attempt++
						if run.Attempt < request.Workflow.Retry.MaxAttempts {
							run.State, child.State = domain.AutomationRunRetryWaiting, domain.AutomationRunRetryWaiting
							run.NextAttempt = s.Clock.Now().Add(automationBackoff(request.Workflow.Retry, run.Attempt))
						} else {
							run.State, child.State = domain.AutomationRunDeadLetter, domain.AutomationRunDeadLetter
						}
						child.Error = boundedAutomationError(transformErr)
						if err := saveRun(run); err != nil {
							return run, err
						}
						run, _ = s.Store.LoadRun(run.ID)
						if run.State == domain.AutomationRunDeadLetter {
							_ = s.Store.DeadLetter(ctx, run)
							s.observe(ctx, run, guild, stepID, "dead_letter", run.State)
						} else {
							s.observe(ctx, run, guild, stepID, "retry", run.State)
						}
						return run, nil
					}
					failures++
					child.State, child.Error = domain.AutomationRunFailed, boundedAutomationError(transformErr)
					break
				}
			}
			operationHash := automationOperationHash(guild, stepID, action, content)
			if step.Action.RequireReview && request.ReviewedHash != operationHash {
				run.State, run.RenderedOperationHash, run.RenderedContent, run.PendingUserID, run.PendingRoleIDs = domain.AutomationRunWaitingApproval, operationHash, content, action.UserID, append([]string(nil), memberRoles...)
				if err := saveRun(run); err != nil {
					return run, err
				}
				s.observe(ctx, run, guild, stepID, "approval_wait", "waiting")
				return s.Store.LoadRun(run.ID)
			}
			if run.RenderedOperationHash != operationHash || run.RenderedContent != content || run.PendingUserID != action.UserID || !slices.Equal(run.PendingRoleIDs, memberRoles) {
				run.RenderedOperationHash, run.RenderedContent, run.PendingUserID, run.PendingRoleIDs = operationHash, content, action.UserID, append([]string(nil), memberRoles...)
				if err := saveRun(run); err != nil {
					return run, err
				}
				run, _ = s.Store.LoadRun(run.ID)
			}
			if _, err := journal.AppendDispatching(stepID, s.Clock.Now()); err != nil {
				return run, err
			}
			invocation := Invocation{Principal: s.Principal, ActorID: s.ActorID, RequestedMode: domain.ModeApply, CorrelationID: domain.CorrelationID(automationID("corr", run.ID, guild, stepID))}
			if reviewGrant.ID != "" {
				invocation.automationReview = &automationReviewAuthorization{grant: reviewGrant, stepID: stepID}
			}
			result, actionErr := s.executeAction(ctx, invocation, domain.GuildID(guild), action, content, stepID)
			if err := s.Store.Heartbeat(ctx, run.ID, leaseOwner, leaseToken, s.Clock.Now(), leaseDuration); err != nil {
				return run, err
			}
			run, err = s.Store.LoadRun(run.ID)
			if err != nil {
				return run, err
			}
			childIndex = automationChildIndex(run.Children, guild)
			child = &run.Children[childIndex]
			if actionErr != nil {
				if !result.Uncertain {
					if _, journalErr := journal.Append("failed", stepID, s.Clock.Now()); journalErr != nil {
						return run, journalErr
					}
				}
				if result.ReviewChanged {
					if result.Rendered == "" || len(result.Rendered) > 4000 {
						return run, errors.New("invalid changed review output")
					}
					run.State, run.RenderedContent = domain.AutomationRunWaitingApproval, result.Rendered
					run.RenderedOperationHash = automationOperationHash(guild, stepID, action, result.Rendered)
					if err := saveRun(run); err != nil {
						return run, err
					}
					s.observe(ctx, run, guild, stepID, "approval_wait", "review_changed")
					return s.Store.LoadRun(run.ID)
				}
				if action.RequireReview && !result.Uncertain {
					run.State, child.Error = domain.AutomationRunWaitingApproval, boundedAutomationError(actionErr)
					if err := saveRun(run); err != nil {
						return run, err
					}
					s.observe(ctx, run, guild, stepID, "approval_wait", "retry_requires_review")
					return s.Store.LoadRun(run.ID)
				}
				child.Error = boundedAutomationError(actionErr)
				failures++
				switch {
				case result.Uncertain:
					run.State, child.State = domain.AutomationRunUncertain, domain.AutomationRunUncertain
				case result.ProvenUnsent && result.Retryable:
					run.Attempt++
					if run.Attempt < request.Workflow.Retry.MaxAttempts {
						run.State, child.State = domain.AutomationRunRetryWaiting, domain.AutomationRunRetryWaiting
						run.NextAttempt = s.Clock.Now().Add(automationBackoff(request.Workflow.Retry, run.Attempt))
					} else {
						run.State, child.State = domain.AutomationRunDeadLetter, domain.AutomationRunDeadLetter
					}
				default:
					child.State = domain.AutomationRunFailed
				}
				if run.State == domain.AutomationRunUncertain || run.State == domain.AutomationRunRetryWaiting || run.State == domain.AutomationRunDeadLetter {
					event := "partial_failure"
					if run.State == domain.AutomationRunRetryWaiting {
						event = "retry"
					} else if run.State == domain.AutomationRunDeadLetter {
						event = "dead_letter"
					}
					s.observe(ctx, run, guild, stepID, event, run.State)
					if err := saveRun(run); err != nil {
						return run, err
					}
					run, _ = s.Store.LoadRun(run.ID)
					if run.State == domain.AutomationRunDeadLetter {
						_ = s.Store.DeadLetter(ctx, run)
					}
					return run, nil
				}
				break
			}
			if _, err := journal.Append("completed", stepID, s.Clock.Now()); err != nil {
				return run, err
			}
			child.CompletedStep++
			run.CompletedSteps = append(run.CompletedSteps, stepID)
			s.observe(ctx, run, guild, stepID, "published", "completed")
			run.RenderedOperationHash, run.RenderedContent, run.PendingUserID, run.PendingRoleIDs = "", "", "", nil
			recovering = false
			reviewGrant = domain.ApprovalGrant{}
			if err := saveRun(run); err != nil {
				return run, err
			}
			run, _ = s.Store.LoadRun(run.ID)
			childIndex = automationChildIndex(run.Children, guild)
			child = &run.Children[childIndex]
		}
		if child.CompletedStep == len(request.Workflow.Steps) {
			child.State = domain.AutomationRunSucceeded
		}
		if err := saveRun(run); err != nil {
			return run, err
		}
		run, _ = s.Store.LoadRun(run.ID)
	}
	if failures > 0 {
		run.State = domain.AutomationRunFailed
	} else {
		run.State = domain.AutomationRunSucceeded
	}
	if err := saveRun(run); err != nil {
		return run, err
	}
	return s.Store.LoadRun(run.ID)
}

func (s *AutomationService) transform(ctx context.Context, step domain.AutomationStep, item *SourceItem) (string, error) {
	content := step.Action.Content
	if item != nil && content == "" {
		content = item.Title + "\n" + item.Text + "\n" + item.Link
	}
	for _, transform := range step.Transforms {
		switch transform.Kind {
		case domain.TransformRender:
			content = transform.Template
			if item != nil {
				content = strings.NewReplacer("{title}", item.Title, "{text}", item.Text, "{link}", item.Link).Replace(content)
			}
		case domain.TransformSummarize:
			if s.Summarizer == nil || item == nil {
				return "", errors.New("summarizer and source item are required")
			}
			result, err := s.Summarizer.Summarize(ctx, SummaryRequest{Title: item.Title, Text: item.Text, Link: item.Link, Published: item.Published, MaxBytes: transform.MaxBytes})
			if err != nil {
				return "", err
			}
			content = result.Text
		}
	}
	content = plainAutomationText(content, 4000)
	if content == "" {
		switch step.Action.Kind {
		case domain.ActionMessage, domain.ActionWebhook, domain.ActionForumThread, domain.ActionMediaThread, domain.ActionPoll, domain.ActionWelcome:
			return "", errors.New("automation action rendered empty content")
		}
	}
	return content, nil
}

func (s *AutomationService) executeAction(ctx context.Context, invocation Invocation, guild domain.GuildID, action domain.AutomationAction, content, stepID string) (AutomationActionResult, error) {
	if action.Kind == domain.ActionSnapshot && s.Snapshot != nil {
		err := s.Snapshot.Snapshot(ctx, guild)
		return AutomationActionResult{ProvenUnsent: true, Retryable: err != nil}, err
	}
	return s.Actions.ExecuteAutomationAction(ctx, invocation, guild, action, content, stepID)
}

func automationConditionsMatch(conditions []domain.AutomationCondition, item *SourceItem, memberRoles []string) bool {
	roles := map[string]bool{}
	for _, role := range memberRoles {
		roles[role] = true
	}
	for _, condition := range conditions {
		switch condition.Kind {
		case domain.ConditionSourceKind:
			if item == nil || item.Kind != condition.Value {
				return false
			}
		case domain.ConditionTextHas:
			if item == nil || !strings.Contains(strings.ToLower(item.Title+" "+item.Text), strings.ToLower(condition.Value)) {
				return false
			}
		case domain.ConditionMemberRole:
			if !roles[condition.RoleID] {
				return false
			}
		}
	}
	return true
}
func (s *AutomationService) observe(ctx context.Context, run domain.AutomationRun, guild, step, event, outcome string) {
	if s.Telemetry == nil {
		return
	}
	s.Telemetry.ObserveWorkflow(ctx, WorkflowObservation{WorkflowID: run.Provenance.WorkflowID, RunID: run.ID, GuildID: guild, StepID: step, Event: event, Outcome: outcome, Attempt: run.Attempt, Provenance: run.Provenance})
}

func validateAutomationTriggerInput(request AutomationRunRequest) error {
	if request.TriggerID == "" || len(request.TriggerID) > 512 || strings.ContainsAny(request.TriggerID, "\r\n\x00") {
		return errors.New("invalid automation trigger identity")
	}
	if request.Item != nil {
		item := request.Item
		link, err := url.Parse(item.Link)
		if item.ID == "" || len(item.ID) > 256 || len(item.Kind) > 64 || len(item.Title) > 300 || len(item.Text) > 12000 || len(item.Link) > 2048 || err != nil || link.Scheme != "https" || link.Host == "" || link.User != nil {
			return errors.New("invalid bounded source item")
		}
	}
	validID := func(value string) bool {
		if value == "" || len(value) > 20 || value[0] < '1' || value[0] > '9' {
			return false
		}
		for _, r := range value {
			if r < '0' || r > '9' {
				return false
			}
		}
		return true
	}
	if request.MemberID != "" && !validID(request.MemberID) {
		return errors.New("invalid member trigger identity")
	}
	if len(request.MemberRoleIDs) > 250 {
		return errors.New("member trigger roles exceed limit")
	}
	for _, role := range request.MemberRoleIDs {
		if !validID(role) {
			return errors.New("invalid member trigger role")
		}
	}
	return nil
}

func automationChildIndex(children []domain.AutomationChildResult, guild string) int {
	for i := range children {
		if children[i].GuildID == guild {
			return i
		}
	}
	return -1
}
func automationID(parts ...string) string {
	digest := sha256.Sum256([]byte(strings.Join(parts, "\x00")))
	return parts[0] + "-" + hex.EncodeToString(digest[:12])
}
func automationOperationHash(guild, step string, action domain.AutomationAction, content string) string {
	encoded, _ := json.Marshal(struct {
		Guild, Step string
		Action      domain.AutomationAction
		Content     string
	}{guild, step, action, content})
	digest := sha256.Sum256(encoded)
	return hex.EncodeToString(digest[:])
}
func automationBackoff(retry domain.AutomationRetry, attempt int) time.Duration {
	initial, _ := time.ParseDuration(retry.Initial)
	maximum, _ := time.ParseDuration(retry.Maximum)
	delay := initial
	for i := 1; i < attempt && delay < maximum/2; i++ {
		delay *= 2
	}
	if delay > maximum {
		return maximum
	}
	return delay
}
func boundedAutomationError(err error) string {
	if err == nil {
		return ""
	}
	value := err.Error()
	if len(value) > 1024 {
		value = value[:1024]
	}
	return value
}
func plainAutomationText(value string, limit int) string {
	value = strings.Map(func(r rune) rune {
		if unicode.IsControl(r) && r != '\n' && r != '\t' {
			return -1
		}
		return r
	}, value)
	value = strings.TrimSpace(value)
	for len(value) > limit {
		_, size := utf8.DecodeLastRuneInString(value)
		value = value[:len(value)-size]
	}
	return value
}
