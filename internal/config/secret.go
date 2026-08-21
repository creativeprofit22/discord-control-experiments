package config

import (
	"encoding/json"
	"fmt"
	"log/slog"
)

const redacted = "[REDACTED]"

type Secret struct{ value string }

func newSecret(value string) Secret              { return Secret{value: value} }
func (Secret) String() string                    { return redacted }
func (Secret) GoString() string                  { return redacted }
func (Secret) MarshalText() ([]byte, error)      { return []byte(redacted), nil }
func (Secret) MarshalJSON() ([]byte, error)      { return json.Marshal(redacted) }
func (Secret) LogValue() slog.Value              { return slog.StringValue(redacted) }
func (Secret) Format(state fmt.State, verb rune) { _, _ = fmt.Fprint(state, redacted) }

func (s Secret) Use(fn func(string) error) error {
	if fn == nil {
		return fmt.Errorf("secret consumer is required")
	}
	return fn(s.value)
}
