package mcp

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/ken-kai/discord-control-experiments/internal/application"
	"github.com/ken-kai/discord-control-experiments/internal/domain"
	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

const (
	ProtocolVersion               = "2026-07-28"
	maxImplementationVersionBytes = 128
)

var fixedPrincipal = domain.Principal{Adapter: "mcp", Subject: "local-stdio", AuthenticationMethod: "os-process"}

type OperatorExecutor interface {
	Execute(context.Context, application.OperatorRequest) (application.OperatorResult, error)
	PrepareDestructive(context.Context, application.OperatorRequest) (domain.OperationPlan, error)
}

type InvocationObserver interface {
	StartMCPInvocation(context.Context, string, domain.CorrelationID) (context.Context, func(string, domain.PolicyReason, time.Duration))
}

type Options struct {
	Enabled                  bool
	ImplementationVersion    string
	GuildID                  domain.GuildID
	ActorID                  domain.ActorID
	Mode                     domain.ExecutionMode
	Toolsets                 []application.Toolset
	ReadOnly                 bool
	MaxMessageBytes          int
	MaxConcurrentCalls       int
	CallTimeout              time.Duration
	Logger                   *slog.Logger
	Input                    io.Reader
	Output                   io.Writer
	Policy                   application.PolicyEvaluator
	Operator                 OperatorExecutor
	Approval                 *application.ApprovalCoordinator
	FormElicitationSupported func(*sdk.CallToolRequest) bool
	Observer                 InvocationObserver
	Clock                    func() time.Time
	Correlation              func() domain.CorrelationID
}

type RegisteredTool struct {
	Contract application.CommandContract
	Input    ToolSchema
	Output   ToolSchema
}

type Server struct {
	enabled     bool
	options     Options
	sdk         *sdk.Server
	tools       []RegisteredTool
	pending     *pendingApprovals
	semaphore   chan struct{}
	lifecycleMu sync.Mutex
	cancel      context.CancelFunc
	done        chan error
	finished    chan struct{}
	serveErr    error
}

func New(options Options) (*Server, error) {
	if options.Logger == nil {
		return nil, errors.New("MCP logger is required")
	}
	if options.Clock == nil {
		options.Clock = time.Now
	}
	if options.Correlation == nil {
		options.Correlation = randomCorrelation
	}
	if options.FormElicitationSupported == nil {
		options.FormElicitationSupported = supportsFormElicitation
	}
	if options.Input == nil {
		options.Input = io.Reader(nil)
	}
	if options.Output == nil {
		options.Output = io.Writer(nil)
	}
	server := &Server{enabled: options.Enabled, options: options, pending: newPendingApprovals(), semaphore: make(chan struct{}, max(1, options.MaxConcurrentCalls)), done: make(chan error, 1), finished: make(chan struct{})}
	if !options.Enabled {
		return server, nil
	}
	implementationVersion := strings.TrimSpace(options.ImplementationVersion)
	if implementationVersion == "" || len(implementationVersion) > maxImplementationVersionBytes {
		return nil, errors.New("MCP implementation version must contain 1 to 128 bytes")
	}
	options.ImplementationVersion = implementationVersion
	server.options = options
	if options.GuildID == "" || options.ActorID == "" || !domain.ValidExecutionMode(options.Mode) || len(options.Toolsets) == 0 || options.MaxMessageBytes < 4096 || options.MaxConcurrentCalls < 1 || options.CallTimeout < time.Second || options.Input == nil || options.Output == nil || options.Policy == nil || options.Operator == nil || (options.Mode == domain.ModeApply && options.Approval == nil) {
		return nil, errors.New("MCP configuration or application dependencies are invalid")
	}
	inventory, err := buildInventory(context.Background(), options)
	if err != nil {
		return nil, err
	}
	if len(inventory) == 0 {
		return nil, errors.New("MCP policy-scoped inventory is empty")
	}
	server.tools = inventory
	server.sdk = sdk.NewServer(&sdk.Implementation{Name: "discord-control", Version: options.ImplementationVersion}, &sdk.ServerOptions{Logger: slog.New(&sanitizedSlogHandler{logger: options.Logger}), Capabilities: &sdk.ServerCapabilities{}, PageSize: len(inventory)})
	for index := range server.tools {
		registered := &server.tools[index]
		readOnly := !registered.Contract.Spec.MutationExposed
		destructive := application.MayRequireDestructiveApproval(registered.Contract.Spec)
		idempotent := registered.Contract.Spec.Idempotency != application.IdempotencyNone
		server.sdk.AddTool(&sdk.Tool{Name: registered.Contract.Name, Description: registered.Contract.Description, InputSchema: registered.Input.Document, OutputSchema: registered.Output.Document, Annotations: &sdk.ToolAnnotations{ReadOnlyHint: readOnly, DestructiveHint: &destructive, IdempotentHint: idempotent}}, server.handler(registered))
	}
	return server, nil
}

