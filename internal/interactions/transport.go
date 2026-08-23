package interactions

import (
	"context"
	"errors"

	"github.com/ken-kai/discord-control-experiments/internal/application"
	"github.com/ken-kai/discord-control-experiments/internal/domain"
)

type Transport struct {
	server       *Server
	executor     *Executor
	registration *application.CommandRegistrationService
	scope        application.CommandRegistrationScope
	correlate    func() (domain.CorrelationID, error)
}

func NewTransport(server *Server, executor *Executor, registration *application.CommandRegistrationService, scope application.CommandRegistrationScope, correlate func() (domain.CorrelationID, error)) (*Transport, error) {
	if server == nil || executor == nil || registration == nil || correlate == nil {
		return nil, errors.New("interaction transport dependencies are required")
	}
	server.SetReady(false)
	return &Transport{server: server, executor: executor, registration: registration, scope: scope, correlate: correlate}, nil
}
func (t *Transport) Name() domain.Adapter { return "interactions" }
func (t *Transport) Start(ctx context.Context) error {
	correlation, err := t.correlate()
	if err != nil {
		return err
	}
	if err := t.executor.Start(ctx); err != nil {
		return err
	}
	started := false
	cleanups := []func(){t.executor.Stop}
	defer func() {
		if started {
			return
		}
		for index := len(cleanups) - 1; index >= 0; index-- {
			cleanups[index]()
		}
	}()
	if err := t.registration.Reconcile(ctx, t.scope, ApplicationManifest(), correlation); err != nil {
		return err
	}
	if err := t.server.Start(ctx); err != nil {
		return err
	}
	cleanups = append(cleanups, func() { _ = t.server.Stop(context.WithoutCancel(ctx)) })
	started = true
	return nil
}
func (t *Transport) Stop(ctx context.Context) error {
	serverErr := t.server.Stop(ctx)
	t.executor.Stop()
	return serverErr
}
func (t *Transport) Done() <-chan error { return t.server.Done() }
