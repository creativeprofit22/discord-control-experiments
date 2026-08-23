package interactions

import (
	"crypto/sha256"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ken-kai/discord-control-experiments/internal/domain"
)

func TestInteractionStoreConcurrentDedupAndCapacity(t *testing.T) {
	now := time.Unix(1_800_000_000, 0)
	store, err := NewInteractionStore(1, 15*time.Minute, func() time.Time { return now })
	if err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256([]byte("body"))
	callback := []byte(`{"type":5,"data":{"flags":64}}`)
	var fresh atomic.Int32
	var wg sync.WaitGroup
	for range 32 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			outcome, cached := store.Claim("123", digest)
			if outcome == ClaimNew {
				fresh.Add(1)
				store.Complete("123", digest, callback)
				cached = callback
			}
			if string(cached) != string(callback) {
				t.Errorf("cached callback changed")
			}
		}()
	}
	wg.Wait()
	if fresh.Load() != 1 {
		t.Fatalf("fresh claims = %d", fresh.Load())
	}
	other := sha256.Sum256([]byte("other"))
	if outcome, _ := store.Claim("123", other); outcome != ClaimDigestMismatch {
		t.Fatalf("digest outcome = %s", outcome)
	}
	if outcome, _ := store.Claim("456", other); outcome != ClaimCapacity {
		t.Fatalf("capacity outcome = %s", outcome)
	}
	if outcome, _ := store.Claim("123", digest); outcome != ClaimCached {
		t.Fatalf("complete outcome = %s", outcome)
	}
	now = now.Add(16 * time.Minute)
	if outcome, _ := store.Claim("456", other); outcome != ClaimNew {
		t.Fatalf("expired outcome = %s", outcome)
	}
}
func TestPendingStoreActorGuildExpiryAndReplay(t *testing.T) {
	now := time.Unix(1_800_000_000, 0)
	store, err := NewPendingStore(2, func() time.Time { return now })
	if err != nil {
		t.Fatal(err)
	}
	action := PendingAction{Kind: PendingResumeReview, ActorUserID: "111", ActorID: "actor", GuildID: "222", RunID: "run", PlanHash: domain.PlanHash("aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"), Recoverable: true, ExpiresAt: now.Add(time.Minute)}
	id, err := store.Put(action)
	if err != nil {
		t.Fatal(err)
	}
	if id == "" || len(id) > 100 {
		t.Fatalf("opaque id = %q", id)
	}
	if _, outcome := store.Get(id, "other", "222"); outcome != PendingWrongActor {
		t.Fatalf("wrong actor outcome = %s", outcome)
	}
	if _, outcome := store.Get(id, "111", "other"); outcome != PendingWrongGuild {
		t.Fatalf("wrong guild outcome = %s", outcome)
	}
	if _, outcome := store.Claim(id, "111", "222"); outcome != PendingAvailable {
		t.Fatalf("valid claim outcome = %s", outcome)
	}
	if _, outcome := store.Claim(id, "111", "222"); outcome != PendingReplayed {
		t.Fatalf("replay outcome = %s", outcome)
	}
	now = now.Add(time.Minute)
	if _, outcome := store.Get(id, "111", "222"); outcome != PendingExpired {
		t.Fatalf("expiry outcome = %s", outcome)
	}
}
