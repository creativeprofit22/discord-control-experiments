package discord

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/ken-kai/discord-control-experiments/internal/domain"
)

type automationCaptureTransport struct {
	request *http.Request
	body    []byte
}

func (t *automationCaptureTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	t.request = request.Clone(request.Context())
	t.body, _ = io.ReadAll(request.Body)
	return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(`{"id":"1","channel_id":"20","author":{"id":"2"},"content":"hello","timestamp":"2026-01-01T00:00:00Z","embeds":[],"attachments":[],"pinned":false}`))}, nil
}

func TestForumThreadRouteAndPayload(t *testing.T) {
	mentions := &domain.AllowedMentions{Parse: []string{}}
	command := domain.StartForumThread{CommandContext: domain.CommandContext{GuildID: "10", IdempotencyKey: "idem"}, ChannelID: "20", Name: "Weekly", Content: "hello", AppliedTags: []string{"30"}, AllowedMentions: mentions}
	route, err := resolveRoute(command)
	if err != nil || route.path != "/channels/20/threads" {
		t.Fatalf("route = %#v, %v", route, err)
	}
	body, _, err := requestBody(command)
	if err != nil {
		t.Fatal(err)
	}
	var payload map[string]any
	if err := json.NewDecoder(body).Decode(&payload); err != nil {
		t.Fatal(err)
	}
	message := payload["message"].(map[string]any)
	if message["allowed_mentions"] == nil || message["content"] != "hello" {
		t.Fatalf("thread payload = %#v", payload)
	}
}

func TestPollPayloadDisablesMentions(t *testing.T) {
	content := "vote"
	command := domain.CreatePoll{CommandContext: domain.CommandContext{GuildID: "10", IdempotencyKey: "idem"}, ChannelID: "20", Content: &content, Question: "Choose", Answers: []domain.PollAnswer{{Text: "A"}, {Text: "B"}}, DurationHours: 24, AllowedMentions: &domain.AllowedMentions{Parse: []string{}}}
	body, _, err := requestBody(command)
	if err != nil {
		t.Fatal(err)
	}
	var payload map[string]any
	_ = json.NewDecoder(body).Decode(&payload)
	poll := payload["poll"].(map[string]any)
	if len(poll["answers"].([]any)) != 2 || payload["allowed_mentions"] == nil || payload["enforce_nonce"] != true {
		t.Fatalf("poll payload = %#v", payload)
	}
}

func TestExecuteWebhookUsesConfiguredSecretWithoutBotAuthorization(t *testing.T) {
	transport := &automationCaptureTransport{}
	client, err := NewWithRoundTripper(staticToken("bot-secret"), transport)
	if err != nil {
		t.Fatal(err)
	}
	if err := client.ConfigureAutomationWebhook("alerts", "10", "20", staticToken("https://discord.com/api/webhooks/123/webhook-secret")); err != nil {
		t.Fatal(err)
	}
	command := domain.ExecuteConfiguredWebhook{CommandContext: domain.CommandContext{GuildID: "10", IdempotencyKey: "idem"}, Alias: "alerts", Content: "hello", AllowedMentions: &domain.AllowedMentions{Parse: []string{}}}
	result, err := client.ExecuteDiscord(context.Background(), command)
	if err != nil || result.Uncertain {
		t.Fatalf("webhook result = %#v, %v", result, err)
	}
	if transport.request.Header.Get("Authorization") != "" || transport.request.URL.Query().Get("wait") != "true" || transport.request.Host != "discord.com" {
		t.Fatal("configured webhook request violated sealed transport contract")
	}
	if strings.Contains(string(transport.body), "webhook-secret") || !strings.Contains(string(transport.body), `"allowed_mentions":{}`) {
		t.Fatal("webhook payload leaked secret or enabled mentions")
	}
}

func TestExecuteWebhookRejectsMalformedURLWithoutPanic(t *testing.T) {
	transport := &automationCaptureTransport{}
	client, err := NewWithRoundTripper(staticToken("bot"), transport)
	if err != nil {
		t.Fatal(err)
	}
	if err := client.ConfigureAutomationWebhook("alerts", "10", "20", staticToken("https://discord.com/api/webhooks/123/%zz")); err != nil {
		t.Fatal(err)
	}
	_, err = client.ExecuteDiscord(context.Background(), domain.ExecuteConfiguredWebhook{CommandContext: domain.CommandContext{GuildID: "10"}, Alias: "alerts", Content: "hello"})
	if err == nil || transport.request != nil {
		t.Fatalf("malformed webhook URL reached transport: request=%v err=%v", transport.request, err)
	}
}

func TestExecuteWebhookRejectsNonCanonicalURL(t *testing.T) {
	client, _ := NewWithRoundTripper(staticToken("bot"), &automationCaptureTransport{})
	_ = client.ConfigureAutomationWebhook("alerts", "10", "20", staticToken("https://example.com/api/webhooks/123/token"))
	_, err := client.ExecuteDiscord(context.Background(), domain.ExecuteConfiguredWebhook{CommandContext: domain.CommandContext{GuildID: "10"}, Alias: "alerts", Content: "hello"})
	if err == nil || strings.Contains(err.Error(), "token") {
		t.Fatalf("invalid webhook error = %v", err)
	}
}
