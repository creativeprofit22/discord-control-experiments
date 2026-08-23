package discord

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ken-kai/discord-control-experiments/internal/domain"
	"github.com/ken-kai/discord-control-experiments/internal/testsupport"
)

func staticToken(value string) TokenUse {
	return func(consumer func(string) error) error { return consumer(value) }
}

type tokenCaptureTransport struct {
	mu      sync.Mutex
	headers []string
}

func (t *tokenCaptureTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	t.mu.Lock()
	t.headers = append(t.headers, request.Header.Get("Authorization"))
	t.mu.Unlock()
	return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(`[]`))}, nil
}

func TestSecretReferencesRotateWithoutExposure(t *testing.T) {
	var mu sync.RWMutex
	token := "first-rotation-canary"
	use := func(consumer func(string) error) error {
		mu.RLock()
		current := token
		mu.RUnlock()
		return consumer(current)
	}
	transport := &tokenCaptureTransport{}
	client, err := NewWithRoundTripper(use, transport)
	if err != nil {
		t.Fatal(err)
	}
	command := domain.ListGuildChannels{CommandContext: domain.CommandContext{GuildID: "1"}}
	if _, err := client.ExecuteDiscord(context.Background(), command); err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	token = "second-rotation-canary"
	mu.Unlock()
	if _, err := client.ExecuteDiscord(context.Background(), command); err != nil {
		t.Fatal(err)
	}
	transport.mu.Lock()
	defer transport.mu.Unlock()
	if len(transport.headers) != 2 || transport.headers[0] != "Bot first-rotation-canary" || transport.headers[1] != "Bot second-rotation-canary" {
		t.Fatalf("authorization rotation failed: %#v", transport.headers)
	}
}

func TestRepresentativeRoutes(t *testing.T) {
	g := domain.CommandContext{GuildID: "1"}
	tests := []struct {
		command      domain.Command
		method, path string
	}{{domain.GetCurrentUser{CommandContext: g}, "GET", "/users/@me"}, {domain.ListGuildChannels{CommandContext: g}, "GET", "/guilds/1/channels"}, {domain.CreateRole{CommandContext: g}, "POST", "/guilds/1/roles"}, {domain.GetMember{CommandContext: g, UserID: "2"}, "GET", "/guilds/1/members/2"}, {domain.GetMessage{CommandContext: g, ChannelID: "3", MessageID: "4"}, "GET", "/channels/3/messages/4"}, {domain.GetWebhook{CommandContext: g, WebhookID: "5"}, "GET", "/webhooks/5"}, {domain.ListGuildApplicationCommands{CommandContext: g, ApplicationID: "6"}, "GET", "/applications/6/guilds/1/commands"}, {domain.GetInvite{CommandContext: g, Code: "abc"}, "GET", "/invites/abc"}, {domain.GetScheduledEvent{CommandContext: g, ScheduledEventID: "7"}, "GET", "/guilds/1/scheduled-events/7"}}
	for _, test := range tests {
		route, err := resolveRoute(test.command)
		if err != nil {
			t.Fatal(err)
		}
		if route.method != test.method || route.path != test.path {
			t.Errorf("%s: got %s %s", test.command.CommandName(), route.method, route.path)
		}
	}
}
func TestClientRetries429AndRedactsCapture(t *testing.T) {
	headers := map[string]string{"X-RateLimit-Bucket": "bucket", "X-RateLimit-Limit": "1", "X-RateLimit-Remaining": "0", "X-RateLimit-Reset-After": "0", "X-RateLimit-Scope": "user", "Retry-After": "0"}
	fake := testsupport.NewScriptedDiscord(testsupport.DiscordStep{Method: "GET", Path: "/api/v10/guilds/1/channels", Status: 429, ResponseHeaders: headers, Body: `{"code":0,"message":"canary-secret"}`}, testsupport.DiscordStep{Method: "GET", Path: "/api/v10/guilds/1/channels", Status: 200, Body: `[]`})
	defer fake.Close()
	client, err := NewWithRoundTripper(staticToken("canary-token"), fake.RoundTripper())
	if err != nil {
		t.Fatal(err)
	}
	result, err := client.ExecuteDiscord(context.Background(), domain.ListGuildChannels{CommandContext: domain.CommandContext{GuildID: "1"}})
	if err != nil {
		t.Fatal(err)
	}
	if result.Attempts != 2 {
		t.Fatalf("got %d attempts", result.Attempts)
	}
	requests := fake.Requests()
	if len(requests) != 2 {
		t.Fatalf("got %d requests", len(requests))
	}
	for _, request := range requests {
		for _, value := range request.Headers {
			if strings.Contains(value, "canary-token") {
				t.Fatal("token retained in capture")
			}
		}
	}
	if failures := fake.Failures(); len(failures) > 0 {
		t.Fatal(failures)
	}
}
func TestDiscordErrorsDoNotRetainBodyText(t *testing.T) {
	err := decodeDiscordError(400, []byte(`{"code":50035,"message":"canary-secret","errors":{"name":{"_errors":[{"code":"BAD","message":"canary-content"}]}}}`))
	if strings.Contains(fmt.Sprintf("%#v", err), "canary") {
		t.Fatalf("error retained body text: %#v", err)
	}
}
func TestQueuedRateLimitWaitCancels(t *testing.T) {
	coordinator := newRateLimitCoordinator(realClock{})
	first, err := coordinator.Acquire(context.Background(), "GET", "/channels/{channel.id}", "1")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()
	_, err = coordinator.Acquire(ctx, "GET", "/channels/{channel.id}", "1")
	var deadline interface{ Timeout() bool }
	if !errors.As(err, &deadline) {
		t.Fatalf("expected cancellation, got %v", err)
	}
	first.Release()
}
func TestResetUsesServerDateForClockSkew(t *testing.T) {
	now := time.Unix(1000, 0)
	headers := http.Header{"Date": {time.Unix(900, 0).UTC().Format(http.TimeFormat)}, "X-Ratelimit-Reset": {"905"}}
	deadline, ok, err := parseReset(headers, now)
	if err != nil || !ok || !deadline.Equal(now.Add(5*time.Second)) {
		t.Fatalf("deadline=%v ok=%v err=%v", deadline, ok, err)
	}
}
func TestBucketHashRemappingGlobalScopeAndMajorIsolation(t *testing.T) {
	coordinator := newRateLimitCoordinator(realClock{})
	headers := make(http.Header)
	headers.Set("X-RateLimit-Bucket", "shared-hash")
	headers.Set("X-RateLimit-Limit", "2")
	headers.Set("X-RateLimit-Remaining", "1")
	headers.Set("X-RateLimit-Reset-After", "0.01")
	headers.Set("X-RateLimit-Scope", "shared")
	first, _ := coordinator.Acquire(context.Background(), "GET", "/channels/{channel.id}", "1")
	if err := first.Update(headers, 200); err != nil {
		t.Fatal(err)
	}
	first.Release()
	second, _ := coordinator.Acquire(context.Background(), "POST", "/channels/{channel.id}/messages", "1")
	if err := second.Update(headers, 200); err != nil {
		t.Fatal(err)
	}
	second.Release()
	coordinator.mu.Lock()
	sameMajorMerged := coordinator.routes["GET /channels/{channel.id} 1"] == coordinator.routes["POST /channels/{channel.id}/messages 1"]
	coordinator.mu.Unlock()
	if !sameMajorMerged {
		t.Fatal("same hash and major were not remapped to one bucket")
	}
	third, _ := coordinator.Acquire(context.Background(), "GET", "/channels/{channel.id}", "2")
	if err := third.Update(headers, 200); err != nil {
		t.Fatal(err)
	}
	third.Release()
	coordinator.mu.Lock()
	majorsIsolated := coordinator.routes["GET /channels/{channel.id} 1"] != coordinator.routes["GET /channels/{channel.id} 2"]
	coordinator.mu.Unlock()
	if !majorsIsolated {
		t.Fatal("different major resources shared a bucket")
	}
	global, _ := coordinator.Acquire(context.Background(), "GET", "/guilds/{guild.id}", "3")
	globalHeaders := make(http.Header)
	globalHeaders.Set("Retry-After", "0.01")
	globalHeaders.Set("X-RateLimit-Scope", "global")
	if err := global.Update(globalHeaders, 429); err != nil {
		t.Fatal(err)
	}
	global.Release()
	coordinator.mu.Lock()
	globalSet := coordinator.global.After(time.Now())
	coordinator.mu.Unlock()
	if !globalSet {
		t.Fatal("global deadline was not applied")
	}
}

