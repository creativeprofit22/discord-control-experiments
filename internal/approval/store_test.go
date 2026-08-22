package approval

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ken-kai/discord-control-experiments/internal/domain"
)

func issueGrant(t *testing.T, store *Store, now time.Time, ttl time.Duration) domain.ApprovalGrant {
	t.Helper()
	grant, err := store.Issue(context.Background(), domain.ApprovalGrant{ActorID: "actor-1", ApproverID: "human-1", OperationHash: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", IssuedAt: now, ExpiresAt: now.Add(ttl)})
	if err != nil {
		t.Fatal(err)
	}
	return grant
}

func TestApprovalExpiryReplayAndBinding(t *testing.T) {
	now := time.Date(2026, 8, 21, 12, 0, 0, 0, time.UTC)
	store, err := NewStoreWithDependencies(time.Hour, func() time.Time { return now }, zeroReader{})
	if err != nil {
		t.Fatal(err)
	}
	grant := issueGrant(t, store, now, time.Minute)
	if _, err := store.Consume(context.Background(), grant.ID, "other", grant.OperationHash, now); !errors.Is(err, ErrMismatch) {
		t.Fatalf("actor mismatch: %v", err)
	}
	if _, err := store.Consume(context.Background(), grant.ID, grant.ActorID, "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb", now); !errors.Is(err, ErrMismatch) {
		t.Fatalf("hash mismatch: %v", err)
	}
	if _, err := store.Consume(context.Background(), grant.ID, grant.ActorID, grant.OperationHash, now); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Consume(context.Background(), grant.ID, grant.ActorID, grant.OperationHash, now); !errors.Is(err, ErrConsumed) {
		t.Fatalf("replay: %v", err)
	}

	expiringStore, _ := NewStoreWithDependencies(time.Hour, func() time.Time { return now }, zeroReader{})
	expiring := issueGrant(t, expiringStore, now, time.Minute)
	if _, err := expiringStore.Consume(context.Background(), expiring.ID, expiring.ActorID, expiring.OperationHash, now.Add(time.Minute)); !errors.Is(err, ErrExpired) {
		t.Fatalf("expiry boundary: %v", err)
	}
}

func TestApprovalMaximumTTLAndRestartInvalidation(t *testing.T) {
	now := time.Now().UTC()
	store, _ := NewStoreWithDependencies(time.Minute, func() time.Time { return now }, zeroReader{})
	if _, err := store.Issue(context.Background(), domain.ApprovalGrant{ActorID: "a", ApproverID: "h", OperationHash: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", IssuedAt: now, ExpiresAt: now.Add(time.Minute + time.Nanosecond)}); !errors.Is(err, ErrInvalidGrant) {
		t.Fatalf("maximum TTL: %v", err)
	}
	grant := issueGrant(t, store, now, time.Minute)
	restarted, _ := NewStoreWithDependencies(time.Minute, func() time.Time { return now }, zeroReader{})
	if _, err := restarted.Consume(context.Background(), grant.ID, grant.ActorID, grant.OperationHash, now); !errors.Is(err, ErrUnknown) {
		t.Fatalf("restart should invalidate: %v", err)
	}
}

func TestApprovalConcurrentConsumeIsAtomic(t *testing.T) {
	now := time.Now().UTC()
	store, _ := NewStoreWithDependencies(time.Hour, func() time.Time { return now }, zeroReader{})
	grant := issueGrant(t, store, now, time.Minute)
	var successes atomic.Int32
	var wg sync.WaitGroup
	for range 32 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := store.Consume(context.Background(), grant.ID, grant.ActorID, grant.OperationHash, now); err == nil {
				successes.Add(1)
			}
		}()
	}
	wg.Wait()
	if successes.Load() != 1 {
		t.Fatalf("successful consumes = %d", successes.Load())
	}
}

type zeroReader struct{}

func (zeroReader) Read(p []byte) (int, error) {
	for i := range p {
		p[i] = byte(i + 1)
	}
	return len(p), nil
}