func buildInventory(ctx context.Context, options Options) ([]RegisteredTool, error) {
	allowedSets := map[application.Toolset]bool{}
	for _, set := range options.Toolsets {
		switch set {
		case application.ToolsetBuild, application.ToolsetModeration, application.ToolsetNews, application.ToolsetDiagnostic:
		default:
			return nil, errors.New("MCP toolset is unknown")
		}
		if allowedSets[set] {
			return nil, errors.New("MCP toolset is duplicated")
		}
		allowedSets[set] = true
	}
	correlation := options.Correlation()
	if correlation == "" {
		return nil, errors.New("MCP correlation generation failed")
	}
	catalog, err := application.CommandCatalogFor(ctx, options.Policy, application.DiscoveryContext{Principal: fixedPrincipal, GuildID: options.GuildID, Mode: options.Mode, CorrelationID: correlation})
	if err != nil {
		return nil, errors.New("MCP policy discovery failed")
	}
	policyAllowed := map[string]bool{}
	for _, spec := range catalog {
		policyAllowed[spec.Name] = true
	}
	inventory := []RegisteredTool{}
	for _, contract := range application.CommandContracts() {
		selected := false
		for _, set := range contract.Toolsets {
			if allowedSets[set] {
				selected = true
				break
			}
		}
		if !selected || !policyAllowed[contract.Name] || (options.ReadOnly && contract.Spec.MutationExposed) {
			continue
		}
		input, output, schemaErr := GenerateSchemas(contract)
		if schemaErr != nil {
			return nil, schemaErr
		}
		inventory = append(inventory, RegisteredTool{Contract: contract, Input: input, Output: output})
	}
	sort.Slice(inventory, func(i, j int) bool { return inventory[i].Contract.Name < inventory[j].Contract.Name })
	return inventory, nil
}

func (s *Server) Inventory() []RegisteredTool {
	result := make([]RegisteredTool, len(s.tools))
	copy(result, s.tools)
	return result
}

func (s *Server) handler(tool *RegisteredTool) sdk.ToolHandler {
	return func(parent context.Context, request *sdk.CallToolRequest) (*sdk.CallToolResult, error) {
		started := s.options.Clock()
		correlation := s.options.Correlation()
		resultClass := "ok"
		var policyReason domain.PolicyReason
		ctx := parent
		var finishObservation func(string, domain.PolicyReason, time.Duration)
		if s.options.Observer != nil {
			ctx, finishObservation = s.options.Observer.StartMCPInvocation(parent, tool.Contract.Name, correlation)
		}
		if finishObservation != nil {
			defer func() {
				finishObservation(resultClass, policyReason, s.options.Clock().Sub(started))
			}()
		}
		if correlation == "" {
			resultClass = "internal_error"
			return s.errorResult(tool, "internal_error", "request correlation unavailable", ""), nil
		}
		ctx, cancel := context.WithTimeout(ctx, s.options.CallTimeout)
		defer cancel()
		select {
		case s.semaphore <- struct{}{}:
			defer func() { <-s.semaphore }()
		case <-ctx.Done():
			resultClass = "queue_rejected"
			return s.errorResult(tool, "limit_exceeded", "tool concurrency queue timed out", correlation), nil
		}
		command, err := application.NewContractCommand(tool.Contract.Name, s.options.GuildID)
		if err != nil {
			resultClass = "internal_error"
			return s.errorResult(tool, "internal_error", "tool contract unavailable", correlation), nil
		}
		if request == nil || request.Params == nil {
			resultClass = "invalid_arguments"
			return s.errorResult(tool, "invalid_arguments", "arguments are required", correlation), nil
		}
		if err = tool.Input.DecodeCommand(request.Params.Arguments, command); err != nil {
			resultClass = "schema_error"
			return s.errorResult(tool, "invalid_arguments", "arguments do not match the closed tool schema", correlation), nil
		}
		if err = application.BindOperatorInvocationFor(command, correlation, "mcp"); err != nil {
			resultClass = "internal_error"
			return s.errorResult(tool, "internal_error", "invocation binding failed", correlation), nil
		}
		invocation := application.Invocation{Principal: fixedPrincipal, RequestedMode: s.options.Mode, CorrelationID: correlation, ActorID: s.options.ActorID}
		operatorRequest := application.OperatorRequest{Handler: tool.Contract.Name, Invocation: invocation, Command: command}
		if application.DestructiveClassForCommand(command, tool.Contract.Spec) != domain.DestructiveNone && s.options.Mode == domain.ModeApply {
			approvalResult, approvalClass, approvalReason := s.handleDestructive(ctx, request, tool, operatorRequest, correlation)
			resultClass = approvalClass
			policyReason = approvalReason
			return approvalResult, nil
		}
		outcome, executeErr := s.options.Operator.Execute(ctx, operatorRequest)
		if executeErr != nil {
			code, detail, reason := stableError(executeErr)
			resultClass = code
			policyReason = reason
			return s.errorResult(tool, code, detail, correlation), nil
		}
		if outcome.Plan != nil {
			resultClass = "plan"
			return s.planResult(tool, *outcome.Plan, correlation), nil
		}
		return s.valueResult(tool, outcome.Value, correlation), nil
	}
}