func TestTransientFailureRetriesAndExhaustion(t *testing.T) {
	fake := testsupport.NewScriptedDiscord(testsupport.DiscordStep{ForceEOF: true}, testsupport.DiscordStep{Status: 200, Body: `[]`})
	defer fake.Close()
	client, _ := NewWithRoundTripper(staticToken("token"), fake.RoundTripper())
	result, err := client.ExecuteDiscord(context.Background(), domain.ListGuildChannels{CommandContext: domain.CommandContext{GuildID: "1"}})
	if err != nil || result.Attempts != 2 {
		t.Fatalf("retry result=%#v err=%v", result, err)
	}
	exhaustedFake := testsupport.NewScriptedDiscord(testsupport.DiscordStep{Status: 500, Body: `{"code":0}`}, testsupport.DiscordStep{Status: 500, Body: `{"code":0}`}, testsupport.DiscordStep{Status: 500, Body: `{"code":0}`})
	defer exhaustedFake.Close()
	exhaustedClient, _ := NewWithRoundTripper(staticToken("token"), exhaustedFake.RoundTripper())
	_, err = exhaustedClient.ExecuteDiscord(context.Background(), domain.ListGuildChannels{CommandContext: domain.CommandContext{GuildID: "1"}})
	var exhausted *domain.RetryExhaustedError
	if !errors.As(err, &exhausted) || exhausted.Attempts != 3 {
		t.Fatalf("expected retry exhaustion, got %v", err)
	}
}

func TestMalformedOversizedAndMultipartLimits(t *testing.T) {
	if _, err := decodeResult(domain.ListGuildChannels{}, []byte(`[] {}`)); err == nil {
		t.Fatal("trailing JSON accepted")
	}
	if _, err := readBounded(bytes.NewReader(bytes.Repeat([]byte("x"), 12)), 10); err == nil {
		t.Fatal("oversized body accepted")
	}
	reader, _, err := encodeMultipart(domain.CreateMessage{CommandContext: domain.CommandContext{GuildID: "1", IdempotencyKey: "i", ReconciliationKey: "r"}, Content: stringPointer("x")}, []domain.Upload{{Filename: "../bad.txt", Size: 2, Reader: strings.NewReader("x")}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = io.ReadAll(reader); err == nil {
		t.Fatal("attachment size mismatch accepted")
	}
}
func stringPointer(value string) *string { return &value }

func FuzzParseRetryAfter(f *testing.F) {
	for _, seed := range []string{"0", "0.25", "-1", "Fri, 21 Aug 2026 10:00:00 GMT", "bad"} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, value string) { _, _, _ = parseRetryAfter(value, time.Unix(0, 0)) })
}
