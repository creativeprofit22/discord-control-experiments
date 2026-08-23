package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/ken-kai/discord-control-experiments/internal/application"
	"github.com/ken-kai/discord-control-experiments/internal/domain"
	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

type approvalClock struct{ now *time.Time }

func (c approvalClock) Now() time.Time { return *c.now }

type approvalIssuer struct{ issued int }

func (i *approvalIssuer) Issue(_ context.Context, grant domain.ApprovalGrant) (domain.ApprovalGrant, error) {
	if grant.ID != "" {
		return grant, errors.New("caller supplied approval ID")
	}
	i.issued++
	grant.ID = domain.ApprovalID(fmt.Sprintf("opaque-grant-%d", i.issued))
	return grant, nil
}

func destructiveServer(t *testing.T, now *time.Time) (*Server, *fakeOperator, *approvalIssuer) {
	t.Helper()
	issuer := &approvalIssuer{}
	coordinator, err := application.NewApprovalCoordinator(issuer, approvalClock{now}, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	operator := &fakeOperator{plan: domain.OperationPlan{GuildID: "123456789012345678", Operation: "delete_message", Capability: domain.CapabilityMessageManage, Mode: domain.ModeApply, DestructiveClass: domain.DestructiveDelete, OperationHash: "operation-hash"}, result: application.OperatorResult{Value: domain.EmptyResult{}}}
	options := Options{Enabled: true, ImplementationVersion: "1.2.3-test", GuildID: "123456789012345678", ActorID: "mcp-operator", Mode: domain.ModeApply, Toolsets: []application.Toolset{application.ToolsetModeration}, ReadOnly: false, MaxMessageBytes: 1 << 20, MaxConcurrentCalls: 2, CallTimeout: time.Second, Logger: slog.New(slog.NewTextHandler(io.Discard, nil)), Input: strings.NewReader(""), Output: io.Discard, Policy: allowPolicy{}, Operator: operator, Approval: coordinator, Clock: func() time.Time { return *now }, Correlation: func() domain.CorrelationID { return "corr-approval" }, FormElicitationSupported: func(*sdk.CallToolRequest) bool { return true }}
	server, err := New(options)
	if err != nil {
		t.Fatal(err)
	}
	return server, operator, issuer
}

func findTool(t *testing.T, server *Server, name string) *RegisteredTool {
	t.Helper()
	for index := range server.tools {
		if server.tools[index].Contract.Name == name {
			return &server.tools[index]
		}
	}
	t.Fatalf("tool %s missing", name)
	return nil
}

func TestDestructiveFormApprovalIsBoundAndOneTime(t *testing.T) {
	now := time.Unix(1000, 0)
	server, operator, issuer := destructiveServer(t, &now)
	tool := findTool(t, server, "delete_message")
	arguments := json.RawMessage(`{"channel_id":"2","message_id":"3"}`)
	first, _ := server.handler(tool)(context.Background(), &sdk.CallToolRequest{Params: &sdk.CallToolParamsRaw{Name: tool.Contract.Name, Arguments: arguments}})
	if len(first.InputRequests) != 1 || first.RequestState == "" {
		t.Fatalf("first %#v", first)
	}
	second, _ := server.handler(tool)(context.Background(), &sdk.CallToolRequest{Params: &sdk.CallToolParamsRaw{Name: tool.Contract.Name, Arguments: arguments, RequestState: first.RequestState, InputResponses: sdk.InputResponseMap{"confirm": &sdk.ElicitResult{Action: "accept", Content: map[string]any{"confirm": true}}}}})
	if second.IsError || issuer.issued != 1 || operator.request.Invocation.ApprovalID == "" {
		t.Fatalf("second=%#v issued=%d request=%#v", second, issuer.issued, operator.request)
	}
	replay, _ := server.handler(tool)(context.Background(), &sdk.CallToolRequest{Params: &sdk.CallToolParamsRaw{Name: tool.Contract.Name, Arguments: arguments, RequestState: first.RequestState, InputResponses: sdk.InputResponseMap{"confirm": &sdk.ElicitResult{Action: "accept", Content: map[string]any{"confirm": true}}}}})
	if !replay.IsError || issuer.issued != 1 {
		t.Fatalf("replay=%#v issued=%d", replay, issuer.issued)
	}
}

func TestDestructiveApprovalRejectsChangedArgumentsAndExpiry(t *testing.T) {
	now := time.Unix(2000, 0)
	server, _, issuer := destructiveServer(t, &now)
	tool := findTool(t, server, "delete_message")
	first, _ := server.handler(tool)(context.Background(), &sdk.CallToolRequest{Params: &sdk.CallToolParamsRaw{Name: tool.Contract.Name, Arguments: json.RawMessage(`{"channel_id":"2","message_id":"3"}`)}})
	now = now.Add(pendingApprovalTTL + time.Second)
	expired, _ := server.handler(tool)(context.Background(), &sdk.CallToolRequest{Params: &sdk.CallToolParamsRaw{Name: tool.Contract.Name, Arguments: json.RawMessage(`{"channel_id":"2","message_id":"4"}`), RequestState: first.RequestState, InputResponses: sdk.InputResponseMap{"confirm": &sdk.ElicitResult{Action: "accept", Content: map[string]any{"confirm": true}}}}})
	if !expired.IsError || issuer.issued != 0 {
		t.Fatalf("expired=%#v issued=%d", expired, issuer.issued)
	}
}

type dynamicApprovalOperator struct {
	prepareCalls int
	executeCalls int
	lastRequest  application.OperatorRequest
}

func (o *dynamicApprovalOperator) PrepareDestructive(_ context.Context, request application.OperatorRequest) (domain.OperationPlan, error) {
	o.prepareCalls++
	o.lastRequest = request
	return domain.OperationPlan{GuildID: request.Command.GuildScope(), Operation: domain.OperationName(request.Handler), Mode: domain.ModeApply, DestructiveClass: domain.DestructiveRoleEscalation, OperationHash: "dynamic-operation-hash"}, nil
}

func (o *dynamicApprovalOperator) Execute(_ context.Context, request application.OperatorRequest) (application.OperatorResult, error) {
	o.executeCalls++
	o.lastRequest = request
	switch request.Handler {
	case "create_role", "modify_role":
		return application.OperatorResult{Value: domain.Role{ID: "4", Name: "role", Permissions: "8"}}, nil
	case "modify_member":
		return application.OperatorResult{Value: domain.Member{User: domain.User{ID: "2", Username: "member"}, Roles: []domain.RoleID{}, JoinedAt: time.Unix(1000, 0)}}, nil
	default:
		return application.OperatorResult{}, errors.New("unexpected dynamic approval command")
	}
}

func dynamicDestructiveServer(t *testing.T, now *time.Time, operator *dynamicApprovalOperator) *Server {
	t.Helper()
	coordinator, err := application.NewApprovalCoordinator(&approvalIssuer{}, approvalClock{now}, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	server, err := New(Options{Enabled: true, ImplementationVersion: "1.2.3-test", GuildID: "123456789012345678", ActorID: "mcp-operator", Mode: domain.ModeApply, Toolsets: []application.Toolset{application.ToolsetBuild, application.ToolsetModeration}, MaxMessageBytes: 1 << 20, MaxConcurrentCalls: 2, CallTimeout: time.Second, Logger: slog.New(slog.NewTextHandler(io.Discard, nil)), Input: strings.NewReader(""), Output: io.Discard, Policy: allowPolicy{}, Operator: operator, Approval: coordinator, Clock: func() time.Time { return *now }, Correlation: func() domain.CorrelationID { return "corr-dynamic" }, FormElicitationSupported: func(*sdk.CallToolRequest) bool { return true }})
	if err != nil {
		t.Fatal(err)
	}
	return server
}

func TestDynamicDestructiveFormsUseOneTimeElicitation(t *testing.T) {
	tests := []struct {
		name               string
		tool               string
		destructiveArgs    json.RawMessage
		nonDestructiveArgs json.RawMessage
	}{
		{name: "create role permissions", tool: "create_role", destructiveArgs: json.RawMessage(`{"name":"privileged","permissions":"8"}`), nonDestructiveArgs: json.RawMessage(`{"name":"plain"}`)},
		{name: "modify role permissions", tool: "modify_role", destructiveArgs: json.RawMessage(`{"role_id":"2","permissions":"8"}`), nonDestructiveArgs: json.RawMessage(`{"role_id":"2","name":"plain"}`)},
		{name: "modify member roles", tool: "modify_member", destructiveArgs: json.RawMessage(`{"user_id":"2","roles":["3"]}`), nonDestructiveArgs: json.RawMessage(`{"user_id":"2","nick":"plain"}`)},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			now := time.Unix(3000, 0)
			operator := &dynamicApprovalOperator{}
			server := dynamicDestructiveServer(t, &now, operator)
			tool := findTool(t, server, test.tool)

			first, _ := server.handler(tool)(context.Background(), &sdk.CallToolRequest{Params: &sdk.CallToolParamsRaw{Name: test.tool, Arguments: test.destructiveArgs}})
			if len(first.InputRequests) != 1 || first.RequestState == "" || operator.prepareCalls != 1 || operator.executeCalls != 0 {
				t.Fatalf("first=%#v prepare=%d execute=%d", first, operator.prepareCalls, operator.executeCalls)
			}
			accepted, _ := server.handler(tool)(context.Background(), &sdk.CallToolRequest{Params: &sdk.CallToolParamsRaw{Name: test.tool, Arguments: test.destructiveArgs, RequestState: first.RequestState, InputResponses: sdk.InputResponseMap{"confirm": &sdk.ElicitResult{Action: "accept", Content: map[string]any{"confirm": true}}}}})
			if accepted.IsError || operator.prepareCalls != 1 || operator.executeCalls != 1 || operator.lastRequest.Invocation.ApprovalID == "" {
				t.Fatalf("accepted=%#v prepare=%d execute=%d request=%#v", accepted, operator.prepareCalls, operator.executeCalls, operator.lastRequest)
			}
			replayed, _ := server.handler(tool)(context.Background(), &sdk.CallToolRequest{Params: &sdk.CallToolParamsRaw{Name: test.tool, Arguments: test.destructiveArgs, RequestState: first.RequestState, InputResponses: sdk.InputResponseMap{"confirm": &sdk.ElicitResult{Action: "accept", Content: map[string]any{"confirm": true}}}}})
			if !replayed.IsError || operator.prepareCalls != 1 || operator.executeCalls != 1 {
				t.Fatalf("replayed=%#v prepare=%d execute=%d", replayed, operator.prepareCalls, operator.executeCalls)
			}

			plainOperator := &dynamicApprovalOperator{}
			plainServer := dynamicDestructiveServer(t, &now, plainOperator)
			plainTool := findTool(t, plainServer, test.tool)
			plain, _ := plainServer.handler(plainTool)(context.Background(), &sdk.CallToolRequest{Params: &sdk.CallToolParamsRaw{Name: test.tool, Arguments: test.nonDestructiveArgs}})
			if len(plain.InputRequests) != 0 || plain.RequestState != "" || plainOperator.prepareCalls != 0 || plainOperator.executeCalls != 1 {
				t.Fatalf("plain=%#v prepare=%d execute=%d", plain, plainOperator.prepareCalls, plainOperator.executeCalls)
			}
		})
	}
}
