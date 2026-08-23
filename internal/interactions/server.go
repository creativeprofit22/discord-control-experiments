package interactions

import (
	"context"
	"crypto/sha256"
	"crypto/tls"
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/ken-kai/discord-control-experiments/internal/domain"
)

type InitialResponder interface {
	Respond(context.Context, VerifiedInteraction) ([]byte, error)
}
type InteractionTracer interface {
	StartInteractionSpan(context.Context, string, domain.CorrelationID) (context.Context, func(string))
}

type InteractionTelemetry interface {
	InteractionTracer
	RecordVerification(context.Context, string, int)
	ObserveAcknowledgement(context.Context, string, time.Duration, bool)
	RecordDuplicate(context.Context, string)
	ObserveDeferred(context.Context, time.Duration, string)
	RecordCallback(context.Context, string)
	RecordApproval(context.Context, string)
	ObserveQueueDepth(context.Context, int)
	ObserveHandlerActive(context.Context, int)
	RecordSaturation(context.Context)
}
type noopInteractionTelemetry struct{}

func (noopInteractionTelemetry) StartInteractionSpan(ctx context.Context, _ string, _ domain.CorrelationID) (context.Context, func(string)) {
	return ctx, func(string) {}
}
func (noopInteractionTelemetry) RecordVerification(context.Context, string, int) {}
func (noopInteractionTelemetry) ObserveAcknowledgement(context.Context, string, time.Duration, bool) {
}
func (noopInteractionTelemetry) RecordDuplicate(context.Context, string)                {}
func (noopInteractionTelemetry) ObserveDeferred(context.Context, time.Duration, string) {}
func (noopInteractionTelemetry) RecordCallback(context.Context, string)                 {}
func (noopInteractionTelemetry) RecordApproval(context.Context, string)                 {}
func (noopInteractionTelemetry) ObserveQueueDepth(context.Context, int)                 {}
func (noopInteractionTelemetry) ObserveHandlerActive(context.Context, int)              {}
func (noopInteractionTelemetry) RecordSaturation(context.Context)                       {}

type ServerOptions struct {
	ListenAddress         string
	Path                  string
	ApplicationID         string
	Verifier              *Verifier
	Responder             InitialResponder
	MaxBodyBytes          int64
	MaxConcurrentRequests int
	AckDeadline           time.Duration
	TLSMode               string
	TLSCertificate        *tls.Certificate
	TrustedProxyCIDRs     []*net.IPNet
	Telemetry             InteractionTelemetry
	CorrelationID         func() (domain.CorrelationID, error)
}

type Server struct {
	options  ServerOptions
	http     *http.Server
	sem      chan struct{}
	ready    atomic.Bool
	done     chan error
	mu       sync.Mutex
	listener net.Listener
}

func NewServer(options ServerOptions) (*Server, error) {
	if options.ListenAddress == "" || options.Path != "/interactions" || !snowflakePattern.MatchString(options.ApplicationID) || options.Verifier == nil || options.Responder == nil || options.CorrelationID == nil || options.MaxBodyBytes < 1024 || options.MaxConcurrentRequests < 1 || options.AckDeadline <= 0 || options.AckDeadline >= 3*time.Second {
		return nil, errors.New("invalid interaction server options")
	}
	if options.TLSMode == "direct" && options.TLSCertificate == nil {
		return nil, errors.New("direct TLS certificate is required")
	}
	if options.TLSMode == "trusted_proxy" && len(options.TrustedProxyCIDRs) == 0 {
		return nil, errors.New("trusted proxy CIDRs are required")
	}
	if options.TLSMode != "direct" && options.TLSMode != "trusted_proxy" {
		return nil, errors.New("invalid interaction TLS mode")
	}
	if options.Telemetry == nil {
		options.Telemetry = noopInteractionTelemetry{}
	}
	s := &Server{options: options, sem: make(chan struct{}, options.MaxConcurrentRequests), done: make(chan error, 1)}
	s.http = &http.Server{Addr: options.ListenAddress, Handler: s, ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 10 * time.Second, WriteTimeout: 5 * time.Second, IdleTimeout: 30 * time.Second, MaxHeaderBytes: 16 << 10}
	return s, nil
}

func (s *Server) Name() domain.Adapter { return domain.Adapter("interactions") }

func (s *Server) Start(context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.listener != nil {
		return errors.New("interactions server already started")
	}
	listener, err := net.Listen("tcp", s.options.ListenAddress)
	if err != nil {
		return err
	}
	if s.options.TLSMode == "direct" {
		listener = tls.NewListener(listener, &tls.Config{MinVersion: tls.VersionTLS13, Certificates: []tls.Certificate{*s.options.TLSCertificate}})
	}
	s.listener = listener
	s.ready.Store(true)
	go func() {
		err := s.http.Serve(listener)
		if errors.Is(err, http.ErrServerClosed) {
			err = nil
		}
		s.done <- err
	}()
	return nil
}

func (s *Server) Stop(ctx context.Context) error { s.ready.Store(false); return s.http.Shutdown(ctx) }
func (s *Server) Done() <-chan error             { return s.done }
func (s *Server) SetReady(value bool)            { s.ready.Store(value) }

