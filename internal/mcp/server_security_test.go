package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/ken-kai/discord-control-experiments/internal/application"
	"github.com/ken-kai/discord-control-experiments/internal/domain"
	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

type allowPolicy struct{}

func (allowPolicy) Decide(_ context.Context, request domain.PolicyRequest) (domain.PolicyDecision, error) {
	return domain.PolicyDecision{Allowed: true, ActorID: "mcp:local-stdio", EffectiveMode: request.Mode, Reason: domain.PolicyAllowed}, nil
}

type cancellingOperator struct{ started chan struct{} }

func (c *cancellingOperator) Execute(ctx context.Context, _ application.OperatorRequest) (application.OperatorResult, error) {
	close(c.started)
	<-ctx.Done()
	return application.OperatorResult{}, ctx.Err()
}
func (c *cancellingOperator) PrepareDestructive(context.Context, application.OperatorRequest) (domain.OperationPlan, error) {
	return domain.OperationPlan{}, errors.New("unexpected")
}

type fakeOperator struct {
	calls   int
	request application.OperatorRequest
	result  application.OperatorResult
	err     error
	plan    domain.OperationPlan
}

func (f *fakeOperator) Execute(_ context.Context, request application.OperatorRequest) (application.OperatorResult, error) {
	f.calls++
	f.request = request
	return f.result, f.err
}
func (f *fakeOperator) PrepareDestructive(_ context.Context, request application.OperatorRequest) (domain.OperationPlan, error) {
	f.calls++
	f.request = request
	return f.plan, f.err
}

type recordingInvocationObserver struct {
	tool         string
	resultClass  string
	policyReason domain.PolicyReason
	correlation  domain.CorrelationID
	started      bool
	finished     bool
}

func (o *recordingInvocationObserver) StartMCPInvocation(ctx context.Context, tool string, correlation domain.CorrelationID) (context.Context, func(string, domain.PolicyReason, time.Duration)) {
	o.started = true
	o.tool = tool
	o.correlation = correlation
	return ctx, func(resultClass string, policyReason domain.PolicyReason, _ time.Duration) {
		o.finished = true
		o.resultClass = resultClass
		o.policyReason = policyReason
	}
}

func testOptions(operator OperatorExecutor) Options {
	return Options{Enabled: true, ImplementationVersion: "1.2.3-test", GuildID: "123456789012345678", ActorID: "mcp-operator", Mode: domain.ModeReadOnly, Toolsets: []application.Toolset{application.ToolsetDiagnostic}, ReadOnly: true, MaxMessageBytes: 1 << 20, MaxConcurrentCalls: 4, CallTimeout: time.Second, Logger: slog.New(slog.NewTextHandler(io.Discard, nil)), Input: strings.NewReader(""), Output: io.Discard, Policy: allowPolicy{}, Operator: operator, Clock: time.Now, Correlation: func() domain.CorrelationID { return "corr-test" }}
}

func TestImplementationVersionValidation(t *testing.T) {
	for _, version := range []string{"", "   ", strings.Repeat("v", maxImplementationVersionBytes+1)} {
		options := testOptions(&fakeOperator{})
		options.ImplementationVersion = version
		if _, err := New(options); err == nil {
			t.Fatalf("New accepted invalid implementation version of %d bytes", len(version))
		}
	}
}

func TestInventoryIsDeterministicScopedAndReadOnly(t *testing.T) {
	server, err := New(testOptions(&fakeOperator{}))
	if err != nil {
		t.Fatal(err)
	}
	previous := ""
	for _, tool := range server.Inventory() {
		if tool.Contract.Name <= previous {
			t.Fatalf("order %q after %q", tool.Contract.Name, previous)
		}
		if tool.Contract.Spec.MutationExposed {
			t.Fatalf("read-only inventory exposed %q", tool.Contract.Name)
		}
		previous = tool.Contract.Name
	}
}

func TestToolHandlerInjectsFixedIdentityAndGuild(t *testing.T) {
	operator := &fakeOperator{result: application.OperatorResult{Value: domain.Channel{ID: "4"}}}
	options := testOptions(operator)
	server, err := New(options)
	if err != nil {
		t.Fatal(err)
	}
	var selected *RegisteredTool
	for index := range server.tools {
		if server.tools[index].Contract.Name == "get_channel" {
			selected = &server.tools[index]
			break
		}
	}
	if selected == nil {
		t.Fatal("get_channel missing")
	}
	result, err := server.handler(selected)(context.Background(), &sdk.CallToolRequest{Params: &sdk.CallToolParamsRaw{Name: "get_channel", Arguments: json.RawMessage(`{"channel_id":"4"}`)}})
	if err != nil || result.IsError {
		t.Fatalf("result=%#v err=%v", result, err)
	}
	if operator.calls != 1 || operator.request.Invocation.Principal != fixedPrincipal || operator.request.Command.GuildScope() != options.GuildID {
		t.Fatalf("request %#v", operator.request)
	}
}

