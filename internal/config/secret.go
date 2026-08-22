package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"sync"
)

const redacted = "[REDACTED]"

type SecretRegistrar interface{ RegisterSecret(string) }

type EnvironmentSecretProvider struct {
	mu        sync.RWMutex
	resolver  SecretResolver
	registrar SecretRegistrar
}

func NewEnvironmentSecretProvider(resolver SecretResolver, registrar SecretRegistrar) (*EnvironmentSecretProvider, error) {
	if resolver == nil {
		return nil, errors.New("environment secret resolver is required")
	}
	return &EnvironmentSecretProvider{resolver: resolver, registrar: registrar}, nil
}

func (p *EnvironmentSecretProvider) SetRegistrar(registrar SecretRegistrar) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.registrar = registrar
}

func (p *EnvironmentSecretProvider) Secret(reference SecretReference) (Secret, error) {
	if p == nil || reference.Source != "env" || reference.Name == "" {
		return Secret{}, errors.New("invalid environment secret reference")
	}
	return Secret{provider: p, reference: reference}, nil
}

func (p *EnvironmentSecretProvider) use(reference SecretReference, consumer func(string) error) error {
	if consumer == nil {
		return errors.New("secret consumer is required")
	}
	p.mu.RLock()
	resolver, registrar := p.resolver, p.registrar
	p.mu.RUnlock()
	value, ok := resolver.ResolveEnvironment(reference.Name)
	if !ok || value == "" {
		return errors.New("required environment secret is unavailable")
	}
	if registrar != nil {
		registrar.RegisterSecret(value)
	}
	return consumer(value)
}

type Secret struct {
	provider  *EnvironmentSecretProvider
	reference SecretReference
}

func (Secret) String() string                    { return redacted }
func (Secret) GoString() string                  { return redacted }
func (Secret) MarshalText() ([]byte, error)      { return []byte(redacted), nil }
func (Secret) MarshalJSON() ([]byte, error)      { return json.Marshal(redacted) }
func (Secret) LogValue() slog.Value              { return slog.StringValue(redacted) }
func (Secret) Format(state fmt.State, verb rune) { _, _ = fmt.Fprint(state, redacted) }

func (s Secret) Use(fn func(string) error) error {
	if s.provider == nil {
		return errors.New("secret handle is unavailable")
	}
	return s.provider.use(s.reference, fn)
}
