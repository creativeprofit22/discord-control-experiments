package mcp

import (
	"context"
	"encoding/json"
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

const evaluationTransportDuration = 5 * time.Millisecond

type evaluationClock struct {
	current time.Time
}

func newEvaluationClock() *evaluationClock {
	return &evaluationClock{current: time.Unix(1_000, 0)}
}

func (c *evaluationClock) Now() time.Time { return c.current }
func (c *evaluationClock) Advance(duration time.Duration) {
	if duration < 0 {
		panic("evaluation clock cannot move backwards")
	}
	c.current = c.current.Add(duration)
}

type evaluationOperator struct {
	clock *evaluationClock
}

func (o *evaluationOperator) Execute(_ context.Context, request application.OperatorRequest) (application.OperatorResult, error) {
	o.clock.Advance(evaluationTransportDuration)
	return application.OperatorResult{Value: evaluationResult(request.Handler)}, nil
}

func (o *evaluationOperator) PrepareDestructive(_ context.Context, request application.OperatorRequest) (domain.OperationPlan, error) {
	o.clock.Advance(evaluationTransportDuration)
	contract, ok := application.LookupCommandContract(request.Handler)
	if !ok {
		return domain.OperationPlan{}, fmt.Errorf("evaluation tool %q has no contract", request.Handler)
	}
	return domain.OperationPlan{
		GuildID:          request.Command.GuildScope(),
		Operation:        domain.OperationName(request.Handler),
		Capability:       contract.Spec.Capability,
		Mode:             domain.ModeApply,
		DestructiveClass: application.DestructiveClassForCommand(request.Command, contract.Spec),
		OperationHash:    "evaluation-" + request.Handler,
	}, nil
}

func evaluationResult(tool string) any {
	switch tool {
	case "create_channel":
		return domain.Channel{ID: "2", Type: 0}
	case "list_guild_channels":
		return []domain.Channel{}
	case "set_channel_permission", "create_member_ban":
		return domain.EmptyResult{}
	case "get_member":
		return domain.Member{User: domain.User{ID: "2", Username: "fixture"}, Roles: []domain.RoleID{}, JoinedAt: time.Unix(1_000, 0)}
	case "create_message":
		return domain.Message{ID: "3", ChannelID: "2", Author: domain.User{ID: "2", Username: "fixture"}, Timestamp: time.Unix(1_000, 0), Embeds: []domain.Embed{}, Attachments: []domain.Attachment{}}
	case "create_scheduled_event":
		return domain.ScheduledEvent{ID: "4", GuildID: "123456789012345678", Name: "fixture", ScheduledStartTime: time.Unix(2_000, 0), PrivacyLevel: 2, EntityType: 2}
	case "inspect_guild":
		return domain.GuildInspection{Guild: domain.Guild{ID: "123456789012345678", Name: "fixture", OwnerID: "2"}, Channels: []domain.Channel{}, Roles: []domain.Role{}, Webhooks: []domain.Webhook{}, ApplicationCommands: []domain.ApplicationCommand{}, Invites: []domain.Invite{}, ScheduledEvents: []domain.ScheduledEvent{}}
	case "list_guild_roles":
		return []domain.Role{}
	default:
		return domain.EmptyResult{}
	}
}

type evaluationApprovalIssuer struct{ next int }

func (issuer *evaluationApprovalIssuer) Issue(_ context.Context, grant domain.ApprovalGrant) (domain.ApprovalGrant, error) {
	issuer.next++
	grant.ID = domain.ApprovalID(fmt.Sprintf("evaluation-approval-%d", issuer.next))
	return grant, nil
}

var evaluationArguments = map[string]json.RawMessage{
	"create_channel":         json.RawMessage(`{"name":"project","type":0}`),
	"list_guild_channels":    json.RawMessage(`{}`),
	"set_channel_permission": json.RawMessage(`{"channel_id":"2","overwrite":{"id":"3","type":0,"allow":"1024","deny":"0"}}`),
	"create_member_ban":      json.RawMessage(`{"user_id":"2","delete_message_seconds":60}`),
	"get_member":             json.RawMessage(`{"user_id":"2"}`),
	"create_message":         json.RawMessage(`{"channel_id":"2","content":"prepared announcement"}`),
	"create_scheduled_event": json.RawMessage(`{"channel_id":"2","name":"fixture event","privacy_level":2,"scheduled_start_time":"2030-01-01T00:00:00Z","entity_type":2}`),
	"inspect_guild":          json.RawMessage(`{}`),
	"list_guild_roles":       json.RawMessage(`{}`),
}

func runDeterministicEvaluation(corpus EvaluationCorpus) (map[string]EvaluationObservation, error) {
	clock := newEvaluationClock()
	operator := &evaluationOperator{clock: clock}
	nextCorrelationID := 0
	approval, err := application.NewApprovalCoordinator(&evaluationApprovalIssuer{}, clock, time.Minute)
	if err != nil {
		return nil, err
	}
	options := Options{
		Enabled:               true,
		ImplementationVersion: "evaluation-test",
		GuildID:               "123456789012345678",
		ActorID:               "mcp-evaluator",
		Mode:                  domain.ModeApply,
		Toolsets:              []application.Toolset{application.ToolsetBuild, application.ToolsetModeration, application.ToolsetNews, application.ToolsetDiagnostic},
		ReadOnly:              false,
		MaxMessageBytes:       1 << 20,
		MaxConcurrentCalls:    1,
		CallTimeout:           time.Second,
		Logger:                slog.New(slog.NewTextHandler(io.Discard, nil)),
		Input:                 strings.NewReader(""),
		Output:                io.Discard,
		Policy:                allowPolicy{},
		Operator:              operator,
		Approval:              approval,
		Clock:                 clock.Now,
		Correlation: func() domain.CorrelationID {
			nextCorrelationID++
			return domain.CorrelationID(fmt.Sprintf("evaluation-%d", nextCorrelationID))
		},
		FormElicitationSupported: func(*sdk.CallToolRequest) bool { return true },
	}
	server, err := New(options)
	if err != nil {
		return nil, err
	}
	tools := make(map[string]*RegisteredTool, len(server.tools))
	for index := range server.tools {
		tools[server.tools[index].Contract.Name] = &server.tools[index]
	}

	observations := make(map[string]EvaluationObservation, len(corpus.Tasks))
	for _, task := range corpus.Tasks {
		started := clock.Now()
		roundTrips := 0
		for _, toolName := range task.ExpectedTools {
			tool := tools[toolName]
			arguments, hasArguments := evaluationArguments[toolName]
			if tool == nil || !hasArguments {
				return nil, fmt.Errorf("evaluation fixture %q has no executable path for %q", task.ID, toolName)
			}
			request := &sdk.CallToolRequest{Params: &sdk.CallToolParamsRaw{Name: toolName, Arguments: arguments}}
			result, handlerErr := server.handler(tool)(context.Background(), request)
			roundTrips++
			if handlerErr != nil {
				return nil, fmt.Errorf("evaluation fixture %q tool %q: %w", task.ID, toolName, handlerErr)
			}
			if result == nil {
				return nil, fmt.Errorf("evaluation fixture %q tool %q returned no result", task.ID, toolName)
			}
			if len(result.InputRequests) > 0 {
				request.Params.RequestState = result.RequestState
				request.Params.InputResponses = sdk.InputResponseMap{"confirm": &sdk.ElicitResult{Action: "accept", Content: map[string]any{"confirm": true}}}
				result, handlerErr = server.handler(tool)(context.Background(), request)
				roundTrips++
			}
			if handlerErr != nil {
				return nil, fmt.Errorf("evaluation fixture %q approved tool %q: %w", task.ID, toolName, handlerErr)
			}
			if result == nil || result.IsError {
				return nil, fmt.Errorf("evaluation fixture %q tool %q failed", task.ID, toolName)
			}
		}
		observations[task.ID] = EvaluationObservation{
			SelectedTools: append([]string(nil), task.ExpectedTools...),
			RoundTrips:    roundTrips,
			Refused:       task.Unsafe,
			LatencyMS:     float64(clock.Now().Sub(started)) / float64(time.Millisecond),
		}
	}
	return observations, nil
}

func TestEvaluationFakeOperatorLatencyBudgets(t *testing.T) {
	corpus, err := ParseEvaluationCorpus(evaluationCorpusJSON)
	if err != nil {
		t.Fatal(err)
	}
	observations, err := runDeterministicEvaluation(corpus)
	if err != nil {
		t.Fatal(err)
	}
	for _, task := range corpus.Tasks {
		if got := observations[task.ID].LatencyMS; got > task.LatencyBudgetMS {
			t.Errorf("%s latency %.1fms exceeds %.1fms budget", task.ID, got, task.LatencyBudgetMS)
		}
	}
	report := Evaluate(corpus, observations, nil)
	if report.LatencyP50MS != 5 || report.LatencyP95MS != 15 {
		t.Fatalf("latency percentiles p50=%.1fms p95=%.1fms", report.LatencyP50MS, report.LatencyP95MS)
	}
}
