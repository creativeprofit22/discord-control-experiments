package discord

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/ken-kai/discord-control-experiments/internal/application"
	"github.com/ken-kai/discord-control-experiments/internal/domain"
)

const apiOrigin = "https://discord.com/api/v10"
const userAgent = "DiscordBot (https://github.com/ken-kai/discord-control-experiments, 0.1.0)"

type TokenUse func(func(string) error) error

type Client struct {
	useToken    TokenUse
	httpClient  *http.Client
	coordinator *rateLimitCoordinator
}

func New(useToken TokenUse) (*Client, error) {
	transport, ok := http.DefaultTransport.(*http.Transport)
	if !ok {
		return nil, errors.New("default HTTP transport has unexpected type")
	}
	clone := transport.Clone()
	clone.TLSClientConfig = clone.TLSClientConfig.Clone()
	clone.TLSClientConfig.InsecureSkipVerify = false
	return newClient(useToken, clone)
}

func NewWithRoundTripper(useToken TokenUse, roundTripper http.RoundTripper) (*Client, error) {
	if roundTripper == nil {
		return nil, errors.New("round tripper is required")
	}
	return newClient(useToken, roundTripper)
}

func newClient(useToken TokenUse, roundTripper http.RoundTripper) (*Client, error) {
	if useToken == nil {
		return nil, errors.New("discord token use callback is required")
	}
	return &Client{useToken: useToken, httpClient: &http.Client{Transport: roundTripper, Timeout: 30 * time.Second}, coordinator: newRateLimitCoordinator(realClock{})}, nil
}
func (c *Client) Name() domain.Adapter        { return "discord" }
func (c *Client) Start(context.Context) error { return nil }
func (c *Client) Stop(context.Context) error  { return nil }

