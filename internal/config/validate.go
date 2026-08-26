package config

import (
	"bytes"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
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
	if err := validateInteractions(file.Interactions, file.SecretEnvironmentAllowlist); err != nil {
		return err
	}
	if err := validateAutomation(file.Automation, file.SecretEnvironmentAllowlist); err != nil {
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
	if err := validateModeration(*file.Moderation); err != nil {
		return err
	}
	if ratio := file.Observability.TraceSampleRatio; ratio <= 0 || ratio > 1 {
		return errors.New("observability traceSampleRatio must be greater than 0 and at most 1")
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

func validateAutomation(value Automation, allowlist []string) error {
	if value.Enabled == nil || value.Summarizer.Enabled == nil {
		return errors.New("automation enabled settings are required")
	}
	poll, e1 := time.ParseDuration(value.PollInterval)
	lease, e2 := time.ParseDuration(value.LeaseDuration)
	if e1 != nil || poll < time.Second || poll > time.Hour || e2 != nil || lease < 10*time.Second || lease > time.Hour {
		return errors.New("invalid automation scheduler timing")
	}
	if value.WorkerCount < 1 || value.WorkerCount > 32 || value.MaxWorkflowBytes < 1024 || value.MaxWorkflowBytes > 1<<20 || value.MaxSteps < 1 || value.MaxSteps > 100 || value.MaxGuildsPerRun < 1 || value.MaxGuildsPerRun > 16 || value.MaxActionsPerRun < 1 || value.MaxActionsPerRun > 10000 || value.MaxDestructivePerRun < 0 || value.MaxDestructivePerRun > value.MaxActionsPerRun {
		return errors.New("invalid automation limits")
	}
	if len(value.WorkflowPaths) > 128 || len(value.Sources) > 64 || len(value.CleanupChannels) > 256 {
		return errors.New("automation paths, sources, or cleanup bindings exceed limits")
	}
	seenPaths, seenSources := map[string]bool{}, map[string]bool{}
	for _, binding := range value.CleanupChannels {
		parts := strings.Split(binding, ":")
		if len(parts) != 2 || !validSnowflake(parts[0]) || !validSnowflake(parts[1]) {
			return errors.New("cleanup channels must be guildId:channelId bindings")
		}
	}
	for _, path := range value.WorkflowPaths {
		normalized := strings.ReplaceAll(path, "\\", "/")
		if normalized == "" || filepath.IsAbs(path) || seenPaths[normalized] {
			return errors.New("automation workflow paths must be distinct, relative, and contained")
		}
		for _, part := range strings.Split(normalized, "/") {
			if part == "" || part == "." || part == ".." {
				return errors.New("automation workflow paths must be distinct, relative, and contained")
			}
		}
		seenPaths[normalized] = true
	}
	for _, source := range value.Sources {
		if !boundedIdentifier(source.Name, 64) || seenSources[source.Name] || source.MaxBodyBytes < 1024 || source.MaxBodyBytes > 8<<20 {
			return errors.New("invalid or duplicate automation source")
		}
		seenSources[source.Name] = true
		switch source.Kind {
		case "rss":
			u, err := url.Parse(source.URL)
			if err != nil || u.Scheme != "https" || u.Host == "" || u.User != nil || u.Fragment != "" {
				return errors.New("RSS source must be an explicit HTTPS URL without credentials or fragment")
			}
			if source.GitHubOwner != "" || source.GitHubRepo != "" {
				return errors.New("RSS source forbids GitHub fields")
			}
		case "github_releases":
			if source.URL != "" || !boundedIdentifier(source.GitHubOwner, 100) || !boundedIdentifier(source.GitHubRepo, 100) {
				return errors.New("GitHub release source requires fixed owner and repo")
			}
		default:
			return errors.New("unknown automation source kind")
		}
	}
	if *value.Enabled && len(value.WorkflowPaths) == 0 {
		return errors.New("enabled automation requires workflow paths")
	}
	if *value.Summarizer.Enabled {
		if !boundedIdentifier(value.Summarizer.Model, 128) || value.Summarizer.APIKey.Source != "env" || !environmentAllowed(value.Summarizer.APIKey.Name, allowlist) {
			return errors.New("enabled summarizer requires model and allowlisted environment API key")
		}
	}
	return nil
}

func validateInteractions(value Interactions, allowlist []string) error {
	if value.Enabled == nil {
		return errors.New("interactions enabled field is required")
	}
	if value.Path != "/interactions" {
		return errors.New("interactions path must be /interactions")
	}
	if value.MaxBodyBytes < 1024 || value.MaxBodyBytes > 1<<20 {
		return errors.New("interactions maxBodyBytes must be between 1024 and 1048576")
	}
	if value.MaxConcurrentRequests < 1 || value.MaxConcurrentRequests > 1024 || value.QueueCapacity < 1 || value.QueueCapacity > 4096 || value.WorkerCount < 1 || value.WorkerCount > 128 {
		return errors.New("invalid interactions concurrency limits")
	}
	ack, e1 := time.ParseDuration(value.AckDeadline)
	work, e2 := time.ParseDuration(value.WorkTimeout)
	callback, e3 := time.ParseDuration(value.CallbackTimeout)
	dedup, e4 := time.ParseDuration(value.DedupTTL)
	skew, e5 := time.ParseDuration(value.TimestampSkew)
	if e1 != nil || ack < 100*time.Millisecond || ack >= 3*time.Second || e2 != nil || work < time.Second || work > 15*time.Minute || e3 != nil || callback < time.Second || callback > time.Minute || e4 != nil || dedup < 15*time.Minute || dedup > 24*time.Hour || e5 != nil || skew < time.Minute || skew > 15*time.Minute {
		return errors.New("invalid interactions timeout limits")
	}
	if !*value.Enabled {
		if len(value.SigningPublicKeys) > 0 && len(value.SigningPublicKeys) > 2 {
			return errors.New("interactions accepts at most two signing public keys")
		}
		return nil
	}
	if !validSnowflake(value.ApplicationID) {
		return errors.New("enabled interactions requires a valid applicationId")
	}
	if len(value.SigningPublicKeys) < 1 || len(value.SigningPublicKeys) > 2 {
		return errors.New("enabled interactions requires one or two signing public keys")
	}
	seenKeys := map[string]bool{}
	for _, encoded := range value.SigningPublicKeys {
		decoded, err := hex.DecodeString(encoded)
		if err != nil || len(decoded) != 32 || strings.ToLower(encoded) != encoded || seenKeys[encoded] {
			return errors.New("interactions signing public keys must be distinct lowercase 64-character hex")
		}
		seenKeys[encoded] = true
	}
	if value.Registration.Scope == "guild" {
		if !validSnowflake(value.Registration.GuildID) {
			return errors.New("guild interaction registration requires a valid guildId")
		}
	} else if value.Registration.Scope == "global" {
		if value.Registration.GuildID != "" {
			return errors.New("global interaction registration forbids guildId")
		}
	} else {
		return errors.New("enabled interactions registration scope must be guild or global")
	}
	host, _, err := net.SplitHostPort(value.ListenAddress)
	if err != nil || host == "" {
		return errors.New("interactions listenAddress must include an explicit host and port")
	}
	if value.TLS.Mode == "direct" {
		if value.TLS.Certificate.Source != "env" || value.TLS.PrivateKey.Source != "env" || !environmentAllowed(value.TLS.Certificate.Name, allowlist) || !environmentAllowed(value.TLS.PrivateKey.Name, allowlist) {
			return errors.New("direct interactions TLS requires allowlisted environment certificate and private key references")
		}
		if len(value.TLS.TrustedProxyCIDRs) != 0 {
			return errors.New("direct interactions TLS forbids trusted proxy CIDRs")
		}
	} else if value.TLS.Mode == "trusted_proxy" {
		if len(value.TLS.TrustedProxyCIDRs) < 1 || len(value.TLS.TrustedProxyCIDRs) > 32 {
			return errors.New("trusted proxy interactions TLS requires one to 32 CIDRs")
		}
		seen := map[string]bool{}
		for _, cidr := range value.TLS.TrustedProxyCIDRs {
			if _, network, err := net.ParseCIDR(cidr); err != nil || network.String() != cidr || seen[cidr] {
				return errors.New("invalid or duplicate trusted proxy CIDR")
			}
			seen[cidr] = true
		}
		if value.TLS.Certificate.Name != "" || value.TLS.PrivateKey.Name != "" {
			return errors.New("trusted proxy mode forbids direct TLS secrets")
		}
	} else {
		return errors.New("enabled interactions TLS mode must be direct or trusted_proxy")
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
		if (webhook.GuildID == "") != (webhook.ChannelID == "") || (webhook.GuildID != "" && (!validSnowflake(webhook.GuildID) || !validSnowflake(webhook.ChannelID))) {
			return errors.New("Discord webhook metadata requires valid guildId and channelId together")
		}
	}
	return nil
}

var validCapabilities = map[string]bool{
	"guild.inspect": true, "channel.read": true, "channel.manage": true, "role.read": true, "role.manage": true,
	"member.read": true, "member.moderate": true, "message.read": true, "message.manage": true, "webhook.read": true,
	"webhook.manage": true, "invite.read": true, "invite.manage": true, "application_command.read": true,
	"application_command.manage": true, "onboarding.read": true, "onboarding.manage": true, "scheduled_event.read": true, "scheduled_event.manage": true,
	"auto_moderation.read": true, "auto_moderation.manage": true, "job.read": true, "job.cancel": true, "job.resume": true,
	"moderation.case.read": true, "moderation.case.write": true, "moderation.evidence.write": true, "moderation.evidence.export": true,
	"moderation.warn": true, "moderation.timeout": true, "moderation.kick": true, "moderation.ban": true,
	"moderation.message_delete": true, "moderation.quarantine": true, "moderation.invite_control": true,
	"moderation.escalation.evaluate": true, "moderation.lockdown.plan": true, "moderation.lockdown.apply": true, "moderation.lockdown.recover": true,
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

func validateModeration(value Moderation) error {
	if !boundedIdentifier(value.PolicyVersion, 64) || value.MaxCasesPerGuild < 1 || value.MaxCasesPerGuild > 100000 || value.MaxActionsPerCase < 1 || value.MaxActionsPerCase > 1000 || value.MaxActionsPerSubjectWindow < 1 || value.MaxActionsPerSubjectWindow > 1000 || value.MaxActionsPerGuildWindow < value.MaxActionsPerSubjectWindow || value.MaxActionsPerGuildWindow > 10000 {
		return errors.New("invalid moderation policy version or caps")
	}
	retention, retentionErr := time.ParseDuration(value.Retention)
	planTTL, planTTLErr := time.ParseDuration(value.LockdownPlanTTL)
	if retentionErr != nil || retention < 24*time.Hour || retention > 10*365*24*time.Hour || planTTLErr != nil || planTTL < time.Minute || planTTL > time.Hour {
		return errors.New("invalid moderation retention or lockdown plan TTL")
	}
	if value.MaxLockdownOperations < 1 || value.MaxLockdownOperations > 1000 || value.MaxLockdownDestructiveOperations < 1 || value.MaxLockdownDestructiveOperations > value.MaxLockdownOperations {
		return errors.New("invalid lockdown operation caps")
	}
	if value.QuarantineRoleID != "" && !validSnowflake(value.QuarantineRoleID) {
		return errors.New("moderation quarantineRoleId is invalid")
	}
	if len(value.ProtectedOperatorRoleIDs) > 20 || len(value.ReasonCodes) > 128 || len(value.EscalationRules) > 100 {
		return errors.New("moderation lists exceed configured limits")
	}
	seenReasons := map[string]bool{}
	for _, reason := range value.ReasonCodes {
		if !validReasonCode(reason) || seenReasons[reason] {
			return errors.New("invalid or duplicate moderation reason code")
		}
		seenReasons[reason] = true
	}
	seenRoles := map[string]bool{}
	for _, roleID := range value.ProtectedOperatorRoleIDs {
		if !validSnowflake(roleID) || seenRoles[roleID] {
			return errors.New("invalid or duplicate protected operator role")
		}
		seenRoles[roleID] = true
	}
	if value.QuarantineRoleID != "" && seenRoles[value.QuarantineRoleID] {
		return errors.New("quarantine role cannot be a protected operator role")
	}
	seenRules := map[string]bool{}
	for i, rule := range value.EscalationRules {
		if !boundedIdentifier(rule.ID, 64) || seenRules[rule.ID] || rule.Priority < 0 || rule.Priority > 10000 || rule.MinimumCount < 1 || rule.MinimumCount > value.MaxActionsPerSubjectWindow || rule.MaximumApplications < 1 || rule.MaximumApplications > value.MaxActionsPerSubjectWindow || len(rule.EligiblePriorKinds) == 0 || len(rule.EligiblePriorKinds) > 12 || !validModerationAction(rule.Action) {
			return errors.New("invalid or duplicate moderation escalation rule")
		}
		seenRules[rule.ID] = true
		lookback, e1 := time.ParseDuration(rule.Lookback)
		duration, e2 := optionalDuration(rule.Duration)
		deleteWindow, e3 := optionalDuration(rule.DeleteWindow)
		if e1 != nil || lookback < time.Minute || lookback > 365*24*time.Hour || e2 != nil || duration < 0 || duration > 28*24*time.Hour || e3 != nil || deleteWindow < 0 || deleteWindow > 14*24*time.Hour {
			return errors.New("invalid moderation escalation duration")
		}
		if rule.Action == "timeout" && duration <= 0 {
			return errors.New("timeout escalation requires duration")
		}
		seenKinds := map[string]bool{}
		for _, kind := range rule.EligiblePriorKinds {
			if !validModerationAction(kind) || seenKinds[kind] {
				return errors.New("invalid or duplicate escalation prior action")
			}
			seenKinds[kind] = true
		}
		for _, prior := range value.EscalationRules[:i] {
			if prior.Priority == rule.Priority && stringSetsOverlap(prior.EligiblePriorKinds, rule.EligiblePriorKinds) {
				return errors.New("equal-priority escalation rules must not overlap")
			}
		}
	}
	return nil
}

func optionalDuration(value string) (time.Duration, error) {
	if value == "" {
		return 0, nil
	}
	return time.ParseDuration(value)
}

func validReasonCode(value string) bool {
	if value == "" || len(value) > 64 {
		return false
	}
	for _, r := range value {
		if !(r >= 'a' && r <= 'z') && !(r >= '0' && r <= '9') && r != '_' && r != '-' && r != '.' {
			return false
		}
	}
	return true
}

func validModerationAction(value string) bool {
	switch value {
	case "warn", "timeout", "timeout_remove", "kick", "ban", "unban", "message_delete", "bulk_message_delete", "quarantine_add", "quarantine_remove", "invite_create", "invite_revoke":
		return true
	default:
		return false
	}
}

func stringSetsOverlap(left, right []string) bool {
	set := make(map[string]struct{}, len(left))
	for _, value := range left {
		set[value] = struct{}{}
	}
	for _, value := range right {
		if _, ok := set[value]; ok {
			return true
		}
	}
	return false
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
	thresholds := []int{alerts.DenialThreshold, alerts.DestructiveThreshold, alerts.SignatureFailureThreshold, alerts.AckDeadlineMissThreshold, alerts.CallbackFailureThreshold, alerts.HandlerSaturationThreshold, alerts.ModerationDestructiveThreshold, alerts.LockdownPartialFailureThreshold}
	windows := []string{alerts.DenialWindow, alerts.DestructiveWindow, alerts.SignatureFailureWindow, alerts.AckDeadlineMissWindow, alerts.CallbackFailureWindow, alerts.HandlerSaturationWindow, alerts.ModerationDestructiveWindow, alerts.LockdownPartialFailureWindow}
	for i, threshold := range thresholds {
		window, err := time.ParseDuration(windows[i])
		if err != nil || threshold <= 0 || threshold > 10000 || window < time.Second || window > 24*time.Hour {
			return errors.New("invalid security alert thresholds")
		}
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