func (s *Server) ServeHTTP(writer http.ResponseWriter, request *http.Request) {
	started := time.Now()
	interactionType := InteractionType(0)
	committed := false
	defer func() {
		if recover() != nil && !committed {
			fixedText(writer, http.StatusInternalServerError, "unavailable")
		}
		if interactionType != 0 {
			s.options.Telemetry.ObserveAcknowledgement(request.Context(), interactionTypeName(interactionType), time.Since(started), time.Since(started) > s.options.AckDeadline)
		}
	}()
	writer.Header().Set("X-Content-Type-Options", "nosniff")
	if request.Method == http.MethodGet && request.URL.Path == "/livez" {
		fixedText(writer, http.StatusOK, "ok")
		return
	}
	if request.Method == http.MethodGet && request.URL.Path == "/readyz" {
		if s.ready.Load() {
			fixedText(writer, http.StatusOK, "ok")
		} else {
			fixedText(writer, http.StatusServiceUnavailable, "unavailable")
		}
		return
	}
	if request.Method != http.MethodPost || request.URL.Path != s.options.Path {
		fixedText(writer, http.StatusNotFound, "not found")
		return
	}
	if !s.transportAllowed(request) {
		fixedText(writer, http.StatusUnauthorized, "unauthorized")
		return
	}
	mediaType, _, err := mime.ParseMediaType(request.Header.Get("Content-Type"))
	if err != nil || mediaType != "application/json" || !validHeaders(request.Header) {
		fixedText(writer, http.StatusBadRequest, "invalid request")
		return
	}
	select {
	case s.sem <- struct{}{}:
		s.options.Telemetry.ObserveHandlerActive(request.Context(), len(s.sem))
		defer func() { <-s.sem; s.options.Telemetry.ObserveHandlerActive(request.Context(), len(s.sem)) }()
	default:
		s.options.Telemetry.RecordSaturation(request.Context())
		fixedJSON(writer, http.StatusServiceUnavailable, genericBusyCallback())
		committed = true
		return
	}
	body, err := io.ReadAll(http.MaxBytesReader(writer, request.Body, s.options.MaxBodyBytes))
	if err != nil {
		fixedText(writer, http.StatusBadRequest, "invalid request")
		return
	}
	keySlot, outcome, err := s.options.Verifier.Verify(request.Header.Get("X-Signature-Timestamp"), request.Header.Get("X-Signature-Ed25519"), body)
	s.options.Telemetry.RecordVerification(request.Context(), string(outcome), keySlot)
	if err != nil {
		fixedText(writer, http.StatusUnauthorized, "unauthorized")
		return
	}
	interaction, err := DecodeVerified(body, s.options.ApplicationID)
	if err != nil {
		fixedText(writer, http.StatusBadRequest, "invalid request")
		return
	}
	interaction.RawBodyDigest = sha256.Sum256(body)
	interaction.KeySlot = keySlot
	interactionType = interaction.Envelope.Type
	correlationID, err := s.options.CorrelationID()
	if err != nil {
		fixedJSON(writer, http.StatusOK, genericUnavailableCallback())
		committed = true
		return
	}
	traceContext, endSpan := s.options.Telemetry.StartInteractionSpan(request.Context(), "inbound", correlationID)
	spanOutcome := "ok"
	defer func() { endSpan(spanOutcome) }()
	interaction.CorrelationID = correlationID
	interaction.TraceContext = context.WithoutCancel(traceContext)
	callback, err := s.options.Responder.Respond(traceContext, interaction)
	if err != nil || len(callback) == 0 || len(callback) > 64<<10 || !json.Valid(callback) {
		callback = genericUnavailableCallback()
		spanOutcome = "error"
	}
	fixedJSON(writer, http.StatusOK, callback)
	committed = true
}

func (s *Server) transportAllowed(request *http.Request) bool {
	if s.options.TLSMode == "direct" {
		return request.TLS != nil
	}
	host, _, err := net.SplitHostPort(request.RemoteAddr)
	if err != nil {
		return false
	}
	ip := net.ParseIP(host)
	if ip == nil {
		return false
	}
	trusted := false
	for _, network := range s.options.TrustedProxyCIDRs {
		if network.Contains(ip) {
			trusted = true
			break
		}
	}
	if !trusted {
		return false
	}
	forwarded := request.Header.Get("X-Forwarded-Proto")
	return forwarded == "https" && !strings.Contains(forwarded, ",")
}

func interactionTypeName(value InteractionType) string {
	switch value {
	case InteractionPing:
		return "ping"
	case InteractionApplicationCommand:
		return "command"
	case InteractionMessageComponent:
		return "component"
	case InteractionAutocomplete:
		return "autocomplete"
	case InteractionModalSubmit:
		return "modal"
	default:
		return "unsupported"
	}
}

func validHeaders(headers http.Header) bool {
	if len(headers.Values("X-Signature-Ed25519")) != 1 || len(headers.Values("X-Signature-Timestamp")) != 1 {
		return false
	}
	if len(headers) > 32 {
		return false
	}
	for name, values := range headers {
		if len(name) > 64 || len(values) > 8 {
			return false
		}
		for _, value := range values {
			if len(value) > 4096 {
				return false
			}
		}
	}
	return true
}
func fixedText(writer http.ResponseWriter, status int, body string) {
	writer.Header().Set("Content-Type", "text/plain; charset=utf-8")
	writer.WriteHeader(status)
	_, _ = io.WriteString(writer, body)
}
func fixedJSON(writer http.ResponseWriter, status int, body []byte) {
	writer.Header().Set("Content-Type", "application/json")
	writer.WriteHeader(status)
	_, _ = writer.Write(body)
}
func genericBusyCallback() []byte {
	return []byte(`{"type":4,"data":{"content":"Service is busy. Try again later.","flags":64}}`)
}
func genericUnavailableCallback() []byte {
	return []byte(`{"type":4,"data":{"content":"This operation is not available.","flags":64}}`)
}
