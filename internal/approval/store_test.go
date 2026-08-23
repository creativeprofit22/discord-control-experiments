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
	grant, err := store.Issue(context.Background(), domain.ApprovalGrant{ActorID: "actor-1", GuildID: "123", ScopeKind: domain.ApprovalScopeOperation, ScopeHash: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", ApproverID: "human-1", IssuedAt: now, ExpiresAt: now.Add(ttl)})
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
	if _, err := store.ConsumeOperation(context.Background(), grant.ID, "other", grant.GuildID, grant.ScopeHash, now); !errors.Is(err, ErrMismatch) {
		t.Fatalf("actor mismatch: %v", err)
	}
	if _, err := store.ConsumeOperation(context.Background(), grant.ID, grant.ActorID, grant.GuildID, "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb", now); !errors.Is(err, ErrMismatch) {
		t.Fatalf("hash mismatch: %v", err)
	}
	if _, err := store.ConsumeOperation(context.Background(), grant.ID, grant.ActorID, grant.GuildID, grant.ScopeHash, now); err != nil {
		t.Fatal(err)
	}
	if _, err := store.ConsumeOperation(context.Background(), grant.ID, grant.ActorID, grant.GuildID, grant.ScopeHash, now); !errors.Is(err, ErrConsumed) {
		t.Fatalf("replay: %v", err)
	}

	expiringStore, _ := NewStoreWithDependencies(time.Hour, func() time.Time { return now }, zeroReader{})
	expiring := issueGrant(t, expiringStore, now, time.Minute)
	if _, err := expiringStore.ConsumeOperation(context.Background(), expiring.ID, expiring.ActorID, expiring.GuildID, expiring.ScopeHash, now.Add(time.Minute)); !errors.Is(err, ErrExpired) {
		t.Fatalf("expiry boundary: %v", err)
	}
}

func TestApprovalMaximumTTLAndRestartInvalidation(t *testing.T) {
	now := time.Now().UTC()
	store, _ := NewStoreWithDependencies(time.Minute, func() time.Time { return now }, zeroReader{})
	if _, err := store.Issue(context.Background(), domain.ApprovalGrant{ActorID: "a", GuildID: "123", ScopeKind: domain.ApprovalScopeOperation, ScopeHash: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", ApproverID: "h", IssuedAt: now, ExpiresAt: now.Add(time.Minute + time.Nanosecond)}); !errors.Is(err, ErrInvalidGrant) {
		t.Fatalf("maximum TTL: %v", err)
	}
	grant := issueGrant(t, store, now, time.Minute)
	restarted, _ := NewStoreWithDependencies(time.Minute, func() time.Time { return now }, zeroReader{})
	if _, err := restarted.ConsumeOperation(context.Background(), grant.ID, grant.ActorID, grant.GuildID, grant.ScopeHash, now); !errors.Is(err, ErrUnknown) {
		t.Fatalf("restart should invalidate: %v", err)
	}
}

func TestApprovalCapacityRetainsExpiredClassificationForBoundedTTL(t *testing.T) {
	clock := newFakeClock(time.Date(2026, 8, 22, 12, 0, 0, 0, time.UTC))
	store, err := newStore(time.Minute, 2, clock.Now, &sequenceReader{})
	if err != nil {
		t.Fatal(err)
	}
	first := issueGrant(t, store, clock.Now(), time.Minute)
	second := issueGrant(t, store, clock.Now(), time.Minute)
	if _, err := store.Issue(context.Background(), grantRequest(clock.Now(), time.Minute)); !errors.Is(err, ErrCapacity) {
		t.Fatalf("full active store = %v", err)
	}

	clock.Advance(time.Minute)
	if _, err := store.Issue(context.Background(), grantRequest(clock.Now(), time.Minute)); !errors.Is(err, ErrCapacity) {
		t.Fatalf("expired tombstones must retain capacity = %v", err)
	}
	if len(store.grants) != 0 || len(store.tombstones) != 2 {
		t.Fatalf("after expiry: grants = %d, tombstones = %d", len(store.grants), len(store.tombstones))
	}
	if _, err := store.ConsumeOperation(context.Background(), first.ID, first.ActorID, first.GuildID, first.ScopeHash, clock.Now()); !errors.Is(err, ErrExpired) {
		t.Fatalf("retained expiry classification = %v", err)
	}

	clock.Advance(time.Minute)
	if _, err := store.Issue(context.Background(), grantRequest(clock.Now(), time.Minute)); err != nil {
		t.Fatalf("issue after tombstone TTL: %v", err)
	}
	if _, err := store.ConsumeOperation(context.Background(), second.ID, second.ActorID, second.GuildID, second.ScopeHash, clock.Now()); !errors.Is(err, ErrUnknown) {
		t.Fatalf("expired tombstone after retention = %v", err)
	}
}

func TestApprovalConsumedGrantBecomesBoundedTombstone(t *testing.T) {
	clock := newFakeClock(time.Date(2026, 8, 22, 12, 0, 0, 0, time.UTC))
	store, err := newStore(time.Minute, 1, clock.Now, &sequenceReader{})
	if err != nil {
		t.Fatal(err)
	}
	grant := issueGrant(t, store, clock.Now(), time.Minute)
	if _, err := store.ConsumeOperation(context.Background(), grant.ID, grant.ActorID, grant.GuildID, grant.ScopeHash, clock.Now()); err != nil {
		t.Fatal(err)
	}
	if len(store.grants) != 0 || len(store.tombstones) != 1 {
		t.Fatalf("after consume: grants = %d, tombstones = %d", len(store.grants), len(store.tombstones))
	}
	if _, err := store.ConsumeOperation(context.Background(), grant.ID, grant.ActorID, grant.GuildID, grant.ScopeHash, clock.Now()); !errors.Is(err, ErrConsumed) {
		t.Fatalf("retained replay classification = %v", err)
	}
	if _, err := store.Issue(context.Background(), grantRequest(clock.Now(), time.Minute)); !errors.Is(err, ErrCapacity) {
		t.Fatalf("consumed tombstone must retain capacity = %v", err)
	}

	clock.Advance(time.Minute)
	if _, err := store.Issue(context.Background(), grantRequest(clock.Now(), time.Minute)); err != nil {
		t.Fatalf("issue after replay tombstone TTL: %v", err)
	}
	if _, err := store.ConsumeOperation(context.Background(), grant.ID, grant.ActorID, grant.GuildID, grant.ScopeHash, clock.Now()); !errors.Is(err, ErrUnknown) {
		t.Fatalf("consumed tombstone after retention = %v", err)
	}
}

func TestApprovalConcurrentIssueNeverExceedsCapacity(t *testing.T) {
	clock := newFakeClock(time.Date(2026, 8, 22, 12, 0, 0, 0, time.UTC))
	const capacity = 8
	store, err := newStore(time.Minute, capacity, clock.Now, &sequenceReader{})
	if err != nil {
		t.Fatal(err)
	}
	var issued atomic.Int32
	var full atomic.Int32
	var wg sync.WaitGroup
	for range 64 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := store.Issue(context.Background(), grantRequest(clock.Now(), time.Minute))
			switch {
			case err == nil:
				issued.Add(1)
			case errors.Is(err, ErrCapacity):
				full.Add(1)
			default:
				t.Errorf("unexpected issue error: %v", err)
			}
		}()
	}
	wg.Wait()
	if issued.Load() != capacity || full.Load() != 64-capacity {
		t.Fatalf("issued = %d, full = %d", issued.Load(), full.Load())
	}
	if len(store.grants)+len(store.tombstones) != capacity {
		t.Fatalf("stored records = %d", len(store.grants)+len(store.tombstones))
	}
}