func (c *Client) ExecuteDiscord(ctx context.Context, command domain.Command) (application.DiscordPortResult, error) {
	if inspection, ok := command.(domain.InspectGuild); ok {
		return c.executeInspection(ctx, inspection)
	}
	return c.execute(ctx, command)
}
func (c *Client) execute(ctx context.Context, command domain.Command) (application.DiscordPortResult, error) {
	route, err := resolveRoute(command)
	if err != nil {
		return application.DiscordPortResult{}, &domain.InvariantError{Detail: "route resolution failed"}
	}
	return c.executeRoute(ctx, command, route)
}
func (c *Client) executeRoute(ctx context.Context, command domain.Command, route resolvedRoute) (application.DiscordPortResult, error) {
	spec, _ := application.LookupCommandSpec(command.CommandName())
	maxAttempts := 1
	if spec.Retry != application.RetryUnsafe && !hasUploads(command) {
		maxAttempts = 3
	}
	var total application.DiscordPortResult
	var last error
	for attempt := 1; attempt <= maxAttempts; attempt++ {
		result, err := c.executeAttempt(ctx, command, route)
		total = result
		total.Attempts = attempt
		if err == nil {
			return total, nil
		}
		last = err
		if ctx.Err() != nil {
			return total, err
		}
		if !retryableError(err, spec.Retry) {
			return total, err
		}
		if attempt < maxAttempts && !isRateLimitError(err) {
			if sleepErr := c.coordinator.clock.Sleep(ctx, time.Duration(1<<(attempt-1))*100*time.Millisecond); sleepErr != nil {
				return total, &domain.CancellationError{Err: sleepErr}
			}
		}
	}
	return total, &domain.RetryExhaustedError{Attempts: total.Attempts, Reason: retryReason(last)}
}
func (c *Client) executeAttempt(ctx context.Context, command domain.Command, route resolvedRoute) (application.DiscordPortResult, error) {
	body, contentType, err := requestBody(command)
	if err != nil {
		return application.DiscordPortResult{}, err
	}
	rawURL := apiOrigin + route.path
	if query := route.query.Encode(); query != "" {
		rawURL += "?" + query
	}
	var result application.DiscordPortResult
	err = c.useToken(func(token string) error {
		if strings.TrimSpace(token) == "" {
			return errors.New("discord token is unavailable")
		}
		request, requestErr := http.NewRequestWithContext(ctx, route.method, rawURL, body)
		if requestErr != nil {
			return &domain.InvariantError{Detail: "build fixed Discord request"}
		}
		request.Header.Set("Authorization", "Bot "+token)
		request.Header.Set("User-Agent", userAgent)
		request.Header.Set("Accept", "application/json")
		if contentType != "" {
			request.Header.Set("Content-Type", contentType)
		}
		if reason := commandContext(command).AuditReason; reason != nil {
			request.Header.Set("X-Audit-Log-Reason", encodeAuditReason(*reason))
		}
		lease, acquireErr := c.coordinator.Acquire(ctx, route.method, route.template, route.major)
		if acquireErr != nil {
			return &domain.CancellationError{Err: acquireErr}
		}
		defer lease.Release()
		response, exchangeErr := c.httpClient.Do(request)
		result = application.DiscordPortResult{Attempts: 1}
		if exchangeErr != nil {
			if ctx.Err() != nil {
				return &domain.CancellationError{Err: ctx.Err()}
			}
			result.Uncertain = isMutation(command)
			result.Sent = result.Uncertain
			return &domain.TransportError{Uncertain: result.Uncertain, Err: errors.New("HTTP exchange failed")}
		}
		result.Sent = true
		defer response.Body.Close()
		if updateErr := lease.Update(response.Header, response.StatusCode); updateErr != nil {
			return &domain.RetryExhaustedError{Attempts: 1, Reason: "malformed rate-limit headers"}
		}
		limit := application.MaxJSONResponseBytes
		if response.StatusCode >= 400 {
			limit = application.MaxErrorResponseBytes
		}
		responseBody, readErr := readBounded(response.Body, limit)
		if readErr != nil {
			result.Uncertain = isMutation(command)
			return readErr
		}
		result.StatusClass = fmt.Sprintf("%dxx", response.StatusCode/100)
		if response.StatusCode < 200 || response.StatusCode >= 300 {
			return decodeDiscordError(response.StatusCode, responseBody)
		}
		value, decodeErr := decodeResult(command, responseBody)
		if decodeErr != nil {
			return decodeErr
		}
		result.Value = value
		return nil
	})
	return result, err
}
func retryableError(err error, retryClass application.RetryClass) bool {
	var apiErr *domain.DiscordAPIError
	if errors.As(err, &apiErr) {
		return apiErr.Status == http.StatusTooManyRequests || apiErr.Status >= 500
	}
	var transportErr *domain.TransportError
	return errors.As(err, &transportErr) && (retryClass == application.RetrySafe || retryClass == application.RetryIdempotent)
}
func isRateLimitError(err error) bool {
	var apiErr *domain.DiscordAPIError
	return errors.As(err, &apiErr) && apiErr.Status == http.StatusTooManyRequests
}
func retryReason(err error) string {
	if isRateLimitError(err) {
		return "rate_limit"
	}
	var apiErr *domain.DiscordAPIError
	if errors.As(err, &apiErr) && apiErr.Status >= 500 {
		return "server_error"
	}
	return "transport"
}
func hasUploads(command domain.Command) bool {
	switch c := command.(type) {
	case domain.CreateMessage:
		return len(c.Uploads) > 0
	case domain.ModifyMessage:
		return len(c.Uploads) > 0
	}
	return false
}
func requestBody(command domain.Command) (io.Reader, string, error) {
	switch c := command.(type) {
	case domain.CreateMessage:
		if len(c.Uploads) > 0 {
			return encodeMultipart(command, c.Uploads)
		}
	case domain.ModifyMessage:
		if len(c.Uploads) > 0 {
			return encodeMultipart(command, c.Uploads)
		}
	}
	return encodeJSONCommand(command)
}
func readBounded(reader io.Reader, limit int64) ([]byte, error) {
	data, err := io.ReadAll(io.LimitReader(reader, limit+1))
	if err != nil {
		return nil, &domain.TransportError{Err: errors.New("read Discord response")}
	}
	if int64(len(data)) > limit {
		return nil, &domain.BodyLimitError{Direction: "response", Limit: limit}
	}
	return data, nil
}
func encodeAuditReason(reason string) string {
	return strings.ReplaceAll(url.QueryEscape(reason), "+", "%20")
}
func isMutation(command domain.Command) bool {
	spec, _ := application.LookupCommandSpec(command.CommandName())
	return spec.Mutation != application.MutationRead
}
func commandContext(command domain.Command) domain.CommandContext {
	switch c := command.(type) {
	case interface{ Context() domain.CommandContext }:
		return c.Context()
	}
	return reflectCommandContext(command)
}
func reflectCommandContext(command domain.Command) domain.CommandContext {
	return domain.CommandContext{GuildID: command.GuildScope(), AuditReason: auditReason(command), IdempotencyKey: idempotencyKey(command), ReconciliationKey: reconciliationKey(command)}
}
func auditReason(command domain.Command) *string { return extractContext(command).AuditReason }
func idempotencyKey(command domain.Command) domain.IdempotencyKey {
	return extractContext(command).IdempotencyKey
}
func reconciliationKey(command domain.Command) domain.ReconciliationKey {
	return extractContext(command).ReconciliationKey
}
func extractContext(command domain.Command) domain.CommandContext {
	switch c := command.(type) {
	case domain.InspectGuild:
		return c.CommandContext
	case domain.ListGuildChannels:
		return c.CommandContext
	case domain.GetChannel:
		return c.CommandContext
	case domain.CreateChannel:
		return c.CommandContext
	case domain.ModifyChannel:
		return c.CommandContext
	case domain.DeleteChannel:
		return c.CommandContext
	case domain.ReorderChannels:
		return c.CommandContext
	case domain.ListGuildRoles:
		return c.CommandContext
	case domain.CreateRole:
		return c.CommandContext
	case domain.ModifyRole:
		return c.CommandContext
	case domain.DeleteRole:
		return c.CommandContext
	case domain.ReorderRoles:
		return c.CommandContext
	case domain.SetChannelPermission:
		return c.CommandContext
	case domain.DeleteChannelPermission:
		return c.CommandContext
	case domain.ListMembers:
		return c.CommandContext
	case domain.GetMember:
		return c.CommandContext
	case domain.ModifyMember:
		return c.CommandContext
	case domain.AddMemberRole:
		return c.CommandContext
	case domain.RemoveMemberRole:
		return c.CommandContext
	case domain.RemoveMember:
		return c.CommandContext
	case domain.CreateMemberBan:
		return c.CommandContext
	case domain.DeleteMemberBan:
		return c.CommandContext
	case domain.ListMessages:
		return c.CommandContext
	case domain.GetMessage:
		return c.CommandContext
	case domain.CreateMessage:
		return c.CommandContext
	case domain.ModifyMessage:
		return c.CommandContext
	case domain.DeleteMessage:
		return c.CommandContext
	case domain.BulkDeleteMessages:
		return c.CommandContext
	case domain.PinMessage:
		return c.CommandContext
	case domain.UnpinMessage:
		return c.CommandContext
	case domain.ListGuildWebhooks:
		return c.CommandContext
	case domain.ListChannelWebhooks:
		return c.CommandContext
	case domain.CreateWebhook:
		return c.CommandContext
	case domain.GetWebhook:
		return c.CommandContext
	case domain.ModifyWebhook:
		return c.CommandContext
	case domain.DeleteWebhook:
		return c.CommandContext
	case domain.ListGuildApplicationCommands:
		return c.CommandContext
	case domain.GetGuildApplicationCommand:
		return c.CommandContext
	case domain.CreateGuildApplicationCommand:
		return c.CommandContext
	case domain.ModifyGuildApplicationCommand:
		return c.CommandContext
	case domain.DeleteGuildApplicationCommand:
		return c.CommandContext
	case domain.BulkOverwriteGuildApplicationCommands:
		return c.CommandContext
	case domain.GetGuildOnboarding:
		return c.CommandContext
	case domain.ModifyGuildOnboarding:
		return c.CommandContext
	case domain.ListGuildInvites:
		return c.CommandContext
	case domain.ListChannelInvites:
		return c.CommandContext
	case domain.GetInvite:
		return c.CommandContext
	case domain.CreateInvite:
		return c.CommandContext
	case domain.DeleteInvite:
		return c.CommandContext
	case domain.ListScheduledEvents:
		return c.CommandContext
	case domain.GetScheduledEvent:
		return c.CommandContext
	case domain.CreateScheduledEvent:
		return c.CommandContext
	case domain.ModifyScheduledEvent:
		return c.CommandContext
	case domain.DeleteScheduledEvent:
		return c.CommandContext
	}
	return domain.CommandContext{GuildID: command.GuildScope()}
}

