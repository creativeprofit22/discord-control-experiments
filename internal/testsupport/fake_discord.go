package testsupport

import (
	"context"
	"errors"
	"sync"

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
