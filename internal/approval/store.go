package approval

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"io"
	"sync"
	"time"

	"github.com/ken-kai/discord-control-experiments/internal/domain"
)

const (
	approvalEntropyBytes = 32
	defaultStoreCapacity = 10_000
)

var (
	ErrInvalidGrant = errors.New("invalid approval grant")
	ErrUnknown      = errors.New("approval unknown")
	ErrExpired      = errors.New("approval expired")
	ErrMismatch     = errors.New("approval binding mismatch")
	ErrConsumed     = errors.New("approval already consumed")
	ErrCapacity     = errors.New("approval store capacity reached")
)

type storedGrant struct {
	grant domain.ApprovalGrant
}

type tombstoneKind uint8

const (
	expiredTombstone tombstoneKind = iota + 1
	consumedTombstone
)

type tombstone struct {
	kind        tombstoneKind
	retainUntil time.Time
}

type Store struct {
	mu         sync.Mutex
	grants     map[domain.ApprovalID]storedGrant
	tombstones map[domain.ApprovalID]tombstone
	maxTTL     time.Duration
	capacity   int
	now        func() time.Time
	random     io.Reader
}

func NewStore(maxTTL time.Duration) (*Store, error) {
	return newStore(maxTTL, defaultStoreCapacity, time.Now, rand.Reader)
}

func NewStoreWithDependencies(maxTTL time.Duration, now func() time.Time, random io.Reader) (*Store, error) {
	return newStore(maxTTL, defaultStoreCapacity, now, random)
}

func newStore(maxTTL time.Duration, capacity int, now func() time.Time, random io.Reader) (*Store, error) {
	if maxTTL <= 0 || maxTTL > 24*time.Hour || capacity <= 0 || now == nil || random == nil {
		return nil, ErrInvalidGrant
	}
	return &Store{
		grants:     make(map[domain.ApprovalID]storedGrant),
		tombstones: make(map[domain.ApprovalID]tombstone),
		maxTTL:     maxTTL,
		capacity:   capacity,
		now:        now,
		random:     random,
	}, nil
}

