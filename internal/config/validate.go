package config

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

func validate(file File) error {
	if file.Version != 1 {
		return errors.New("configuration version must be 1")
	}
	if file.Discord.Token.Source != "env" {
		return errors.New("discord token source must be env")
	}
	if file.MCP.Enabled == nil {
		return errors.New("mcp enabled field is required")
	}
	if err := validateMCP(file.MCP); err != nil {
		return err
	}
	if !environmentAllowed(file.Discord.Token.Name, file.SecretEnvironmentAllowlist) {
		return errors.New("discord token environment name is not allowlisted")
	}
	if err := validateWebhooks(file.Discord.Webhooks, file.SecretEnvironmentAllowlist); err != nil {
		return err
	}
	if err := validatePolicy(*file.Policy); err != nil {
		return err
	}
	if err := validateSecuritySettings(*file.Approvals, *file.Audit, *file.SecurityAlerts); err != nil {
		return err
	}
	if err := validateReconciliation(*file.Reconciliation); err != nil {
		return err
	}
	switch file.Observability.Exporter {
	case "none", "stderr":
		if file.Observability.Endpoint != "" {
			return errors.New("observability endpoint is allowed only for otlp-http")
		}
	case "otlp-http":
		u, err := url.Parse(file.Observability.Endpoint)
		if err != nil || u.Scheme != "https" || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
			return errors.New("otlp endpoint must be an explicit HTTPS URL without credentials, query, or fragment")
		}
	default:
		return errors.New("unsupported observability exporter")
	}
	return nil
}

func validateMCP(value MCP) error {
	if !validMode(value.Mode) {
		return errors.New("mcp mode must be read_only, dry_run, or apply")
	}
	if value.ReadOnly == nil {
		return errors.New("mcp readOnly setting is required after defaults")
	}
	if *value.Enabled && !validSnowflake(value.GuildID) {
		return errors.New("enabled mcp requires one valid guildId")
	}
	if !*value.Enabled && value.GuildID != "" && !validSnowflake(value.GuildID) {
		return errors.New("mcp guildId is invalid")
	}
	if len(value.Toolsets) == 0 || len(value.Toolsets) > 4 {
		return errors.New("mcp toolsets must contain one to four values")
	}
	allowed := map[string]bool{"build": true, "moderation": true, "news": true, "diagnostic": true}
	seen := map[string]bool{}
	for _, toolset := range value.Toolsets {
		if !allowed[toolset] || seen[toolset] {
			return errors.New("mcp toolsets contain an unknown or duplicate value")
		}
		seen[toolset] = true
	}
	if value.MaxMessageBytes < 4096 || value.MaxMessageBytes > 8<<20 {
		return errors.New("mcp maxMessageBytes must be between 4096 and 8388608")
	}
	if value.MaxConcurrentCalls < 1 || value.MaxConcurrentCalls > 32 {
		return errors.New("mcp maxConcurrentCalls must be between 1 and 32")
	}
	timeout, err := time.ParseDuration(value.CallTimeout)
	if err != nil || timeout < time.Second || timeout > 5*time.Minute {
		return errors.New("mcp callTimeout must be between 1s and 5m")
	}
	return nil
}

func environmentAllowed(name string, configured []string) bool {
	if _, ok := allowedEnvironmentSecrets[name]; ok {
		return true
	}
	for _, candidate := range configured {
		if candidate == name {
			return true
		}
	}
	return false
}

func validateWebhooks(webhooks []WebhookSecret, allowlist []string) error {
	if len(webhooks) > 32 || len(allowlist) > 64 || hasDuplicateStrings(allowlist) {
		return errors.New("invalid secret environment allowlist")
	}
	seen := map[string]bool{}
	for _, webhook := range webhooks {
		if webhook.Name == "" || len(webhook.Name) > 64 || strings.ContainsAny(webhook.Name, "\r\n\x00") || seen[webhook.Name] {
			return errors.New("invalid or duplicate Discord webhook name")
		}
		seen[webhook.Name] = true
		if webhook.URL.Source != "env" || !environmentAllowed(webhook.URL.Name, allowlist) {
			return errors.New("Discord webhook URL must use an allowlisted environment reference")
		}
	}
	return nil
}

var validCapabilities = map[string]bool{
	"guild.inspect": true, "channel.read": true, "channel.manage": true, "role.read": true, "role.manage": true,
	"member.read": true, "member.moderate": true, "message.read": true, "message.manage": true, "webhook.read": true,
	"webhook.manage": true, "invite.read": true, "invite.manage": true, "application_command.read": true,
	"application_command.manage": true, "onboarding.read": true, "onboarding.manage": true, "scheduled_event.read": true, "scheduled_event.manage": true,
	"auto_moderation.read": true, "auto_moderation.manage": true,
}

