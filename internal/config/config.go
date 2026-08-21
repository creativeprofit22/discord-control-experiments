package config

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"unicode/utf8"
)

const MaxBytes = 64 * 1024

var allowedEnvironmentSecrets = map[string]struct{}{"DISCORD_BOT_TOKEN": {}}

type SecretReference struct {
	Source string `json:"source"`
	Name   string `json:"name"`
}

type Discord struct {
	Token SecretReference `json:"token"`
}
type MCP struct {
	Enabled *bool `json:"enabled"`
}
type Observability struct {
	Exporter string `json:"exporter"`
	Endpoint string `json:"endpoint,omitempty"`
}

type File struct {
	Version       int           `json:"version"`
	Discord       Discord       `json:"discord"`
	MCP           MCP           `json:"mcp"`
	Observability Observability `json:"observability"`
}

type Resolved struct {
	Version       int
	DiscordToken  Secret
	MCPEnabled    bool
	Observability Observability
}

type Diagnostics struct {
	Version         int      `json:"version"`
	EnabledAdapters []string `json:"enabledAdapters"`
	Exporter        string   `json:"exporter"`
}

type SecretResolver interface{ ResolveEnvironment(string) (string, bool) }

type EnvironmentResolver struct{ Lookup func(string) (string, bool) }

func (r EnvironmentResolver) ResolveEnvironment(name string) (string, bool) {
	if r.Lookup == nil {
		return "", false
	}
	return r.Lookup(name)
}

func Load(reader io.Reader, resolver SecretResolver) (Resolved, error) {
	if reader == nil || resolver == nil {
		return Resolved{}, errors.New("configuration reader and secret resolver are required")
	}
	data, err := io.ReadAll(io.LimitReader(reader, MaxBytes+1))
	if err != nil {
		return Resolved{}, fmt.Errorf("read configuration: %w", err)
	}
	if len(data) > MaxBytes {
		return Resolved{}, errors.New("configuration exceeds 65536 bytes")
	}
	if !utf8.Valid(data) {
		return Resolved{}, errors.New("configuration is not valid UTF-8")
	}
	if err := rejectDuplicateKeys(data); err != nil {
		return Resolved{}, err
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	var file File
	if err := decoder.Decode(&file); err != nil {
		return Resolved{}, fmt.Errorf("decode configuration: %w", err)
	}
	if err := requireEOF(decoder); err != nil {
		return Resolved{}, err
	}
	if err := validate(file); err != nil {
		return Resolved{}, err
	}
	value, ok := resolver.ResolveEnvironment(file.Discord.Token.Name)
	if !ok || value == "" {
		return Resolved{}, errors.New("required Discord environment secret is unavailable")
	}
	return Resolved{Version: file.Version, DiscordToken: newSecret(value), MCPEnabled: *file.MCP.Enabled, Observability: file.Observability}, nil
}

func (r Resolved) Diagnostics() Diagnostics {
	adapters := []string{"discord"}
	if r.MCPEnabled {
		adapters = append(adapters, "mcp")
	}
	return Diagnostics{Version: r.Version, EnabledAdapters: adapters, Exporter: r.Observability.Exporter}
}

func requireEOF(decoder *json.Decoder) error {
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		return errors.New("configuration contains trailing JSON")
	}
	return nil
}