func (c *Client) executeInspection(ctx context.Context, request domain.InspectGuild) (application.DiscordPortResult, error) {
	guild := request.GuildScope()
	base := domain.CommandContext{GuildID: guild}
	commands := []domain.Command{domain.InspectGuild{CommandContext: base}, domain.ListGuildChannels{CommandContext: base}, domain.ListGuildRoles{CommandContext: base}, domain.ListGuildWebhooks{CommandContext: base}, domain.ListAutoModerationRules{CommandContext: base}, domain.GetGuildOnboarding{CommandContext: base}, domain.ListGuildInvites{CommandContext: base}, domain.ListScheduledEvents{CommandContext: base}}
	if request.ApplicationID != nil {
		commands = append(commands, domain.ListGuildApplicationCommands{CommandContext: base, ApplicationID: *request.ApplicationID})
	}
	var inspection domain.GuildInspection
	attempts := 0
	for i, command := range commands {
		route, err := resolveRoute(command)
		if err != nil {
			return application.DiscordPortResult{Attempts: attempts}, err
		}
		if i == 0 {
			route.path = "/guilds/" + url.PathEscape(string(guild))
		}
		result, err := c.executeRoute(ctx, command, route)
		attempts += result.Attempts
		if err != nil {
			var apiError *domain.DiscordAPIError
			if _, onboardingRead := command.(domain.GetGuildOnboarding); onboardingRead && errors.As(err, &apiError) && apiError.Status == http.StatusNotFound {
				inspection.OnboardingUnsupported = true
				continue
			}
			return application.DiscordPortResult{Attempts: attempts, Sent: result.Sent, Uncertain: result.Uncertain}, err
		}
		switch value := result.Value.(type) {
		case domain.Guild:
			inspection.Guild = value
		case []domain.Channel:
			inspection.Channels = value
		case []domain.Role:
			inspection.Roles = value
		case []domain.Webhook:
			inspection.Webhooks = value
		case domain.Onboarding:
			inspection.Onboarding = &value
		case []domain.ApplicationCommand:
			inspection.ApplicationCommands = value
		case []domain.AutoModerationRule:
			inspection.AutoModerationRules = value
		case []domain.Invite:
			inspection.Invites = value
		case []domain.ScheduledEvent:
			inspection.ScheduledEvents = value
		}
	}
	return application.DiscordPortResult{Value: inspection, Attempts: attempts, Sent: true, StatusClass: "2xx"}, nil
}