func TestPlanGrantBindsGuildKindHashAndMismatchDoesNotConsume(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	store, err := NewStoreWithDependencies(time.Minute, func() time.Time { return now }, zeroReader{})
	if err != nil {
		t.Fatal(err)
	}
	hash := "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	grant, err := store.Issue(context.Background(), domain.ApprovalGrant{ActorID: "actor", GuildID: "123", ScopeKind: domain.ApprovalScopeReconciliationPlan, ScopeHash: hash, ApproverID: "actor", IssuedAt: now, ExpiresAt: now.Add(time.Minute)})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.ConsumeReconciliationPlan(context.Background(), grant.ID, "actor", "other", hash, now); !errors.Is(err, ErrMismatch) {
		t.Fatalf("wrong guild = %v", err)
	}
	if _, err := store.ConsumeOperation(context.Background(), grant.ID, "actor", "123", hash, now); !errors.Is(err, ErrMismatch) {
		t.Fatalf("wrong kind = %v", err)
	}
	if _, err := store.ConsumeReconciliationPlan(context.Background(), grant.ID, "actor", "123", hash, now); err != nil {
		t.Fatal(err)
	}
	if _, err := store.ConsumeReconciliationPlan(context.Background(), grant.ID, "actor", "123", hash, now); !errors.Is(err, ErrConsumed) {
		t.Fatalf("replay = %v", err)
	}
}

func TestStoreConsumesSingleUseLockdownPlan(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	store, err := NewStoreWithDependencies(time.Minute, func() time.Time { return now }, zeroReader{})
	if err != nil {
		t.Fatal(err)
	}
	hash := "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	grant, err := store.Issue(context.Background(), domain.ApprovalGrant{ActorID: "actor", GuildID: "123", ScopeKind: domain.ApprovalScopeLockdownPlan, ScopeHash: hash, ApproverID: "reviewer", IssuedAt: now, ExpiresAt: now.Add(time.Minute)})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.ConsumeLockdownPlan(context.Background(), grant.ID, "actor", "123", hash, now); err != nil {
		t.Fatal(err)
	}
	if _, err := store.ConsumeLockdownPlan(context.Background(), grant.ID, "actor", "123", hash, now); !errors.Is(err, ErrConsumed) {
		t.Fatalf("second consume = %v", err)
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
			if _, err := store.ConsumeOperation(context.Background(), grant.ID, grant.ActorID, grant.GuildID, grant.ScopeHash, now); err == nil {
				successes.Add(1)
			}
		}()
	}
	wg.Wait()
	if successes.Load() != 1 {
		t.Fatalf("successful consumes = %d", successes.Load())
	}
}

func grantRequest(now time.Time, ttl time.Duration) domain.ApprovalGrant {
	return domain.ApprovalGrant{
		ActorID:    "actor-1",
		GuildID:    "123",
		ScopeKind:  domain.ApprovalScopeOperation,
		ScopeHash:  "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		ApproverID: "human-1",
		IssuedAt:   now,
		ExpiresAt:  now.Add(ttl),
	}
}

type fakeClock struct {
	mu  sync.Mutex
	now time.Time
}

func newFakeClock(now time.Time) *fakeClock {
	return &fakeClock{now: now}
}

func (c *fakeClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *fakeClock) Advance(elapsed time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.now = c.now.Add(elapsed)
}

type sequenceReader struct {
	next byte
}

func (r *sequenceReader) Read(p []byte) (int, error) {
	r.next++
	for i := range p {
		p[i] = r.next + byte(i)
	}
	return len(p), nil
}

type zeroReader struct{}

func (zeroReader) Read(p []byte) (int, error) {
	for i := range p {
		p[i] = byte(i + 1)
	}
	return len(p), nil
}
