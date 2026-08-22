package application

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/ken-kai/discord-control-experiments/internal/domain"
)

type IdempotencyState string

const (
	IdempotencyPending   IdempotencyState = "pending"
	IdempotencySucceeded IdempotencyState = "succeeded"
	IdempotencyUncertain IdempotencyState = "uncertain"
	IdempotencyFailed    IdempotencyState = "failed"
)

type IdempotencyRecord struct {
	Namespace         string
	Fingerprint       string
	ReconciliationKey domain.ReconciliationKey
	State             IdempotencyState
	Result            any
	Baseline          []domain.Snowflake
	Attempts          int
	Partial           *domain.PartialResult
	LeaseUntil        time.Time
	Version           uint64
}

type IdempotencyStore interface {
	Load(context.Context, string) (IdempotencyRecord, bool, error)
	Create(context.Context, IdempotencyRecord) (bool, error)
	CompareAndSwap(context.Context, string, uint64, IdempotencyRecord) (bool, error)
	Wait(context.Context, string, uint64) (IdempotencyRecord, error)
}

type InMemoryIdempotencyStore struct {
	mu      sync.Mutex
	records map[string]IdempotencyRecord
	changed map[string]chan struct{}
}

func NewInMemoryIdempotencyStore() *InMemoryIdempotencyStore {
	return &InMemoryIdempotencyStore{records: make(map[string]IdempotencyRecord), changed: make(map[string]chan struct{})}
}
func (s *InMemoryIdempotencyStore) Load(_ context.Context, key string) (IdempotencyRecord, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	record, ok := s.records[key]
	return record, ok, nil
}
func (s *InMemoryIdempotencyStore) Create(_ context.Context, record IdempotencyRecord) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, exists := s.records[record.Namespace]; exists {
		return false, nil
	}
	record.Version = 1
	s.records[record.Namespace] = record
	s.changed[record.Namespace] = make(chan struct{})
	return true, nil
}
func (s *InMemoryIdempotencyStore) CompareAndSwap(_ context.Context, key string, version uint64, next IdempotencyRecord) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	current, ok := s.records[key]
	if !ok || current.Version != version {
		return false, nil
	}
	old := s.changed[key]
	next.Namespace = key
	next.Version = version + 1
	s.records[key] = next
	s.changed[key] = make(chan struct{})
	close(old)
	return true, nil
}
func (s *InMemoryIdempotencyStore) Wait(ctx context.Context, key string, version uint64) (IdempotencyRecord, error) {
	for {
		s.mu.Lock()
		current, ok := s.records[key]
		if !ok {
			s.mu.Unlock()
			return IdempotencyRecord{}, errors.New("idempotency record disappeared")
		}
		if current.Version != version {
			s.mu.Unlock()
			return current, nil
		}
		changed := s.changed[key]
		s.mu.Unlock()
		select {
		case <-ctx.Done():
			return IdempotencyRecord{}, ctx.Err()
		case <-changed:
		}
	}
}