type resultEnvelope struct {
	OK            bool               `json:"ok"`
	Kind          string             `json:"kind"`
	CorrelationID string             `json:"correlation_id"`
	Value         any                `json:"value,omitempty"`
	Plan          *safeOperationPlan `json:"plan,omitempty"`
	Error         *safeError         `json:"error,omitempty"`
}
type safeError struct {
	Code   string `json:"code"`
	Detail string `json:"detail"`
}

func (s *Server) valueResult(tool *RegisteredTool, value any, correlation domain.CorrelationID) *sdk.CallToolResult {
	return s.render(tool, resultEnvelope{OK: true, Kind: "value", CorrelationID: string(correlation), Value: sanitizeValue(value)}, false)
}
func (s *Server) planResult(tool *RegisteredTool, plan domain.OperationPlan, correlation domain.CorrelationID) *sdk.CallToolResult {
	safe := safePlan(plan)
	return s.render(tool, resultEnvelope{OK: true, Kind: "plan", CorrelationID: string(correlation), Plan: &safe}, false)
}
func (s *Server) approvalPlanResult(tool *RegisteredTool, plan domain.OperationPlan, correlation domain.CorrelationID) *sdk.CallToolResult {
	safe := safePlan(plan)
	return s.render(tool, resultEnvelope{OK: false, Kind: "approval_required", CorrelationID: string(correlation), Plan: &safe}, true)
}
func (s *Server) errorResult(tool *RegisteredTool, code, detail string, correlation domain.CorrelationID) *sdk.CallToolResult {
	return s.render(tool, resultEnvelope{OK: false, Kind: "error", CorrelationID: string(correlation), Error: &safeError{Code: sanitizeString(code, 64), Detail: sanitizeString(detail, 512)}}, true)
}
func (s *Server) render(tool *RegisteredTool, envelope resultEnvelope, isError bool) *sdk.CallToolResult {
	raw, err := json.Marshal(envelope)
	if err != nil {
		return &sdk.CallToolResult{IsError: true, Content: []sdk.Content{&sdk.TextContent{Text: `{"ok":false,"kind":"error","correlation_id":"unknown","error":{"code":"internal_error","detail":"result encoding failed"}}`}}}
	}
	if _, err = tool.Output.ValidateRaw(raw); err != nil {
		s.options.Logger.Error("MCP output validation failed", "tool", tool.Contract.Name)
		raw = []byte(`{"ok":false,"kind":"error","correlation_id":"unknown","error":{"code":"internal_error","detail":"result validation failed"}}`)
		isError = true
	}
	var structured any
	_ = json.Unmarshal(raw, &structured)
	return &sdk.CallToolResult{IsError: isError, StructuredContent: structured, Content: []sdk.Content{&sdk.TextContent{Text: string(raw)}}}
}

