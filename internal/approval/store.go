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

const approvalEntropyBytes = 32

var (
	ErrInvalidGrant = errors.New("invalid approval grant")
	ErrUnknown      = errors.New("approval unknown")
	ErrExpired      = errors.New("approval expired")
	ErrMismatch     = errors.New("approval binding mismatch")
	ErrConsumed     = errors.New("approval already consumed")
)

type storedGrant struct {
	grant    domain.ApprovalGrant
	consumed bool
}

type Store struct {
	mu     sync.Mutex
	grants map[domain.ApprovalID]storedGrant
	maxTTL time.Duration
	now    func() time.Time
	random io.Reader
}

func NewStore(maxTTL time.Duration) (*Store, error) {
	return NewStoreWithDependencies(maxTTL, time.Now, rand.Reader)
}

func NewStoreWithDependencies(maxTTL time.Duration, now func() time.Time, random io.Reader) (*Store, error) {
	if maxTTL <= 0 || maxTTL > 24*time.Hour || now == nil || random == nil {
		return nil, ErrInvalidGrant
	}
	return &Store{grants: make(map[domain.ApprovalID]storedGrant), maxTTL: maxTTL, now: now, random: random}, nil
}

// Issue is a trusted administrative boundary. It is intentionally not exposed as an adapter tool.
func (s *Store) Issue(_ context.Context, requested domain.ApprovalGrant) (domain.ApprovalGrant, error) {
	now := s.now().UTC()
	issuedAt := requested.IssuedAt.UTC()
	if requested.ID != "" || requested.ActorID == "" || requested.ApproverID == "" || len(requested.OperationHash) != 64 || requested.ExpiresAt.IsZero() {
		return domain.ApprovalGrant{}, ErrInvalidGrant
	}
	if issuedAt.IsZero() {
		issuedAt = now
	}
	if issuedAt.After(now) || now.Sub(issuedAt) > time.Second || !requested.ExpiresAt.After(now) || requested.ExpiresAt.Sub(issuedAt) > s.maxTTL {
		return domain.ApprovalGrant{}, ErrInvalidGrant
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
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, collision := s.grants[grant.ID]; collision {
		return domain.ApprovalGrant{}, ErrInvalidGrant
	}
	s.grants[grant.ID] = storedGrant{grant: grant}
	return grant, nil
}

func (s *Store) Consume(_ context.Context, id domain.ApprovalID, actor domain.ActorID, operationHash string, now time.Time) (domain.ApprovalGrant, error) {
	if id == "" || actor == "" || len(operationHash) != 64 || now.IsZero() {
		return domain.ApprovalGrant{}, ErrMismatch
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	stored, ok := s.grants[id]
	if !ok {
		return domain.ApprovalGrant{}, ErrUnknown
	}
	if stored.consumed {
		return domain.ApprovalGrant{}, ErrConsumed
	}
	if !now.UTC().Before(stored.grant.ExpiresAt) {
		return domain.ApprovalGrant{}, ErrExpired
	}
	actorMatches := subtle.ConstantTimeCompare([]byte(actor), []byte(stored.grant.ActorID)) == 1
	hashMatches := subtle.ConstantTimeCompare([]byte(operationHash), []byte(stored.grant.OperationHash)) == 1
	if !actorMatches || !hashMatches {
		return domain.ApprovalGrant{}, ErrMismatch
	}
	stored.consumed = true
	stored.grant.ConsumedAt = now.UTC()
	s.grants[id] = stored
	return stored.grant, nil
}
