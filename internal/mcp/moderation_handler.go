package mcp

import (
	"context"
	"encoding/json"
	"reflect"

	"github.com/ken-kai/discord-control-experiments/internal/application"
	"github.com/ken-kai/discord-control-experiments/internal/domain"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func (s *Server) handleLocalModerationTool(ctx context.Context, request *mcp.CallToolRequest, tool *RegisteredTool, correlation domain.CorrelationID) (*mcp.CallToolResult, error) {
	if request == nil || request.Params == nil {
		return s.errorResult(tool, "invalid_arguments", "arguments are required", correlation), nil
	}
	value := reflect.New(tool.Contract.InputType)
	if err := tool.Input.DecodeCommand(request.Params.Arguments, value.Interface()); err != nil {
		return s.errorResult(tool, "invalid_arguments", "arguments do not match the closed tool schema", correlation), nil
	}
	payload, err := json.Marshal(value.Elem().Interface())
	if err != nil {
		return s.errorResult(tool, "internal_error", "arguments could not be normalized", correlation), nil
	}
	var object map[string]json.RawMessage
	if err := json.Unmarshal(payload, &object); err != nil {
		return s.errorResult(tool, "internal_error", "arguments could not be normalized", correlation), nil
	}
	invocation := application.Invocation{Principal: fixedPrincipal, RequestedMode: s.options.Mode, CorrelationID: correlation, ActorID: s.options.ActorID}
	if raw := object["approval_id"]; len(raw) > 0 {
		_ = json.Unmarshal(raw, &invocation.ApprovalID)
		delete(object, "approval_id")
	}
	values := map[string]string{"--guild": string(s.options.GuildID)}
	if raw := object["plan_hash"]; len(raw) > 0 {
		var hash string
		_ = json.Unmarshal(raw, &hash)
		values["--plan-hash"] = hash
		delete(object, "plan_hash")
	}
	if invocation.ApprovalID != "" {
		values["--approval-id"] = string(invocation.ApprovalID)
	}
	payload, err = json.Marshal(object)
	if err != nil {
		return s.errorResult(tool, "internal_error", "arguments could not be normalized", correlation), nil
	}
	outcome, executeErr := s.options.Operator.Execute(ctx, application.OperatorRequest{Handler: tool.LocalHandler, Invocation: invocation, Values: values, Payload: payload})
	if executeErr != nil {
		code, detail, _ := stableError(executeErr)
		return s.errorResult(tool, code, detail, correlation), nil
	}
	if outcome.Plan != nil {
		return s.planResult(tool, *outcome.Plan, correlation), nil
	}
	return s.valueResult(tool, outcome.Value, correlation), nil
}
