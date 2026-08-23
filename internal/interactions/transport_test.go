package interactions

import (
	"context"
	"errors"
	"net"
	"testing"
	"time"

	"github.com/ken-kai/discord-control-experiments/internal/application"
	"github.com/ken-kai/discord-control-experiments/internal/domain"
)

type transportRegistry struct {
	listErr error
}

func (r *transportRegistry) ListApplicationCommands(context.Context, application.CommandRegistrationScope) ([]application.ApplicationCommandDefinition, error) {
	if r.listErr != nil {
		return nil, r.listErr
	}
	return ApplicationManifest(), nil
}

func (*transportRegistry) BulkOverwriteApplicationCommands(context.Context, application.CommandRegistrationScope, []application.ApplicationCommandDefinition) ([]application.ApplicationCommandDefinition, error) {
	return nil, errors.New("unexpected command overwrite")
}

type transportAudit struct{}

func (transportAudit) Append(context.Context, domain.AuditEvent) error { return nil }

type transportClock struct{}

func (transportClock) Now() time.Time { return time.Unix(1_800_000_000, 0) }

func TestTransportStartRollsBackExecutorAfterRegistrationFailure(t *testing.T) {
	registrationErr := errors.New("registration unavailable")
	registry := &transportRegistry{listErr: registrationErr}
	transport := newTestTransport(t, registry)

	if err := transport.Start(context.Background()); !errors.Is(err, registrationErr) {
		t.Fatalf("start error = %v, want registration error", err)
	}
	assertExecutorStopped(t, transport.executor)
	if transport.server.ready.Load() {
		t.Fatal("registration failure exposed interactions readiness")
	}

	registry.listErr = nil
	if err := transport.Start(context.Background()); err != nil {
		t.Fatalf("retry start: %v", err)
	}
	if !transport.server.ready.Load() {
		t.Fatal("successful registration and listener startup did not expose readiness")
	}
	if err := transport.Stop(context.Background()); err != nil {
		t.Fatalf("stop after retry: %v", err)
	}
	if transport.server.ready.Load() {
		t.Fatal("stopped interactions server remained ready")
	}
}

func TestTransportStartRollsBackExecutorAfterServerFailure(t *testing.T) {
	occupied, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	registry := &transportRegistry{}
	transport := newTestTransport(t, registry)
	transport.server.options.ListenAddress = occupied.Addr().String()

	startupErr := transport.Start(context.Background())
	if startupErr == nil {
		t.Fatal("start unexpectedly succeeded on occupied address")
	}
	assertExecutorStopped(t, transport.executor)
	if transport.server.ready.Load() {
		t.Fatal("listener startup failure exposed interactions readiness")
	}

	if err := occupied.Close(); err != nil {
		t.Fatal(err)
	}
	if err := transport.Start(context.Background()); err != nil {
		t.Fatalf("retry start after %v: %v", startupErr, err)
	}
	if err := transport.Stop(context.Background()); err != nil {
		t.Fatalf("stop after retry: %v", err)
	}
}

func newTestTransport(t *testing.T, registry application.ApplicationCommandRegistry) *Transport {
	t.Helper()
	registration, err := application.NewCommandRegistrationService(registry, transportAudit{}, transportClock{})
	if err != nil {
		t.Fatal(err)
	}
	executor, err := NewExecutor(1, 1, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	server, _, _ := testServer(t, time.Now())
	transport, err := NewTransport(
		server,
		executor,
		registration,
		application.CommandRegistrationScope{Kind: "guild", ApplicationID: "123456789012345678", GuildID: "234567890123456789"},
		func() (domain.CorrelationID, error) { return "transport-test", nil },
	)
	if err != nil {
		t.Fatal(err)
	}
	return transport
}

func assertExecutorStopped(t *testing.T, executor *Executor) {
	t.Helper()
	if executor.Enqueue(WorkItem{InteractionID: "probe", Run: func(context.Context) {}}) {
		t.Fatal("executor accepted work after failed transport start")
	}
}