func TestDryRunPlanTelemetry(t *testing.T) {
	operator := &fakeOperator{result: application.OperatorResult{Plan: &domain.OperationPlan{Operation: "get_channel", Mode: domain.ModeDryRun}}}
	observer := &recordingInvocationObserver{}
	options := testOptions(operator)
	options.Mode = domain.ModeDryRun
	options.Observer = observer
	server, err := New(options)
	if err != nil {
		t.Fatal(err)
	}
	var selected *RegisteredTool
	for index := range server.tools {
		if server.tools[index].Contract.Name == "get_channel" {
			selected = &server.tools[index]
			break
		}
	}
	if selected == nil {
		t.Fatal("get_channel missing")
	}
	result, err := server.handler(selected)(context.Background(), &sdk.CallToolRequest{Params: &sdk.CallToolParamsRaw{Name: "get_channel", Arguments: json.RawMessage(`{"channel_id":"4"}`)}})
	if err != nil || result.IsError {
		t.Fatalf("result=%#v err=%v", result, err)
	}
	if operator.request.Invocation.RequestedMode != domain.ModeDryRun {
		t.Fatalf("requested mode = %q", operator.request.Invocation.RequestedMode)
	}
	if !observer.started || !observer.finished || observer.tool != "get_channel" || observer.resultClass != "plan" || observer.correlation != "corr-test" {
		t.Fatalf("observation started=%t finished=%t tool=%q result=%q correlation=%q", observer.started, observer.finished, observer.tool, observer.resultClass, observer.correlation)
	}
}

func TestMCPPolicyDenialIncludesAllowlistedReason(t *testing.T) {
	operator := &fakeOperator{err: &domain.AuthorizationError{Reason: domain.PolicyCapabilityDenied}}
	observer := &recordingInvocationObserver{}
	options := testOptions(operator)
	options.Observer = observer
	server, err := New(options)
	if err != nil {
		t.Fatal(err)
	}
	var selected *RegisteredTool
	for index := range server.tools {
		if server.tools[index].Contract.Name == "get_channel" {
			selected = &server.tools[index]
			break
		}
	}
	if selected == nil {
		t.Fatal("get_channel missing")
	}
	result, err := server.handler(selected)(context.Background(), &sdk.CallToolRequest{Params: &sdk.CallToolParamsRaw{Name: "get_channel", Arguments: json.RawMessage(`{"channel_id":"4"}`)}})
	if err != nil || !result.IsError {
		t.Fatalf("result=%#v err=%v", result, err)
	}
	if observer.resultClass != "policy_denied" || observer.policyReason != domain.PolicyCapabilityDenied {
		t.Fatalf("result=%q policy reason=%q", observer.resultClass, observer.policyReason)
	}
}

func TestStableErrorPolicyReasonRejectsUnknownValue(t *testing.T) {
	code, _, reason := stableError(&domain.AuthorizationError{Reason: domain.PolicyReason("argument-canary")})
	if code != "policy_denied" || reason != "" {
		t.Fatalf("code=%q policy reason=%q", code, reason)
	}
}

func TestMalformedArgumentsNeverReachOperator(t *testing.T) {
	operator := &fakeOperator{}
	server, err := New(testOptions(operator))
	if err != nil {
		t.Fatal(err)
	}
	tool := &server.tools[0]
	result, err := server.handler(tool)(context.Background(), &sdk.CallToolRequest{Params: &sdk.CallToolParamsRaw{Name: tool.Contract.Name, Arguments: json.RawMessage(`{"guild_id":"999"}`)}})
	if err != nil || !result.IsError || operator.calls != 0 {
		t.Fatalf("result=%#v err=%v calls=%d", result, err, operator.calls)
	}
}

func TestMCPToolHandlerSpanPropagatesCancellation(t *testing.T) {
	operator := &cancellingOperator{started: make(chan struct{})}
	observer := &recordingInvocationObserver{}
	options := testOptions(operator)
	options.Observer = observer
	server, err := New(options)
	if err != nil {
		t.Fatal(err)
	}
	var tool *RegisteredTool
	for index := range server.tools {
		if server.tools[index].Contract.Name == "get_channel" {
			tool = &server.tools[index]
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	resultChannel := make(chan *sdk.CallToolResult, 1)
	go func() {
		result, _ := server.handler(tool)(ctx, &sdk.CallToolRequest{Params: &sdk.CallToolParamsRaw{Name: "get_channel", Arguments: json.RawMessage(`{"channel_id":"4"}`)}})
		resultChannel <- result
	}()
	<-operator.started
	cancel()
	select {
	case result := <-resultChannel:
		if !result.IsError || !observer.started || !observer.finished || observer.resultClass != "cancelled" {
			t.Fatalf("cancellation result=%#v observation=%#v", result, observer)
		}
	case <-time.After(time.Second):
		t.Fatal("cancelled handler returned late")
	}
}

func TestSanitizerRemovesSecretShapedValues(t *testing.T) {
	value := sanitizeValue(map[string]any{"token": "secret", "message": "Authorization: Bot should-not-leak", "safe": "ok"}).(map[string]any)
	if _, ok := value["token"]; ok || value["message"] != "[redacted]" || value["safe"] != "ok" {
		t.Fatalf("sanitized %#v", value)
	}
}
