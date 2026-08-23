package application

import (
	"sync"

	"github.com/ken-kai/discord-control-experiments/internal/domain"
)

type ResourceKind = domain.ResourceKind

const (
	ResourceChannel            = domain.ResourceChannel
	ResourceRole               = domain.ResourceRole
	ResourceUser               = domain.ResourceMember
	ResourceMessage            = domain.ResourceMessage
	ResourceWebhook            = domain.ResourceWebhook
	ResourceInvite             = domain.ResourceInvite
	ResourceApplicationCommand = domain.ResourceApplicationCommand
	ResourceScheduledEvent     = domain.ResourceScheduledEvent
	ResourceAutoModerationRule = domain.ResourceAutoModerationRule
)

type ResourceOwnershipStore interface {
	Guild(ResourceKind, string) (domain.GuildID, bool)
	Record(ResourceKind, string, domain.GuildID)
	Remove(ResourceKind, string)
}

type InMemoryResourceOwnershipStore struct {
	mu     sync.RWMutex
	owners map[ownershipKey]domain.GuildID
}
type ownershipKey struct {
	kind ResourceKind
	id   string
}

func NewInMemoryResourceOwnershipStore() *InMemoryResourceOwnershipStore {
	return &InMemoryResourceOwnershipStore{owners: make(map[ownershipKey]domain.GuildID)}
}
func (s *InMemoryResourceOwnershipStore) Guild(kind ResourceKind, id string) (domain.GuildID, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	guild, ok := s.owners[ownershipKey{kind, id}]
	return guild, ok
}
func (s *InMemoryResourceOwnershipStore) Record(kind ResourceKind, id string, guild domain.GuildID) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.owners[ownershipKey{kind, id}] = guild
}
func (s *InMemoryResourceOwnershipStore) Remove(kind ResourceKind, id string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.owners, ownershipKey{kind, id})
}
