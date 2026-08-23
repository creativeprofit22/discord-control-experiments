package application

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/ken-kai/discord-control-experiments/internal/domain"
)

type registrationRegistry struct {
	current    []ApplicationCommandDefinition
	overwrites int
}

func (r *registrationRegistry) ListApplicationCommands(context.Context, CommandRegistrationScope) ([]ApplicationCommandDefinition, error) {
	return r.current, nil
}
func (r *registrationRegistry) BulkOverwriteApplicationCommands(_ context.Context, _ CommandRegistrationScope, manifest []ApplicationCommandDefinition) ([]ApplicationCommandDefinition, error) {
	r.overwrites++
	r.current = manifest
	return manifest, nil
}

type registrationAudit struct {
	fail   bool
	events []domain.AuditEvent
}

func (a *registrationAudit) Append(_ context.Context, event domain.AuditEvent) error {
	if a.fail {
		return errors.New("audit unavailable")
	}
	a.events = append(a.events, event)
	return nil
}

type registrationClock struct{ now time.Time }

func (c registrationClock) Now() time.Time { return c.now }

func TestCommandRegistrationNoopDriftAndAuditGate(t *testing.T) {
	manifest := []ApplicationCommandDefinition{{Type: 1, Name: "discord-control", Description: "control", Options: []ApplicationCommandOptionDefinition{}, IntegrationTypes: []int{0}, Contexts: []int{0}}}
	scope := CommandRegistrationScope{Kind: "guild", ApplicationID: "123", GuildID: "456"}
	clock := registrationClock{now: time.Unix(1_800_000_000, 0)}
	registry := &registrationRegistry{current: manifest}
	audit := &registrationAudit{}
	service, err := NewCommandRegistrationService(registry, audit, clock)
	if err != nil {
		t.Fatal(err)
	}
	if err := service.Reconcile(context.Background(), scope, manifest, "correlation"); err != nil || registry.overwrites != 0 {
		t.Fatalf("no-op = %v overwrites=%d", err, registry.overwrites)
	}
	registry.current = nil
	if err := service.Reconcile(context.Background(), scope, manifest, "correlation"); err != nil || registry.overwrites != 1 || len(audit.events) != 2 {
		t.Fatalf("drift = %v overwrites=%d audit=%d", err, registry.overwrites, len(audit.events))
	}
	registry.current = nil
	audit.fail = true
	if err := service.Reconcile(context.Background(), scope, manifest, "correlation"); err == nil || registry.overwrites != 1 {
		t.Fatalf("audit gate = %v overwrites=%d", err, registry.overwrites)
	}
}
