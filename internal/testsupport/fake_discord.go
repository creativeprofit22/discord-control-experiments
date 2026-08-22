package testsupport

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/ken-kai/discord-control-experiments/internal/domain"
)

type FakeTransport struct {
	Adapter    domain.Adapter
	FailStarts int
	Failure    error
	Started    chan struct{}
	mu         sync.Mutex
	StartCalls int
	StopCalls  int
}

func NewFakeTransport(adapter domain.Adapter) *FakeTransport {
	return &FakeTransport{Adapter: adapter, Failure: errors.New("fake start failure"), Started: make(chan struct{})}
}
func (f *FakeTransport) Name() domain.Adapter { return f.Adapter }
func (f *FakeTransport) Start(context.Context) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.StartCalls++
	if f.StartCalls <= f.FailStarts {
		return f.Failure
	}
	select {
	case <-f.Started:
	default:
		close(f.Started)
	}
	return nil
}
func (f *FakeTransport) Stop(context.Context) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.StopCalls++
	return nil
}
func (f *FakeTransport) Calls() (int, int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.StartCalls, f.StopCalls
}

const MaxCapturedRequestBytes int64 = 2 << 20

type DiscordStep struct {
	Method          string
	Path            string
	Headers         map[string]string
	BodyCheck       func([]byte) error
	Status          int
	ResponseHeaders map[string]string
	Body            string
	Delay           time.Duration
	Chunks          []string
	ForceEOF        bool
}
type CapturedDiscordRequest struct {
	Method, Path string
	Headers      map[string]string
	BodyLength   int
	BodySHA256   [32]byte
}
type ScriptedDiscord struct {
	Server   *httptest.Server
	mu       sync.Mutex
	steps    []DiscordStep
	requests []CapturedDiscordRequest
	failures []error
}

func NewScriptedDiscord(steps ...DiscordStep) *ScriptedDiscord {
	fake := &ScriptedDiscord{steps: append([]DiscordStep(nil), steps...)}
	fake.Server = httptest.NewServer(http.HandlerFunc(fake.serve))
	return fake
}
func (f *ScriptedDiscord) Close() { f.Server.Close() }
func (f *ScriptedDiscord) RoundTripper() http.RoundTripper {
	target, _ := url.Parse(f.Server.URL)
	return redirectRoundTripper{target: target, base: http.DefaultTransport}
}
func (f *ScriptedDiscord) Requests() []CapturedDiscordRequest {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]CapturedDiscordRequest(nil), f.requests...)
}
func (f *ScriptedDiscord) Failures() []error {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]error(nil), f.failures...)
}
func (f *ScriptedDiscord) Pending() int { f.mu.Lock(); defer f.mu.Unlock(); return len(f.steps) }
func (f *ScriptedDiscord) serve(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(io.LimitReader(r.Body, MaxCapturedRequestBytes+1))
	if err != nil {
		f.fail(fmt.Errorf("read request: %w", err))
		w.WriteHeader(500)
		return
	}
	if int64(len(body)) > MaxCapturedRequestBytes {
		f.fail(errors.New("request capture exceeded bound"))
		w.WriteHeader(413)
		return
	}
	capture := CapturedDiscordRequest{Method: r.Method, Path: r.URL.RequestURI(), Headers: redactedHeaders(r.Header), BodyLength: len(body), BodySHA256: sha256.Sum256(body)}
	f.mu.Lock()
	f.requests = append(f.requests, capture)
	if len(f.steps) == 0 {
		f.failures = append(f.failures, errors.New("unexpected Discord request"))
		f.mu.Unlock()
		w.WriteHeader(500)
		return
	}
	step := f.steps[0]
	f.steps = f.steps[1:]
	f.mu.Unlock()
	if step.Method != "" && step.Method != r.Method {
		f.fail(fmt.Errorf("method: got %s want %s", r.Method, step.Method))
	}
	if step.Path != "" && step.Path != r.URL.RequestURI() {
		f.fail(fmt.Errorf("path: got %s want %s", r.URL.RequestURI(), step.Path))
	}
	for key, want := range step.Headers {
		if got := r.Header.Get(key); got != want {
			f.fail(fmt.Errorf("header %s mismatch", key))
		}
	}
	if step.BodyCheck != nil {
		if err := step.BodyCheck(body); err != nil {
			f.fail(err)
		}
	}
	if step.Delay > 0 {
		select {
		case <-r.Context().Done():
			return
		case <-time.After(step.Delay):
		}
	}
	if step.ForceEOF {
		hijacker, ok := w.(http.Hijacker)
		if !ok {
			f.fail(errors.New("hijacking unsupported"))
			return
		}
		connection, _, err := hijacker.Hijack()
		if err == nil {
			_ = connection.Close()
		}
		return
	}
	for key, value := range step.ResponseHeaders {
		w.Header().Set(key, value)
	}
	status := step.Status
	if status == 0 {
		status = 200
	}
	w.WriteHeader(status)
	if len(step.Chunks) > 0 {
		flusher, _ := w.(http.Flusher)
		for _, chunk := range step.Chunks {
			_, _ = io.WriteString(w, chunk)
			if flusher != nil {
				flusher.Flush()
			}
		}
	} else {
		_, _ = io.WriteString(w, step.Body)
	}
}
func (f *ScriptedDiscord) fail(err error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.failures = append(f.failures, err)
}
func redactedHeaders(headers http.Header) map[string]string {
	safe := map[string]string{}
	for key, values := range headers {
		lower := strings.ToLower(key)
		if lower == "authorization" || lower == "cookie" || lower == "x-audit-log-reason" {
			safe[key] = "[REDACTED]"
			continue
		}
		safe[key] = strings.Join(values, ",")
	}
	return safe
}

type redirectRoundTripper struct {
	target *url.URL
	base   http.RoundTripper
}

func (r redirectRoundTripper) RoundTrip(request *http.Request) (*http.Response, error) {
	clone := request.Clone(request.Context())
	copied := *request.URL
	copied.Scheme = r.target.Scheme
	copied.Host = r.target.Host
	clone.URL = &copied
	clone.Host = r.target.Host
	return r.base.RoundTrip(clone)
}
