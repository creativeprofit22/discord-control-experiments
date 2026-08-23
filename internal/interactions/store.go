package interactions

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"sync"
	"time"

	"github.com/ken-kai/discord-control-experiments/internal/domain"
)

type ClaimOutcome string

const (
	ClaimNew            ClaimOutcome = "new"
	ClaimCached         ClaimOutcome = "cached"
	ClaimInFlight       ClaimOutcome = "in_flight"
	ClaimDigestMismatch ClaimOutcome = "digest_mismatch"
	ClaimCapacity       ClaimOutcome = "capacity"
)

type interactionRecord struct {
	digest   [32]byte
	callback []byte
	complete bool
	ready    chan struct{}
	expires  time.Time
}
type InteractionStore struct {
	mu       sync.Mutex
	records  map[string]*interactionRecord
	capacity int
	ttl      time.Duration
	now      func() time.Time
}

func NewInteractionStore(capacity int, ttl time.Duration, now func() time.Time) (*InteractionStore, error) {
	if capacity < 1 || ttl < 15*time.Minute || now == nil {
		return nil, errors.New("invalid interaction store options")
	}
	return &InteractionStore{records: map[string]*interactionRecord{}, capacity: capacity, ttl: ttl, now: now}, nil
}
func (s *InteractionStore) Claim(id string, digest [32]byte) (ClaimOutcome, []byte) {
	for {
		s.mu.Lock()
		s.expireLocked()
		if existing := s.records[id]; existing != nil {
			if subtle.ConstantTimeCompare(existing.digest[:], digest[:]) != 1 {
				s.mu.Unlock()
				return ClaimDigestMismatch, nil
			}
			if existing.complete {
				callback := append([]byte(nil), existing.callback...)
				s.mu.Unlock()
				return ClaimCached, callback
			}
			ready := existing.ready
			s.mu.Unlock()
			<-ready
			continue
		}
		if len(s.records) >= s.capacity {
			s.mu.Unlock()
			return ClaimCapacity, nil
		}
		s.records[id] = &interactionRecord{digest: digest, ready: make(chan struct{}), expires: s.now().Add(s.ttl)}
		s.mu.Unlock()
		return ClaimNew, nil
	}
}
func (s *InteractionStore) Abandon(id string, digest [32]byte) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if record := s.records[id]; record != nil && !record.complete && subtle.ConstantTimeCompare(record.digest[:], digest[:]) == 1 {
		delete(s.records, id)
		close(record.ready)
	}
}
func (s *InteractionStore) Complete(id string, digest [32]byte, callback []byte) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if record := s.records[id]; record != nil && !record.complete && subtle.ConstantTimeCompare(record.digest[:], digest[:]) == 1 {
		record.callback = append([]byte(nil), callback...)
		record.complete = true
		close(record.ready)
	}
}
func (s *InteractionStore) expireLocked() {
	now := s.now()
	for id, record := range s.records {
		if !record.expires.After(now) {
			delete(s.records, id)
			if !record.complete {
				close(record.ready)
			}
		}
	}
}

type PendingKind string

const (
	PendingJobSelect    PendingKind = "job_select"
	PendingCancel       PendingKind = "cancel"
	PendingResumeReview PendingKind = "resume_review"
	PendingResumeModal  PendingKind = "resume_modal"
)

type PendingAction struct {
	Kind        PendingKind
	ActorUserID string
	ActorID     domain.ActorID
	GuildID     domain.GuildID
	RunID       domain.RunID
	PlanHash    domain.PlanHash
	Recoverable bool
	ExpiresAt   time.Time
}
type PendingOutcome string

const (
	PendingAvailable  PendingOutcome = "available"
	PendingExpired    PendingOutcome = "expired"
	PendingReplayed   PendingOutcome = "replayed"
	PendingWrongActor PendingOutcome = "wrong_actor"
	PendingWrongGuild PendingOutcome = "wrong_guild"
	PendingNotFound   PendingOutcome = "not_found"
)

type pendingRecord struct {
	action  PendingAction
	claimed bool
}

type PendingStore struct {
	mu       sync.Mutex
	values   map[string]pendingRecord
	capacity int
	now      func() time.Time
}

func NewPendingStore(capacity int, now func() time.Time) (*PendingStore, error) {
	if capacity < 1 || now == nil {
		return nil, errors.New("invalid pending store options")
	}
	return &PendingStore{values: map[string]pendingRecord{}, capacity: capacity, now: now}, nil
}
func (s *PendingStore) Put(action PendingAction) (string, error) {
	if action.Kind == "" || action.ActorUserID == "" || action.GuildID == "" || action.RunID == "" || !action.ExpiresAt.After(s.now()) {
		return "", errors.New("invalid pending action")
	}
	if (action.Kind == PendingResumeReview || action.Kind == PendingResumeModal) && (action.ActorID == "" || len(action.PlanHash) != 64 || !action.Recoverable) {
		return "", errors.New("invalid resume action")
	}
	nonceBytes := make([]byte, 18)
	if _, err := rand.Read(nonceBytes); err != nil {
		return "", err
	}
	nonce := base64.RawURLEncoding.EncodeToString(nonceBytes)
	customID := string(action.Kind) + "." + nonce
	s.mu.Lock()
	defer s.mu.Unlock()
	s.pruneTerminalLocked()
	if len(s.values) >= s.capacity {
		return "", errors.New("pending action capacity reached")
	}
	s.values[customID] = pendingRecord{action: action}
	return customID, nil
}
func (s *PendingStore) Get(customID, userID string, guildID domain.GuildID) (PendingAction, PendingOutcome) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.lookupLocked(customID, userID, guildID, false)
}
func (s *PendingStore) Claim(customID, userID string, guildID domain.GuildID) (PendingAction, PendingOutcome) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.lookupLocked(customID, userID, guildID, true)
}
func (s *PendingStore) lookupLocked(customID, userID string, guildID domain.GuildID, claim bool) (PendingAction, PendingOutcome) {
	record, ok := s.values[customID]
	if !ok {
		return PendingAction{}, PendingNotFound
	}
	action := record.action
	if !action.ExpiresAt.After(s.now()) {
		return action, PendingExpired
	}
	if subtle.ConstantTimeCompare([]byte(action.ActorUserID), []byte(userID)) != 1 {
		return action, PendingWrongActor
	}
	if subtle.ConstantTimeCompare([]byte(action.GuildID), []byte(guildID)) != 1 {
		return action, PendingWrongGuild
	}
	if record.claimed {
		return action, PendingReplayed
	}
	if claim {
		record.claimed = true
		s.values[customID] = record
	}
	return action, PendingAvailable
}
func (s *PendingStore) Restore(customID string, action PendingAction) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if customID == "" || !action.ExpiresAt.After(s.now()) {
		return false
	}
	if record, ok := s.values[customID]; ok {
		if !record.claimed || record.action != action {
			return false
		}
		record.claimed = false
		s.values[customID] = record
		return true
	}
	s.pruneTerminalLocked()
	if len(s.values) >= s.capacity {
		return false
	}
	s.values[customID] = pendingRecord{action: action}
	return true
}
func (s *PendingStore) pruneTerminalLocked() {
	if len(s.values) < s.capacity {
		return
	}
	now := s.now()
	for id, record := range s.values {
		if record.claimed || !record.action.ExpiresAt.After(now) {
			delete(s.values, id)
			if len(s.values) < s.capacity {
				return
			}
		}
	}
}