func safePlan(plan domain.OperationPlan) safeOperationPlan {
	intent := make([]safeIntent, 0, len(plan.Intent))
	keys := make([]string, 0, len(plan.Intent))
	for key := range plan.Intent {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		intent = append(intent, safeIntent{Key: sanitizeString(key, 64), Value: sanitizeString(plan.Intent[key], 512)})
	}
	kinds := make([]string, len(plan.ResourceKinds))
	for i, kind := range plan.ResourceKinds {
		kinds[i] = string(kind)
	}
	return safeOperationPlan{Operation: string(plan.Operation), Capability: string(plan.Capability), Mode: string(plan.Mode), DestructiveClass: string(plan.DestructiveClass), ResourceKinds: kinds, Intent: intent, OperationHash: sanitizeString(plan.OperationHash, 128), Preapproved: plan.Preapproved}
}

func stableError(err error) (string, string, domain.PolicyReason) {
	switch {
	case errors.Is(err, context.Canceled), errors.Is(err, context.DeadlineExceeded):
		return "cancelled", "operation was cancelled", ""
	}
	var authorization *domain.AuthorizationError
	if errors.As(err, &authorization) {
		return "policy_denied", "operation is not authorized", observablePolicyReason(authorization)
	}
	var approval *domain.ApprovalError
	if errors.As(err, &approval) {
		return "approval_required", "destructive approval is required", ""
	}
	var validation *domain.ValidationError
	if errors.As(err, &validation) {
		return "invalid_arguments", "arguments failed application validation", ""
	}
	var ownership *domain.OwnershipError
	if errors.As(err, &ownership) {
		return "ownership_denied", "resource is outside the configured guild", ""
	}
	return "operation_failed", "operation could not be completed", ""
}

func observablePolicyReason(authorization *domain.AuthorizationError) domain.PolicyReason {
	if authorization == nil {
		return ""
	}
	switch authorization.Reason {
	case domain.PolicyAllowed, domain.PolicyUnknownPrincipal, domain.PolicyAmbiguousPrincipal, domain.PolicyMalformedPrincipal,
		domain.PolicyGuildDenied, domain.PolicyCapabilityDenied, domain.PolicyModeDenied, domain.PolicyDestructiveDenied,
		domain.PolicyResourceDenied, domain.PolicyUnknownCommand, domain.PolicyError, domain.PolicyApprovalRequired,
		domain.PolicyApprovalInvalid, domain.PolicyAuditUnavailable:
		return authorization.Reason
	default:
		return ""
	}
}

func sanitizeValue(value any) any {
	raw, err := json.Marshal(value)
	if err != nil {
		return map[string]any{}
	}
	var decoded any
	if json.Unmarshal(raw, &decoded) != nil {
		return map[string]any{}
	}
	return sanitizeAny(decoded, 0)
}
func sanitizeAny(value any, depth int) any {
	if depth > maxSchemaDepth {
		return "[truncated]"
	}
	switch current := value.(type) {
	case string:
		return sanitizeString(current, maxStringLength)
	case []any:
		if len(current) > maxArrayItems {
			current = current[:maxArrayItems]
		}
		for i := range current {
			current[i] = sanitizeAny(current[i], depth+1)
		}
		return current
	case map[string]any:
		result := map[string]any{}
		count := 0
		for key, item := range current {
			if count >= maxObjectFields {
				break
			}
			lower := strings.ToLower(key)
			if strings.Contains(lower, "token") || strings.Contains(lower, "authorization") || strings.Contains(lower, "secret") || strings.Contains(lower, "approval_id") {
				continue
			}
			result[key] = sanitizeAny(item, depth+1)
			count++
		}
		return result
	default:
		return value
	}
}
func sanitizeString(value string, maximum int) string {
	value = strings.Map(func(r rune) rune {
		if r < ' ' && r != '\n' && r != '\t' {
			return -1
		}
		return r
	}, value)
	lower := strings.ToLower(value)
	if strings.Contains(lower, "bot ") || strings.Contains(lower, "authorization:") || strings.Contains(lower, "webhook") && strings.Contains(lower, "token") {
		value = "[redacted]"
	}
	if maximum <= 0 {
		return ""
	}
	runes := []rune(value)
	if len(runes) > maximum {
		value = string(runes[:maximum])
	}
	return value
}
func randomCorrelation() domain.CorrelationID {
	var value [16]byte
	if _, err := rand.Read(value[:]); err != nil {
		return ""
	}
	return domain.CorrelationID(hex.EncodeToString(value[:]))
}

const maxSDKLogAttrs = 5

type sanitizedSlogAttr struct {
	attr       slog.Attr
	groupDepth int
}

type sanitizedSlogHandler struct {
	logger *slog.Logger
	attrs  []sanitizedSlogAttr
	groups []string
}

