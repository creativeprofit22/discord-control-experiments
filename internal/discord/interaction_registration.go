package discord

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"regexp"
	"strings"

	"github.com/ken-kai/discord-control-experiments/internal/application"
	"github.com/ken-kai/discord-control-experiments/internal/domain"
)

var registrationIDPattern = regexp.MustCompile(`^[1-9][0-9]{0,19}$`)

func (c *Client) ListApplicationCommands(ctx context.Context, scope application.CommandRegistrationScope) ([]application.ApplicationCommandDefinition, error) {
	return c.applicationCommands(ctx, http.MethodGet, scope, nil)
}
func (c *Client) BulkOverwriteApplicationCommands(ctx context.Context, scope application.CommandRegistrationScope, manifest []application.ApplicationCommandDefinition) ([]application.ApplicationCommandDefinition, error) {
	return c.applicationCommands(ctx, http.MethodPut, scope, manifest)
}
func (c *Client) applicationCommands(ctx context.Context, method string, scope application.CommandRegistrationScope, manifest []application.ApplicationCommandDefinition) ([]application.ApplicationCommandDefinition, error) {
	path, template, major, err := registrationRoute(scope)
	if err != nil {
		return nil, err
	}
	var encoded []byte
	if method == http.MethodPut {
		encoded, err = json.Marshal(manifest)
		if err != nil || len(encoded) > 128<<10 {
			return nil, &domain.BodyLimitError{Direction: "request", Limit: 128 << 10}
		}
	}
	for attempt := 0; attempt < 3; attempt++ {
		var result []application.ApplicationCommandDefinition
		status, exchangeErr := c.registrationAttempt(ctx, method, path, template, major, encoded, &result)
		if exchangeErr == nil {
			return result, nil
		}
		if ctx.Err() != nil || (status != http.StatusTooManyRequests && status < 500) || attempt == 2 {
			return nil, exchangeErr
		}
	}
	return nil, errors.New("application command registration unavailable")
}
func (c *Client) registrationAttempt(ctx context.Context, method, path, template, major string, body []byte, result *[]application.ApplicationCommandDefinition) (int, error) {
	status := 0
	err := c.useToken(func(token string) error {
		if strings.TrimSpace(token) == "" {
			return errors.New("discord token is unavailable")
		}
		var requestBody *bytes.Reader
		if body == nil {
			requestBody = bytes.NewReader(nil)
		} else {
			requestBody = bytes.NewReader(body)
		}
		request, err := http.NewRequestWithContext(ctx, method, apiOrigin+path, requestBody)
		if err != nil {
			return &domain.InvariantError{Detail: "build application command request"}
		}
		request.Header.Set("Authorization", "Bot "+token)
		request.Header.Set("User-Agent", userAgent)
		request.Header.Set("Accept", "application/json")
		if method == http.MethodPut {
			request.Header.Set("Content-Type", "application/json")
		}
		lease, err := c.coordinator.Acquire(ctx, method, template, major)
		if err != nil {
			return &domain.CancellationError{Err: err}
		}
		defer lease.Release()
		response, err := c.httpClient.Do(request)
		if err != nil {
			return &domain.TransportError{Uncertain: method == http.MethodPut, Err: errors.New("application command exchange failed")}
		}
		defer response.Body.Close()
		status = response.StatusCode
		if err := lease.Update(response.Header, response.StatusCode); err != nil {
			return &domain.RetryExhaustedError{Attempts: 1, Reason: "malformed rate-limit headers"}
		}
		limit := int64(256 << 10)
		if response.StatusCode >= 400 {
			limit = application.MaxErrorResponseBytes
		}
		responseBody, err := readBounded(response.Body, limit)
		if err != nil {
			return err
		}
		if response.StatusCode < 200 || response.StatusCode >= 300 {
			return decodeDiscordError(response.StatusCode, responseBody)
		}
		decoder := json.NewDecoder(bytes.NewReader(responseBody))
		if err := decoder.Decode(result); err != nil || len(*result) > 100 {
			return &domain.TransportError{Err: errors.New("decode application command response")}
		}
		return nil
	})
	return status, err
}
func registrationRoute(scope application.CommandRegistrationScope) (string, string, string, error) {
	if !registrationIDPattern.MatchString(scope.ApplicationID) {
		return "", "", "", errors.New("invalid application command scope")
	}
	if scope.Kind == "global" && scope.GuildID == "" {
		return "/applications/" + scope.ApplicationID + "/commands", "/applications/{application}/commands", scope.ApplicationID, nil
	}
	if scope.Kind == "guild" && registrationIDPattern.MatchString(scope.GuildID) {
		return "/applications/" + scope.ApplicationID + "/guilds/" + scope.GuildID + "/commands", "/applications/{application}/guilds/{guild}/commands", scope.ApplicationID + ":" + scope.GuildID, nil
	}
	return "", "", "", errors.New("invalid application command scope")
}