// Issue is a trusted administrative boundary. It is intentionally not exposed as an adapter tool.
func (s *Store) Issue(_ context.Context, requested domain.ApprovalGrant) (domain.ApprovalGrant, error) {
	now := s.now().UTC()
	issuedAt := requested.IssuedAt.UTC()
	if requested.ScopeKind == "" {
		requested.ScopeKind = domain.ApprovalScopeOperation
	}
	if requested.ID != "" || requested.ActorID == "" || requested.GuildID == "" || requested.ApproverID == "" || len(requested.ScopeHash) != 64 || requested.ExpiresAt.IsZero() || (requested.ScopeKind != domain.ApprovalScopeOperation && requested.ScopeKind != domain.ApprovalScopeReconciliationPlan && requested.ScopeKind != domain.ApprovalScopeLockdownPlan) {
		return domain.ApprovalGrant{}, ErrInvalidGrant
	}
	if issuedAt.IsZero() {
		issuedAt = now
	}
	if issuedAt.After(now) || now.Sub(issuedAt) > time.Second || !requested.ExpiresAt.After(now) || requested.ExpiresAt.Sub(issuedAt) > s.maxTTL {
		return domain.ApprovalGrant{}, ErrInvalidGrant
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.purgeLocked(now)
	if len(s.grants)+len(s.tombstones) >= s.capacity {
		return domain.ApprovalGrant{}, ErrCapacity
	}
	idBytes := make([]byte, approvalEntropyBytes)
	if _, err := io.ReadFull(s.random, idBytes); err != nil {
		return domain.ApprovalGrant{}, ErrInvalidGrant
	}
	grant := requested
	grant.ID = domain.ApprovalID(base64.RawURLEncoding.EncodeToString(idBytes))
	grant.IssuedAt = issuedAt
	grant.ExpiresAt = requested.ExpiresAt.UTC()
	grant.ConsumedAt = time.Time{}
	if _, collision := s.grants[grant.ID]; collision {
		return domain.ApprovalGrant{}, ErrInvalidGrant
	}
	if _, collision := s.tombstones[grant.ID]; collision {
		return domain.ApprovalGrant{}, ErrInvalidGrant
	}
	s.grants[grant.ID] = storedGrant{grant: grant}
	return grant, nil
}

func (s *Store) ConsumeOperation(ctx context.Context, id domain.ApprovalID, actor domain.ActorID, guild domain.GuildID, operationHash string, now time.Time) (domain.ApprovalGrant, error) {
	return s.consume(ctx, id, actor, guild, domain.ApprovalScopeOperation, operationHash, now)
}

func (s *Store) ConsumeLockdownPlan(ctx context.Context, id domain.ApprovalID, actor domain.ActorID, guild domain.GuildID, planHash string, now time.Time) (domain.ApprovalGrant, error) {
	return s.consume(ctx, id, actor, guild, domain.ApprovalScopeLockdownPlan, planHash, now)
}

func (s *Store) ConsumeReconciliationPlan(ctx context.Context, id domain.ApprovalID, actor domain.ActorID, guild domain.GuildID, planHash string, now time.Time) (domain.ApprovalGrant, error) {
	return s.consume(ctx, id, actor, guild, domain.ApprovalScopeReconciliationPlan, planHash, now)
}

func (s *Store) consume(_ context.Context, id domain.ApprovalID, actor domain.ActorID, guild domain.GuildID, kind domain.ApprovalScopeKind, scopeHash string, now time.Time) (domain.ApprovalGrant, error) {
	if id == "" || actor == "" || guild == "" || len(scopeHash) != 64 || now.IsZero() {
		return domain.ApprovalGrant{}, ErrMismatch
	}
	now = now.UTC()
	s.mu.Lock()
	defer s.mu.Unlock()
	s.purgeLocked(now)
	if marker, ok := s.tombstones[id]; ok {
		if marker.kind == consumedTombstone {
			return domain.ApprovalGrant{}, ErrConsumed
		}
		return domain.ApprovalGrant{}, ErrExpired
	}
	stored, ok := s.grants[id]
	if !ok {
		return domain.ApprovalGrant{}, ErrUnknown
	}
	if !now.Before(stored.grant.ExpiresAt) {
		s.expireGrantLocked(id, stored.grant.ExpiresAt, now)
		return domain.ApprovalGrant{}, ErrExpired
	}
	actorMatches := subtle.ConstantTimeCompare([]byte(actor), []byte(stored.grant.ActorID)) == 1
	guildMatches := subtle.ConstantTimeCompare([]byte(guild), []byte(stored.grant.GuildID)) == 1
	kindMatches := subtle.ConstantTimeCompare([]byte(kind), []byte(stored.grant.ScopeKind)) == 1
	hashMatches := subtle.ConstantTimeCompare([]byte(scopeHash), []byte(stored.grant.ScopeHash)) == 1
	if !actorMatches || !guildMatches || !kindMatches || !hashMatches {
		return domain.ApprovalGrant{}, ErrMismatch
	}
	delete(s.grants, id)
	stored.grant.ConsumedAt = now
	s.tombstones[id] = tombstone{kind: consumedTombstone, retainUntil: now.Add(s.maxTTL)}
	return stored.grant, nil
}

func (s *Store) purgeLocked(now time.Time) {
	for id, marker := range s.tombstones {
		if !now.Before(marker.retainUntil) {
			delete(s.tombstones, id)
		}
	}
	for id, stored := range s.grants {
		if !now.Before(stored.grant.ExpiresAt) {
			s.expireGrantLocked(id, stored.grant.ExpiresAt, now)
		}
	}
}

func (s *Store) expireGrantLocked(id domain.ApprovalID, expiresAt, now time.Time) {
	delete(s.grants, id)
	retainUntil := expiresAt.Add(s.maxTTL)
	if now.Before(retainUntil) {
		s.tombstones[id] = tombstone{kind: expiredTombstone, retainUntil: retainUntil}
	}
}