func validatePolicy(policy Policy) error {
	if !validMode(policy.Mode) || len(policy.Actors) > 64 {
		return errors.New("invalid policy mode or actor count")
	}
	actors, principals := map[string]bool{}, map[string]bool{}
	for _, actor := range policy.Actors {
		if !boundedIdentifier(actor.ID, 128) || actors[actor.ID] || len(actor.Principals) == 0 || len(actor.Principals) > 16 || len(actor.Guilds) > 128 || len(actor.Capabilities) > 64 || len(actor.PreapprovedOperations) > 64 || !validMode(actor.Mode) || modeRank(actor.Mode) > modeRank(policy.Mode) {
			return errors.New("invalid or duplicate policy actor")
		}
		actors[actor.ID] = true
		guilds, capabilities, preapproved := map[string]bool{}, map[string]bool{}, map[string]bool{}
		for _, principal := range actor.Principals {
			if !boundedIdentifier(principal.Adapter, 64) || !boundedIdentifier(principal.Subject, 256) || !boundedIdentifier(principal.AuthenticationMethod, 64) {
				return errors.New("invalid policy principal")
			}
			key := principal.Adapter + "\x00" + principal.Subject + "\x00" + principal.AuthenticationMethod
			if principals[key] {
				return errors.New("ambiguous policy principal")
			}
			principals[key] = true
		}
		for _, guild := range actor.Guilds {
			if !validSnowflake(guild) || guilds[guild] {
				return errors.New("invalid or duplicate guild grant")
			}
			guilds[guild] = true
		}
		for _, capability := range actor.Capabilities {
			if !validCapabilities[capability] || strings.Contains(capability, "*") || capabilities[capability] {
				return errors.New("invalid or duplicate capability grant")
			}
			capabilities[capability] = true
		}
		for _, operation := range actor.PreapprovedOperations {
			if !boundedIdentifier(operation, 128) || strings.Contains(operation, "*") || preapproved[operation] {
				return errors.New("invalid or duplicate preapproved operation")
			}
			preapproved[operation] = true
		}
	}
	return nil
}

func validateReconciliation(value Reconciliation) error {
	normalized := strings.ReplaceAll(value.StateDirectory, "\\", "/")
	if normalized == "" || filepath.IsAbs(value.StateDirectory) {
		return errors.New("reconciliation stateDirectory must be relative and contained")
	}
	for _, part := range strings.Split(normalized, "/") {
		if part == "" || part == "." || part == ".." {
			return errors.New("reconciliation stateDirectory must be relative and contained")
		}
	}
	if value.MaxOperations < 1 || value.MaxOperations > 10000 || value.MaxDestructiveOperations < 0 || value.MaxDestructiveOperations > value.MaxOperations {
		return errors.New("invalid reconciliation operation limits")
	}
	if value.RetentionDays < 1 || value.RetentionDays > 3650 {
		return errors.New("invalid reconciliation retention")
	}
	return nil
}

func validateSecuritySettings(approvals Approvals, audit Audit, alerts SecurityAlerts) error {
	ttl, err := time.ParseDuration(approvals.MaxTTL)
	if err != nil || ttl <= 0 || ttl > 24*time.Hour {
		return errors.New("approvals maxTtl must be between zero and 24h")
	}
	normalized := strings.ReplaceAll(audit.Path, "\\", "/")
	if normalized == "" || filepath.IsAbs(audit.Path) {
		return errors.New("audit path must be relative and contained")
	}
	for _, part := range strings.Split(normalized, "/") {
		if part == "" || part == "." || part == ".." {
			return errors.New("audit path must be relative and contained")
		}
	}
	if audit.RetentionDays < 1 || audit.RetentionDays > 3650 || audit.MaxBytes < 64<<10 || audit.MaxBytes > 10<<30 {
		return errors.New("invalid audit retention settings")
	}
	denialWindow, err1 := time.ParseDuration(alerts.DenialWindow)
	destructiveWindow, err2 := time.ParseDuration(alerts.DestructiveWindow)
	if err1 != nil || err2 != nil || alerts.DenialThreshold <= 0 || alerts.DenialThreshold > 10000 || alerts.DestructiveThreshold <= 0 || alerts.DestructiveThreshold > 10000 || denialWindow < time.Second || denialWindow > 24*time.Hour || destructiveWindow < time.Second || destructiveWindow > 24*time.Hour {
		return errors.New("invalid security alert thresholds")
	}
	return nil
}

func validMode(value string) bool {
	return value == "read_only" || value == "dry_run" || value == "apply"
}
func modeRank(value string) int {
	if value == "read_only" {
		return 1
	}
	if value == "dry_run" {
		return 2
	}
	if value == "apply" {
		return 3
	}
	return 99
}
func boundedIdentifier(value string, max int) bool {
	return value != "" && len(value) <= max && strings.TrimSpace(value) == value && !strings.ContainsAny(value, "*\r\n\x00")
}
func validSnowflake(value string) bool {
	if value == "" || value[0] < '1' || value[0] > '9' {
		return false
	}
	for _, r := range value {
		if r < '0' || r > '9' {
			return false
		}
	}
	parsed, err := strconv.ParseUint(value, 10, 64)
	return err == nil && parsed > 0
}
func hasDuplicateStrings(values []string) bool {
	seen := map[string]bool{}
	for _, value := range values {
		if !boundedIdentifier(value, 128) || seen[value] {
			return true
		}
		seen[value] = true
	}
	return false
}

func rejectDuplicateKeys(data []byte) error {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	if err := scanValue(decoder); err != nil {
		return fmt.Errorf("inspect configuration structure: %w", err)
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		return errors.New("configuration contains trailing JSON")
	}
	return nil
}

func scanValue(decoder *json.Decoder) error {
	token, err := decoder.Token()
	if err != nil {
		return err
	}
	delim, ok := token.(json.Delim)
	if !ok {
		return nil
	}
	switch delim {
	case '{':
		seen := map[string]struct{}{}
		for decoder.More() {
			keyToken, err := decoder.Token()
			if err != nil {
				return err
			}
			key, ok := keyToken.(string)
			if !ok {
				return errors.New("object key is not a string")
			}
			if _, exists := seen[key]; exists {
				return fmt.Errorf("duplicate object key %q", key)
			}
			seen[key] = struct{}{}
			if err := scanValue(decoder); err != nil {
				return err
			}
		}
		_, err := decoder.Token()
		return err
	case '[':
		for decoder.More() {
			if err := scanValue(decoder); err != nil {
				return err
			}
		}
		_, err := decoder.Token()
		return err
	default:
		return errors.New("unexpected JSON delimiter")
	}
}
