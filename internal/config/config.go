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
	Name      string          `json:"name"`
	GuildID   string          `json:"guildId,omitempty"`
	ChannelID string          `json:"channelId,omitempty"`
	URL       SecretReference `json:"url"`
}

type AutomationSource struct {
	Name         string `json:"name"`
	Kind         string `json:"kind"`
	URL          string `json:"url,omitempty"`
	GitHubOwner  string `json:"githubOwner,omitempty"`
	GitHubRepo   string `json:"githubRepo,omitempty"`
	MaxBodyBytes int64  `json:"maxBodyBytes,omitempty"`
}

type AutomationSummarizer struct {
	Enabled *bool           `json:"enabled,omitempty"`
	Model   string          `json:"model,omitempty"`
	APIKey  SecretReference `json:"apiKey,omitempty"`
}

type Automation struct {
	Enabled              *bool                `json:"enabled,omitempty"`
	WorkflowPaths        []string             `json:"workflowPaths,omitempty"`
	Sources              []AutomationSource   `json:"sources,omitempty"`
	CleanupChannels      []string             `json:"cleanupChannels,omitempty"`
	Summarizer           AutomationSummarizer `json:"summarizer,omitempty"`
	PollInterval         string               `json:"pollInterval,omitempty"`
	LeaseDuration        string               `json:"leaseDuration,omitempty"`
	WorkerCount          int                  `json:"workerCount,omitempty"`
	MaxWorkflowBytes     int64                `json:"maxWorkflowBytes,omitempty"`
	MaxSteps             int                  `json:"maxSteps,omitempty"`
	MaxGuildsPerRun      int                  `json:"maxGuildsPerRun,omitempty"`
	MaxActionsPerRun     int                  `json:"maxActionsPerRun,omitempty"`
	MaxDestructivePerRun int                  `json:"maxDestructivePerRun,omitempty"`
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

type InteractionRegistration struct {
	Scope   string `json:"scope,omitempty"`
	GuildID string `json:"guildId,omitempty"`
}

type InteractionTLS struct {
	Mode              string          `json:"mode,omitempty"`
	Certificate       SecretReference `json:"certificate,omitempty"`
	PrivateKey        SecretReference `json:"privateKey,omitempty"`
	TrustedProxyCIDRs []string        `json:"trustedProxyCidrs,omitempty"`
}

type Interactions struct {
	Enabled               *bool                   `json:"enabled"`
	ListenAddress         string                  `json:"listenAddress,omitempty"`
	Path                  string                  `json:"path,omitempty"`
	ApplicationID         string                  `json:"applicationId,omitempty"`
	SigningPublicKeys     []string                `json:"signingPublicKeys,omitempty"`
	Registration          InteractionRegistration `json:"registration,omitempty"`
	TLS                   InteractionTLS          `json:"tls,omitempty"`
	MaxBodyBytes          int64                   `json:"maxBodyBytes,omitempty"`
	MaxConcurrentRequests int                     `json:"maxConcurrentRequests,omitempty"`
	QueueCapacity         int                     `json:"queueCapacity,omitempty"`
	WorkerCount           int                     `json:"workerCount,omitempty"`
	AckDeadline           string                  `json:"ackDeadline,omitempty"`
	WorkTimeout           string                  `json:"workTimeout,omitempty"`
	CallbackTimeout       string                  `json:"callbackTimeout,omitempty"`
	DedupTTL              string                  `json:"dedupTtl,omitempty"`
	TimestampSkew         string                  `json:"timestampSkew,omitempty"`
}
type Observability struct {
	Exporter         string  `json:"exporter"`
	Endpoint         string  `json:"endpoint,omitempty"`
	TraceSampleRatio float64 `json:"traceSampleRatio,omitempty"`
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

type ModerationEscalationRule struct {
	ID                  string   `json:"id"`
	Priority            int      `json:"priority"`
	EligiblePriorKinds  []string `json:"eligiblePriorKinds"`
	Lookback            string   `json:"lookback"`
	MinimumCount        int      `json:"minimumCount"`
	Action              string   `json:"action"`
	Duration            string   `json:"duration,omitempty"`
	DeleteWindow        string   `json:"deleteWindow,omitempty"`
	MaximumApplications int      `json:"maximumApplications"`
}

type Moderation struct {
	PolicyVersion                    string                     `json:"policyVersion"`
	Retention                        string                     `json:"retention"`
	MaxCasesPerGuild                 int                        `json:"maxCasesPerGuild"`
	MaxActionsPerCase                int                        `json:"maxActionsPerCase"`
	MaxActionsPerSubjectWindow       int                        `json:"maxActionsPerSubjectWindow"`
	MaxActionsPerGuildWindow         int                        `json:"maxActionsPerGuildWindow"`
	ReasonCodes                      []string                   `json:"reasonCodes"`
	QuarantineRoleID                 string                     `json:"quarantineRoleId,omitempty"`
	ProtectedOperatorRoleIDs         []string                   `json:"protectedOperatorRoleIds"`
	EscalationRules                  []ModerationEscalationRule `json:"escalationRules"`
	MaxLockdownOperations            int                        `json:"maxLockdownOperations"`
	MaxLockdownDestructiveOperations int                        `json:"maxLockdownDestructiveOperations"`
	LockdownPlanTTL                  string                     `json:"lockdownPlanTtl"`
}

type SecurityAlerts struct {
	DenialThreshold                 int    `json:"denialThreshold"`
	DenialWindow                    string `json:"denialWindow"`
	DestructiveThreshold            int    `json:"destructiveThreshold"`
	DestructiveWindow               string `json:"destructiveWindow"`
	SignatureFailureThreshold       int    `json:"signatureFailureThreshold,omitempty"`
	SignatureFailureWindow          string `json:"signatureFailureWindow,omitempty"`
	AckDeadlineMissThreshold        int    `json:"ackDeadlineMissThreshold,omitempty"`
	AckDeadlineMissWindow           string `json:"ackDeadlineMissWindow,omitempty"`
	CallbackFailureThreshold        int    `json:"callbackFailureThreshold,omitempty"`
	CallbackFailureWindow           string `json:"callbackFailureWindow,omitempty"`
	HandlerSaturationThreshold      int    `json:"handlerSaturationThreshold,omitempty"`
	HandlerSaturationWindow         string `json:"handlerSaturationWindow,omitempty"`
	ModerationDestructiveThreshold  int    `json:"moderationDestructiveThreshold,omitempty"`
	ModerationDestructiveWindow     string `json:"moderationDestructiveWindow,omitempty"`
	LockdownPartialFailureThreshold int    `json:"lockdownPartialFailureThreshold,omitempty"`
	LockdownPartialFailureWindow    string `json:"lockdownPartialFailureWindow,omitempty"`
}

type File struct {
	Version                    int             `json:"version"`
	Discord                    Discord         `json:"discord"`
	MCP                        MCP             `json:"mcp"`
	Interactions               Interactions    `json:"interactions,omitempty"`
	Automation                 Automation      `json:"automation,omitempty"`
	Observability              Observability   `json:"observability"`
	Policy                     *Policy         `json:"policy,omitempty"`
	Approvals                  *Approvals      `json:"approvals,omitempty"`
	Audit                      *Audit          `json:"audit,omitempty"`
	SecurityAlerts             *SecurityAlerts `json:"securityAlerts,omitempty"`
	Reconciliation             *Reconciliation `json:"reconciliation,omitempty"`
	Moderation                 *Moderation     `json:"moderation,omitempty"`
	SecretEnvironmentAllowlist []string        `json:"secretEnvironmentAllowlist,omitempty"`
}

type Resolved struct {
	Version                 int
	Discord                 Discord
	DiscordToken            Secret
	DiscordWebhooks         map[string]Secret
	AutomationSummarizerKey Secret
	SecretProvider          *EnvironmentSecretProvider
	MCPEnabled              bool
	MCP                     MCP
	InteractionsEnabled     bool
	Interactions            Interactions
	AutomationEnabled       bool
	Automation              Automation
	InteractionCertificate  Secret
	InteractionPrivateKey   Secret
	Observability           Observability
	Policy                  Policy
	Approvals               Approvals
	Audit                   Audit
	SecurityAlerts          SecurityAlerts
	Reconciliation          Reconciliation
	Moderation              Moderation
}

type Diagnostics struct {
	Version                  int      `json:"version"`
	EnabledAdapters          []string `json:"enabledAdapters"`
	Exporter                 string   `json:"exporter"`
	InteractionsEnabled      bool     `json:"interactionsEnabled"`
	InteractionsTLSMode      string   `json:"interactionsTlsMode,omitempty"`
	InteractionsRegistration string   `json:"interactionsRegistration,omitempty"`
}

type SecretResolver interface{ ResolveEnvironment(string) (string, bool) }

func applySecurityDefaults(file *File) {
	if file.Observability.TraceSampleRatio == 0 {
		file.Observability.TraceSampleRatio = 0.1
	}
	if file.Interactions.Enabled == nil {
		value := false
		file.Interactions.Enabled = &value
	}
	if file.Interactions.Path == "" {
		file.Interactions.Path = "/interactions"
	}
	if file.Interactions.ListenAddress == "" {
		file.Interactions.ListenAddress = "127.0.0.1:8443"
	}
	if file.Interactions.MaxBodyBytes == 0 {
		file.Interactions.MaxBodyBytes = 64 << 10
	}
	if file.Interactions.MaxConcurrentRequests == 0 {
		file.Interactions.MaxConcurrentRequests = 32
	}
	if file.Interactions.QueueCapacity == 0 {
		file.Interactions.QueueCapacity = 64
	}
	if file.Interactions.WorkerCount == 0 {
		file.Interactions.WorkerCount = 4
	}
	if file.Interactions.AckDeadline == "" {
		file.Interactions.AckDeadline = "2500ms"
	}
	if file.Interactions.WorkTimeout == "" {
		file.Interactions.WorkTimeout = "2m"
	}
	if file.Interactions.CallbackTimeout == "" {
		file.Interactions.CallbackTimeout = "10s"
	}
	if file.Interactions.DedupTTL == "" {
		file.Interactions.DedupTTL = "15m"
	}
	if file.Interactions.TimestampSkew == "" {
		file.Interactions.TimestampSkew = "5m"
	}
	if file.Automation.Enabled == nil {
		value := false
		file.Automation.Enabled = &value
	}
	if file.Automation.Summarizer.Enabled == nil {
		value := false
		file.Automation.Summarizer.Enabled = &value
	}
	if file.Automation.PollInterval == "" {
		file.Automation.PollInterval = "30s"
	}
	if file.Automation.LeaseDuration == "" {
		file.Automation.LeaseDuration = "2m"
	}
	if file.Automation.WorkerCount == 0 {
		file.Automation.WorkerCount = 2
	}
	if file.Automation.MaxWorkflowBytes == 0 {
		file.Automation.MaxWorkflowBytes = 64 << 10
	}
	if file.Automation.MaxSteps == 0 {
		file.Automation.MaxSteps = 100
	}
	if file.Automation.MaxGuildsPerRun == 0 {
		file.Automation.MaxGuildsPerRun = 16
	}
	if file.Automation.MaxActionsPerRun == 0 {
		file.Automation.MaxActionsPerRun = 500
	}
	if file.Automation.MaxDestructivePerRun == 0 {
		file.Automation.MaxDestructivePerRun = 25
	}
	for i := range file.Automation.Sources {
		if file.Automation.Sources[i].MaxBodyBytes == 0 {
			file.Automation.Sources[i].MaxBodyBytes = 1 << 20
		}
	}
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
	alerts := file.SecurityAlerts
	if alerts.SignatureFailureThreshold == 0 {
		alerts.SignatureFailureThreshold = 20
	}
	if alerts.SignatureFailureWindow == "" {
		alerts.SignatureFailureWindow = "5m"
	}
	if alerts.AckDeadlineMissThreshold == 0 {
		alerts.AckDeadlineMissThreshold = 3
	}
	if alerts.AckDeadlineMissWindow == "" {
		alerts.AckDeadlineMissWindow = "5m"
	}
	if alerts.CallbackFailureThreshold == 0 {
		alerts.CallbackFailureThreshold = 5
	}
	if alerts.CallbackFailureWindow == "" {
		alerts.CallbackFailureWindow = "5m"
	}
	if alerts.HandlerSaturationThreshold == 0 {
		alerts.HandlerSaturationThreshold = 5
	}
	if alerts.HandlerSaturationWindow == "" {
		alerts.HandlerSaturationWindow = "5m"
	}
	if file.Reconciliation == nil {
		file.Reconciliation = &Reconciliation{StateDirectory: "state", MaxOperations: 100, MaxDestructiveOperations: 10, RetentionDays: 30}
	}
	if file.Moderation == nil {
		file.Moderation = &Moderation{PolicyVersion: "disabled-v1", Retention: "720h", MaxCasesPerGuild: 10000, MaxActionsPerCase: 100, MaxActionsPerSubjectWindow: 10, MaxActionsPerGuildWindow: 100, ReasonCodes: []string{}, ProtectedOperatorRoleIDs: []string{}, EscalationRules: []ModerationEscalationRule{}, MaxLockdownOperations: 100, MaxLockdownDestructiveOperations: 20, LockdownPlanTTL: "15m"}
	}
	if alerts.ModerationDestructiveThreshold == 0 {
		alerts.ModerationDestructiveThreshold = 5
	}
	if alerts.ModerationDestructiveWindow == "" {
		alerts.ModerationDestructiveWindow = "5m"
	}
	if alerts.LockdownPartialFailureThreshold == 0 {
		alerts.LockdownPartialFailureThreshold = 1
	}
	if alerts.LockdownPartialFailureWindow == "" {
		alerts.LockdownPartialFailureWindow = "5m"
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
	var certificate, privateKey, summarizerKey Secret
	if *file.Interactions.Enabled && file.Interactions.TLS.Mode == "direct" {
		certificate, err = provider.Secret(file.Interactions.TLS.Certificate)
		if err != nil || certificate.Use(func(string) error { return nil }) != nil {
			return Resolved{}, errors.New("required interactions TLS certificate is unavailable")
		}
		privateKey, err = provider.Secret(file.Interactions.TLS.PrivateKey)
		if err != nil || privateKey.Use(func(string) error { return nil }) != nil {
			return Resolved{}, errors.New("required interactions TLS private key is unavailable")
		}
	}
	if *file.Automation.Summarizer.Enabled {
		summarizerKey, err = provider.Secret(file.Automation.Summarizer.APIKey)
		if err != nil || summarizerKey.Use(func(string) error { return nil }) != nil {
			return Resolved{}, errors.New("required automation summarizer environment secret is unavailable")
		}
	}
	return Resolved{Version: file.Version, Discord: file.Discord, DiscordToken: token, DiscordWebhooks: webhooks, AutomationSummarizerKey: summarizerKey, SecretProvider: provider, MCPEnabled: *file.MCP.Enabled, MCP: file.MCP, InteractionsEnabled: *file.Interactions.Enabled, Interactions: file.Interactions, AutomationEnabled: *file.Automation.Enabled, Automation: file.Automation, InteractionCertificate: certificate, InteractionPrivateKey: privateKey, Observability: file.Observability, Policy: *file.Policy, Approvals: *file.Approvals, Audit: *file.Audit, SecurityAlerts: *file.SecurityAlerts, Reconciliation: *file.Reconciliation, Moderation: *file.Moderation}, nil
}

func (r Resolved) Diagnostics() Diagnostics {
	adapters := []string{"discord"}
	if r.MCPEnabled {
		adapters = append(adapters, "mcp")
	}
	if r.InteractionsEnabled {
		adapters = append(adapters, "interactions")
	}
	diagnostics := Diagnostics{Version: r.Version, EnabledAdapters: adapters, Exporter: r.Observability.Exporter, InteractionsEnabled: r.InteractionsEnabled}
	if r.InteractionsEnabled {
		diagnostics.InteractionsTLSMode = r.Interactions.TLS.Mode
		diagnostics.InteractionsRegistration = r.Interactions.Registration.Scope
	}
	return diagnostics
}

func requireEOF(decoder *json.Decoder) error {
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		return errors.New("configuration contains trailing JSON")
	}
	return nil
}