func (h *sanitizedSlogHandler) Enabled(context.Context, slog.Level) bool { return true }
func (h *sanitizedSlogHandler) Handle(ctx context.Context, record slog.Record) error {
	handler := h.logger.Handler().WithAttrs([]slog.Attr{slog.String("component", "mcp-sdk")})
	groupDepth := 0
	for _, persistent := range h.attrs {
		for groupDepth < persistent.groupDepth {
			handler = handler.WithGroup(h.groups[groupDepth])
			groupDepth++
		}
		handler = handler.WithAttrs([]slog.Attr{persistent.attr})
	}
	for groupDepth < len(h.groups) {
		handler = handler.WithGroup(h.groups[groupDepth])
		groupDepth++
	}

	sanitized := slog.NewRecord(record.Time, slog.LevelInfo, sanitizeString(record.Message, 256), record.PC)
	if record.Level >= slog.LevelError {
		sanitized.Level = slog.LevelError
	}
	if !handler.Enabled(ctx, sanitized.Level) {
		return nil
	}
	remaining := maxSDKLogAttrs - len(h.attrs)
	record.Attrs(func(attr slog.Attr) bool {
		if remaining == 0 {
			return false
		}
		sanitized.AddAttrs(sanitizeSDKLogAttr(attr))
		remaining--
		return remaining > 0
	})
	return handler.Handle(ctx, sanitized)
}

func (h *sanitizedSlogHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	remaining := maxSDKLogAttrs - len(h.attrs)
	if remaining <= 0 || len(attrs) == 0 {
		return h
	}
	if len(attrs) > remaining {
		attrs = attrs[:remaining]
	}
	clone := &sanitizedSlogHandler{
		logger: h.logger,
		attrs:  append([]sanitizedSlogAttr(nil), h.attrs...),
		groups: append([]string(nil), h.groups...),
	}
	for _, attr := range attrs {
		clone.attrs = append(clone.attrs, sanitizedSlogAttr{attr: sanitizeSDKLogAttr(attr), groupDepth: len(clone.groups)})
	}
	return clone
}

func (h *sanitizedSlogHandler) WithGroup(name string) slog.Handler {
	if name == "" || len(h.groups) >= maxSDKLogAttrs {
		return h
	}
	clone := &sanitizedSlogHandler{
		logger: h.logger,
		attrs:  append([]sanitizedSlogAttr(nil), h.attrs...),
		groups: append([]string(nil), h.groups...),
	}
	clone.groups = append(clone.groups, sanitizeString(name, 64))
	return clone
}

func sanitizeSDKLogAttr(attr slog.Attr) slog.Attr {
	return slog.String(sanitizeString(attr.Key, 64), sanitizeString(fmt.Sprint(attr.Value.Any()), 256))
}

func (s *Server) Name() domain.Adapter { return "mcp" }
func (s *Server) Start(ctx context.Context) error {
	if !s.enabled {
		return nil
	}
	s.lifecycleMu.Lock()
	defer s.lifecycleMu.Unlock()
	if s.cancel != nil {
		return errors.New("MCP server already started")
	}
	serveCtx, cancel := context.WithCancel(ctx)
	s.cancel = cancel
	go func() {
		err := s.Serve(serveCtx)
		s.lifecycleMu.Lock()
		s.serveErr = err
		s.lifecycleMu.Unlock()
		select {
		case s.done <- err:
		default:
		}
		close(s.finished)
	}()
	return nil
}
func (s *Server) Stop(ctx context.Context) error {
	s.lifecycleMu.Lock()
	cancel := s.cancel
	s.lifecycleMu.Unlock()
	if cancel == nil {
		return nil
	}
	cancel()
	select {
	case <-s.finished:
		s.lifecycleMu.Lock()
		err := s.serveErr
		s.lifecycleMu.Unlock()
		if errors.Is(err, context.Canceled) || errors.Is(err, io.EOF) {
			return nil
		}
		return err
	case <-ctx.Done():
		return ctx.Err()
	}
}
func (s *Server) Done() <-chan error { return s.done }
func (s *Server) Serve(ctx context.Context) error {
	if !s.enabled {
		return nil
	}
	transport := &boundedStdioTransport{reader: nopReadCloser{s.options.Input}, writer: nopWriteCloser{s.options.Output}, maxBytes: s.options.MaxMessageBytes}
	err := s.sdk.Run(ctx, transport)
	if errors.Is(err, io.EOF) || errors.Is(err, context.Canceled) {
		return nil
	}
	return err
}
