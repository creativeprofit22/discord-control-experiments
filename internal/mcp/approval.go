package mcp

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"sync"
	"time"

	"github.com/ken-kai/discord-control-experiments/internal/application"
	"github.com/ken-kai/discord-control-experiments/internal/domain"
	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

const (
	pendingApprovalTTL  = 2 * time.Minute
	maxPendingApprovals = 256
)

type pendingApproval struct {
	Tool          string
	ArgumentsHash string
	OperationHash string
	ActorID       domain.ActorID
	GuildID       domain.GuildID
	ExpiresAt     time.Time
	Plan          domain.OperationPlan
}

type pendingApprovals struct {
	mu     sync.Mutex
	values map[string]pendingApproval
}

func newPendingApprovals() *pendingApprovals {
	return &pendingApprovals{values: map[string]pendingApproval{}}
}

func (p *pendingApprovals) issue(value pendingApproval) (string, error) {
	bytes := make([]byte, 24)
	if _, err := rand.Read(bytes); err != nil {
		return "", errors.New("approval state generation failed")
	}
	state := hex.EncodeToString(bytes)
	p.mu.Lock()
	defer p.mu.Unlock()
	for existingState, pending := range p.values {
		if !value.ExpiresAt.Add(-pendingApprovalTTL).Before(pending.ExpiresAt) {
			delete(p.values, existingState)
		}
	}
	if len(p.values) >= maxPendingApprovals {
		return "", errors.New("approval state capacity reached")
	}
	p.values[state] = value
	return state, nil
}

func (p *pendingApprovals) consume(state string, now time.Time) (pendingApproval, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	value, ok := p.values[state]
	delete(p.values, state)
	if !ok {
		return pendingApproval{}, errors.New("approval state is unknown or already consumed")
	}
	if !now.Before(value.ExpiresAt) {
		return pendingApproval{}, errors.New("approval state expired")
	}
	return value, nil
}

func canonicalArgumentsHash(raw json.RawMessage) (string, error) {
	var value any
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	if err := decoder.Decode(&value); err != nil {
		return "", err
	}
	canonical, err := json.Marshal(value)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(canonical)
	return hex.EncodeToString(sum[:]), nil
}

func supportsFormElicitation(request *sdk.CallToolRequest) bool {
	if request == nil || request.Session == nil {
		return false
	}
	params := request.Session.InitializeParams()
	if params == nil || params.Capabilities == nil || params.Capabilities.Elicitation == nil {
		return false
	}
	capability := params.Capabilities.Elicitation
	return capability.Form != nil || (capability.Form == nil && capability.URL == nil)
}

func (s *Server) handleDestructive(ctx context.Context, request *sdk.CallToolRequest, tool *RegisteredTool, operatorRequest application.OperatorRequest, correlation domain.CorrelationID) (*sdk.CallToolResult, string, domain.PolicyReason) {
	argumentsHash, err := canonicalArgumentsHash(request.Params.Arguments)
	if err != nil {
		return s.errorResult(tool, "invalid_arguments", "arguments cannot be bound to approval", correlation), "schema_error", ""
	}
	invocation := operatorRequest.Invocation
	if request.Params.RequestState != "" || len(request.Params.InputResponses) > 0 {
		pending, consumeErr := s.pending.consume(request.Params.RequestState, s.options.Clock())
		if consumeErr != nil {
			return s.errorResult(tool, "approval_invalid", "approval request is expired, changed, or already used", correlation), "approval_invalid", ""
		}
		if pending.Tool != tool.Contract.Name || pending.ArgumentsHash != argumentsHash || pending.ActorID != invocation.ActorID || pending.GuildID != s.options.GuildID || !acceptedConfirmation(request) {
			return s.errorResult(tool, "approval_invalid", "approval was declined or did not match the operation", correlation), "approval_rejected", ""
		}
		grant, issueErr := s.options.Approval.Issue(ctx, invocation, pending.Plan, application.ConfirmationInteractive)
		if issueErr != nil {
			return s.errorResult(tool, "approval_invalid", "approval grant could not be issued", correlation), "approval_invalid", ""
		}
		invocation.ApprovalID = grant.ID
		operatorRequest.Invocation = invocation
		outcome, executeErr := s.options.Operator.Execute(ctx, operatorRequest)
		if executeErr != nil {
			code, detail, reason := stableError(executeErr)
			return s.errorResult(tool, code, detail, correlation), code, reason
		}
		if outcome.Plan != nil {
			return s.planResult(tool, *outcome.Plan, correlation), "plan", ""
		}
		return s.valueResult(tool, outcome.Value, correlation), "ok", ""
	}
	plan, prepareErr := s.options.Operator.PrepareDestructive(ctx, operatorRequest)
	if prepareErr != nil {
		code, detail, reason := stableError(prepareErr)
		return s.errorResult(tool, code, detail, correlation), code, reason
	}
	if plan.Preapproved {
		grant, issueErr := s.options.Approval.Issue(ctx, invocation, plan, application.ConfirmationPolicyPreapproved)
		if issueErr != nil {
			return s.errorResult(tool, "approval_invalid", "policy preapproval could not be issued", correlation), "approval_invalid", ""
		}
		invocation.ApprovalID = grant.ID
		operatorRequest.Invocation = invocation
		outcome, executeErr := s.options.Operator.Execute(ctx, operatorRequest)
		if executeErr != nil {
			code, detail, reason := stableError(executeErr)
			return s.errorResult(tool, code, detail, correlation), code, reason
		}
		return s.valueResult(tool, outcome.Value, correlation), "ok", ""
	}
	if !s.options.FormElicitationSupported(request) {
		return s.approvalPlanResult(tool, plan, correlation), "approval_required", ""
	}
	state, stateErr := s.pending.issue(pendingApproval{Tool: tool.Contract.Name, ArgumentsHash: argumentsHash, OperationHash: plan.OperationHash, ActorID: invocation.ActorID, GuildID: s.options.GuildID, ExpiresAt: s.options.Clock().Add(pendingApprovalTTL), Plan: plan})
	if stateErr != nil {
		return s.errorResult(tool, "internal_error", "approval state unavailable", correlation), "internal_error", ""
	}
	message := sanitizeString("Confirm "+string(plan.Operation)+" ("+string(plan.DestructiveClass)+") for configured guild", 256)
	return &sdk.CallToolResult{InputRequests: sdk.InputRequestMap{"confirm": &sdk.ElicitParams{Mode: "form", Message: message, RequestedSchema: map[string]any{"type": "object", "additionalProperties": false, "required": []string{"confirm"}, "properties": map[string]any{"confirm": map[string]any{"type": "boolean", "const": true}}}}}, RequestState: state}, "approval_required", ""
}

func acceptedConfirmation(request *sdk.CallToolRequest) bool {
	if request == nil || request.Params == nil {
		return false
	}
	response, ok := request.Params.InputResponses["confirm"]
	if !ok {
		return false
	}
	elicited, ok := response.(*sdk.ElicitResult)
	if !ok || elicited == nil || elicited.Action != "accept" {
		return false
	}
	confirmed, ok := elicited.Content["confirm"].(bool)
	return ok && confirmed
}
