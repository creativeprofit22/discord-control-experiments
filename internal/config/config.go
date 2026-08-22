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
	Token    SecretReference `json:"token"`
	Webhooks []WebhookSecret `json:"webhooks,omitempty"`
}

type WebhookSecret struct {
	Name string          `json:"name"`
	URL  SecretReference `json:"url"`
}
type MCP struct {
	Enabled            *bool    `json:"enabled"`
	GuildID            string   `json:"guildId,omitempty"`
	Mode               string   `json:"mode,omitempty"`
	Toolsets           []string `json:"toolsets,omitempty"`
	ReadOnly           *bool    `json:"readOnly,omitempty"`
	MaxMessageBytes    int      `json:"maxMessageBytes,omitempty"`
	MaxConcurrentCalls int      `json:"maxConcurrentCalls,omitempty"`
	CallTimeout        string   `json:"callTimeout,omitempty"`
}
type Observability struct {
	Exporter string `json:"exporter"`
	Endpoint string `json:"endpoint,omitempty"`
}
type PolicyPrincipal struct {
	Adapter              string `json:"adapter"`
	Subject              string `json:"subject"`
	AuthenticationMethod string `json:"authenticationMethod"`
}

type PolicyActor struct {
	ID                    string            `json:"id"`
	Principals            []PolicyPrincipal `json:"principals"`
	Guilds                []string          `json:"guilds"`
	Capabilities          []string          `json:"capabilities"`
	Mode                  string            `json:"mode"`
	PreapprovedOperations []string          `json:"preapprovedOperations,omitempty"`
}

type Policy struct {
	Mode   string        `json:"mode"`
	Actors []PolicyActor `json:"actors"`
}

type Approvals struct {
	MaxTTL string `json:"maxTtl"`
}

type Audit struct {
	Path          string `json:"path"`
	RetentionDays int    `json:"retentionDays"`
	MaxBytes      int64  `json:"maxBytes"`
}

type Reconciliation struct {
	StateDirectory           string `json:"stateDirectory"`
	MaxOperations            int    `json:"maxOperations"`
	MaxDestructiveOperations int    `json:"maxDestructiveOperations"`
	RetentionDays            int    `json:"retentionDays"`
}

type SecurityAlerts struct {
	DenialThreshold      int    `json:"denialThreshold"`
	DenialWindow         string `json:"denialWindow"`
	DestructiveThreshold int    `json:"destructiveThreshold"`
	DestructiveWindow    string `json:"destructiveWindow"`
}

type File struct {
	Version                    int             `json:"version"`
	Discord                    Discord         `json:"discord"`
	MCP                        MCP             `json:"mcp"`
	Observability              Observability   `json:"observability"`
	Policy                     *Policy         `json:"policy,omitempty"`
	Approvals                  *Approvals      `json:"approvals,omitempty"`
	Audit                      *Audit          `json:"audit,omitempty"`
	SecurityAlerts             *SecurityAlerts `json:"securityAlerts,omitempty"`
	Reconciliation             *Reconciliation `json:"reconciliation,omitempty"`
	SecretEnvironmentAllowlist []string        `json:"secretEnvironmentAllowlist,omitempty"`
}

type Resolved struct {
	Version         int
	DiscordToken    Secret
	DiscordWebhooks map[string]Secret
	SecretProvider  *EnvironmentSecretProvider
	MCPEnabled      bool
	MCP             MCP
	Observability   Observability
	Policy          Policy
	Approvals       Approvals
	Audit           Audit
	SecurityAlerts  SecurityAlerts
	Reconciliation  Reconciliation
}

type Diagnostics struct {
	Version         int      `json:"version"`
	EnabledAdapters []string `json:"enabledAdapters"`
	Exporter        string   `json:"exporter"`
}

type SecretResolver interface{ ResolveEnvironment(string) (string, bool) }

func applySecurityDefaults(file *File) {
	if file.MCP.Mode == "" {
		file.MCP.Mode = "read_only"
	}
	if file.MCP.Toolsets == nil {
		file.MCP.Toolsets = []string{"diagnostic"}
	}
	if file.MCP.ReadOnly == nil {
		value := true
		file.MCP.ReadOnly = &value
	}
	if file.MCP.MaxMessageBytes == 0 {
		file.MCP.MaxMessageBytes = 1 << 20
	}
	if file.MCP.MaxConcurrentCalls == 0 {
		file.MCP.MaxConcurrentCalls = 4
	}
	if file.MCP.CallTimeout == "" {
		file.MCP.CallTimeout = "30s"
	}
	if file.Policy == nil {
		file.Policy = &Policy{Mode: "read_only", Actors: []PolicyActor{}}
	}
	if file.Approvals == nil {
		file.Approvals = &Approvals{MaxTTL: "15m"}
	}
	if file.Audit == nil {
		file.Audit = &Audit{Path: "audit/events.jsonl", RetentionDays: 30, MaxBytes: 64 << 20}
	}
	if file.SecurityAlerts == nil {
		file.SecurityAlerts = &SecurityAlerts{DenialThreshold: 10, DenialWindow: "5m", DestructiveThreshold: 5, DestructiveWindow: "5m"}
	}
	if file.Reconciliation == nil {
		file.Reconciliation = &Reconciliation{StateDirectory: "state", MaxOperations: 100, MaxDestructiveOperations: 10, RetentionDays: 30}
	}
}

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
	applySecurityDefaults(&file)
	if err := validate(file); err != nil {
		return Resolved{}, err
	}
	provider, err := NewEnvironmentSecretProvider(resolver, nil)
	if err != nil {
		return Resolved{}, err
	}
	token, err := provider.Secret(file.Discord.Token)
	if err != nil || token.Use(func(string) error { return nil }) != nil {
		return Resolved{}, errors.New("required Discord environment secret is unavailable")
	}
	webhooks := make(map[string]Secret, len(file.Discord.Webhooks))
	for _, configured := range file.Discord.Webhooks {
		handle, handleErr := provider.Secret(configured.URL)
		if handleErr != nil || handle.Use(func(string) error { return nil }) != nil {
			return Resolved{}, errors.New("required Discord webhook environment secret is unavailable")
		}
		webhooks[configured.Name] = handle
	}
	return Resolved{Version: file.Version, DiscordToken: token, DiscordWebhooks: webhooks, SecretProvider: provider, MCPEnabled: *file.MCP.Enabled, MCP: file.MCP, Observability: file.Observability, Policy: *file.Policy, Approvals: *file.Approvals, Audit: *file.Audit, SecurityAlerts: *file.SecurityAlerts, Reconciliation: *file.Reconciliation}, nil
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
