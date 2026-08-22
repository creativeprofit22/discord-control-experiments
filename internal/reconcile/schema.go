package reconcile

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/big"
	"regexp"
	"strings"

	"github.com/ken-kai/discord-control-experiments/internal/domain"
)

const (
	MaxDesiredBytes         = 1 << 20
	MaxJSONDepth            = 32
	MaxJSONNodes            = 20000
	MaxResourcesPerKind     = 1000
	MaxOverwritesPerChannel = 100
	MaxOnboardingPrompts    = 50
	MaxOnboardingOptions    = 100
)

var (
	errDesiredTooLarge = errors.New("desired document exceeds size limit")
	logicalIDPattern   = regexp.MustCompile(`^[a-z][a-z0-9_-]{0,63}$`)
	secretKeys         = map[string]struct{}{
		"authorization": {}, "headers": {}, "password": {}, "secret": {}, "token": {},
		"webhooktoken": {}, "webhookurl": {}, "executableurl": {}, "apikey": {}, "privatekey": {},
	}
)

func ParseDesired(data []byte) (domain.DesiredGuild, error) {
	if len(data) > MaxDesiredBytes {
		return domain.DesiredGuild{}, errDesiredTooLarge
	}
	if err := inspectJSON(data); err != nil {
		return domain.DesiredGuild{}, err
	}
	migrated, err := MigrateDesired(data)
	if err != nil {
		return domain.DesiredGuild{}, err
	}
	decoder := json.NewDecoder(bytes.NewReader(migrated))
	decoder.DisallowUnknownFields()
	var desired domain.DesiredGuild
	if err := decoder.Decode(&desired); err != nil {
		return domain.DesiredGuild{}, fmt.Errorf("decode desired document: %w", err)
	}
	if err := requireEOF(decoder); err != nil {
		return domain.DesiredGuild{}, err
	}
	if err := ValidateDesired(desired); err != nil {
		return domain.DesiredGuild{}, err
	}
	return NormalizeDesired(desired), nil
}

func MigrateDesired(data []byte) ([]byte, error) {
	var envelope struct {
		SchemaVersion string `json:"schema_version"`
	}
	if err := json.Unmarshal(data, &envelope); err != nil {
		return nil, fmt.Errorf("decode schema version: %w", err)
	}
	if envelope.SchemaVersion != domain.DeclarativeSchemaV1 {
		return nil, fmt.Errorf("unsupported schema_version %q", envelope.SchemaVersion)
	}
	return append([]byte(nil), data...), nil
}

func ValidateDesired(desired domain.DesiredGuild) error {
	if desired.SchemaVersion != domain.DeclarativeSchemaV1 {
		return fmt.Errorf("schema_version must be %q", domain.DeclarativeSchemaV1)
	}
	if desired.GuildID == "" {
		return errors.New("guild_id is required")
	}
	if strings.TrimSpace(desired.Ownership.Owner) == "" || len(desired.Ownership.Owner) > 128 {
		return errors.New("ownership.owner must contain 1-128 characters")
	}
	counts := []struct {
		name string
		n    int
	}{{"categories", len(desired.Categories)}, {"channels", len(desired.Channels)}, {"roles", len(desired.Roles)}, {"webhooks", len(desired.Webhooks)}, {"application_commands", len(desired.ApplicationCommands)}}
	for _, count := range counts {
		if count.n > MaxResourcesPerKind {
			return fmt.Errorf("%s exceeds limit %d", count.name, MaxResourcesPerKind)
		}
	}

	known := make(map[domain.StableResourceRef]struct{})
	add := func(kind domain.ResourceKind, logicalID string) error {
		if !logicalIDPattern.MatchString(logicalID) {
			return fmt.Errorf("invalid %s logical_id %q", kind, logicalID)
		}
		ref := domain.StableResourceRef{Kind: kind, LogicalID: logicalID}
		if _, exists := known[ref]; exists {
			return fmt.Errorf("duplicate logical reference %s/%s", kind, logicalID)
		}
		known[ref] = struct{}{}
		return nil
	}
	for _, category := range desired.Categories {
		if err := validateMode(category.Mode); err != nil {
			return err
		}
		if err := add(domain.ResourceCategory, category.LogicalID); err != nil {
			return err
		}
		if err := validateName(category.Name, "category"); err != nil {
			return err
		}
	}
	for _, role := range desired.Roles {
		if err := validateMode(role.Mode); err != nil {
			return err
		}
		if err := add(domain.ResourceRole, role.LogicalID); err != nil {
			return err
		}
		if err := validateName(role.Name, "role"); err != nil {
			return err
		}
		if _, err := CanonicalPermission(role.Permissions); err != nil {
			return fmt.Errorf("role %s permissions: %w", role.LogicalID, err)
		}
	}
	for _, channel := range desired.Channels {
		if err := validateMode(channel.Mode); err != nil {
			return err
		}
		if err := add(domain.ResourceChannel, channel.LogicalID); err != nil {
			return err
		}
		if err := validateName(channel.Name, "channel"); err != nil {
			return err
		}
		if len(channel.Overwrites) > MaxOverwritesPerChannel {
			return fmt.Errorf("channel %s permission_overwrites exceeds limit", channel.LogicalID)
		}
		if channel.Category != nil && channel.Category.Kind != domain.ResourceCategory {
			return fmt.Errorf("channel %s category reference has wrong kind", channel.LogicalID)
		}
		for _, overwrite := range channel.Overwrites {
			if overwrite.Target.Kind != domain.ResourceRole {
				return fmt.Errorf("channel %s overwrite target must be a role", channel.LogicalID)
			}
			if _, err := CanonicalPermission(overwrite.Allow); err != nil {
				return fmt.Errorf("channel %s overwrite allow: %w", channel.LogicalID, err)
			}
			if _, err := CanonicalPermission(overwrite.Deny); err != nil {
				return fmt.Errorf("channel %s overwrite deny: %w", channel.LogicalID, err)
			}
		}
	}
	for _, webhook := range desired.Webhooks {
		if err := validateMode(webhook.Mode); err != nil {
			return err
		}
		if err := add(domain.ResourceWebhook, webhook.LogicalID); err != nil {
			return err
		}
		if webhook.Channel.Kind != domain.ResourceChannel {
			return fmt.Errorf("webhook %s channel reference has wrong kind", webhook.LogicalID)
		}
		if err := validateName(webhook.Name, "webhook"); err != nil {
			return err
		}
	}
	for _, command := range desired.ApplicationCommands {
		if err := validateMode(command.Mode); err != nil {
			return err
		}
		if err := add(domain.ResourceApplicationCommand, command.LogicalID); err != nil {
			return err
		}
		if err := validateName(command.Name, "application command"); err != nil {
			return err
		}
		if command.DefaultMemberPermissions != nil {
			if _, err := CanonicalPermission(*command.DefaultMemberPermissions); err != nil {
				return fmt.Errorf("application command %s permissions: %w", command.LogicalID, err)
			}
		}
	}
	if desired.Onboarding != nil {
		if len(desired.Onboarding.Prompts) > MaxOnboardingPrompts {
			return errors.New("onboarding prompts exceed limit")
		}
		for _, prompt := range desired.Onboarding.Prompts {
			if len(prompt.Options) > MaxOnboardingOptions {
				return fmt.Errorf("onboarding prompt %s options exceed limit", prompt.LogicalID)
			}
			if err := add(domain.ResourceOnboarding, prompt.LogicalID); err != nil {
				return err
			}
		}
	}
	return validateReferences(desired, known)
}

func validateReferences(desired domain.DesiredGuild, known map[domain.StableResourceRef]struct{}) error {
	check := func(owner string, ref domain.StableResourceRef) error {
		if _, ok := known[ref]; !ok {
			return fmt.Errorf("%s references unknown %s/%s", owner, ref.Kind, ref.LogicalID)
		}
		return nil
	}
	for _, channel := range desired.Channels {
		if channel.Category != nil {
			if err := check("channel "+channel.LogicalID, *channel.Category); err != nil {
				return err
			}
		}
		for _, overwrite := range channel.Overwrites {
			if err := check("channel "+channel.LogicalID, overwrite.Target); err != nil {
				return err
			}
		}
	}
	for _, webhook := range desired.Webhooks {
		if err := check("webhook "+webhook.LogicalID, webhook.Channel); err != nil {
			return err
		}
	}
	if desired.Onboarding != nil {
		for _, ref := range desired.Onboarding.DefaultChannelIDs {
			if err := check("onboarding", ref); err != nil {
				return err
			}
		}
		for _, prompt := range desired.Onboarding.Prompts {
			for _, option := range prompt.Options {
				for _, ref := range option.Channels {
					if err := check("onboarding option "+option.LogicalID, ref); err != nil {
						return err
					}
				}
				for _, ref := range option.Roles {
					if err := check("onboarding option "+option.LogicalID, ref); err != nil {
						return err
					}
				}
			}
		}
	}
	return nil
}

func validateMode(mode domain.ManagementMode) error {
	if mode == "" || mode == domain.ManagementModeManaged || mode == domain.ManagementModeObserved || mode == domain.ManagementModeIgnored {
		return nil
	}
	return fmt.Errorf("invalid management mode %q", mode)
}

func validateName(name, kind string) error {
	if strings.TrimSpace(name) == "" || len(name) > 100 {
		return fmt.Errorf("%s name must contain 1-100 characters", kind)
	}
	return nil
}

func CanonicalPermission(bits domain.PermissionBits) (domain.PermissionBits, error) {
	text := string(bits)
	if text == "" {
		return "", errors.New("permission value is empty")
	}
	if strings.HasPrefix(text, "+") || strings.HasPrefix(text, "-") {
		return "", errors.New("permission value must be an unsigned decimal integer")
	}
	for _, character := range text {
		if character < '0' || character > '9' {
			return "", errors.New("permission value must contain decimal digits only")
		}
	}
	integer := new(big.Int)
	if _, ok := integer.SetString(text, 10); !ok || integer.Sign() < 0 {
		return "", errors.New("permission value is invalid")
	}
	return domain.PermissionBits(integer.String()), nil
}

func inspectJSON(data []byte) error {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	nodes := 0
	var inspectValue func(int) error
	inspectValue = func(depth int) error {
		if depth > MaxJSONDepth {
			return fmt.Errorf("JSON nesting exceeds limit %d", MaxJSONDepth)
		}
		token, err := decoder.Token()
		if err != nil {
			return fmt.Errorf("decode desired JSON: %w", err)
		}
		nodes++
		if nodes > MaxJSONNodes {
			return fmt.Errorf("JSON values exceed limit %d", MaxJSONNodes)
		}
		delimiter, isDelimiter := token.(json.Delim)
		if !isDelimiter {
			return nil
		}
		switch delimiter {
		case '{':
			seen := make(map[string]struct{})
			for decoder.More() {
				keyToken, err := decoder.Token()
				if err != nil {
					return fmt.Errorf("decode object key: %w", err)
				}
				key, ok := keyToken.(string)
				if !ok {
					return errors.New("JSON object key is not a string")
				}
				if _, duplicate := seen[key]; duplicate {
					return fmt.Errorf("duplicate JSON key %q", key)
				}
				seen[key] = struct{}{}
				if isSecretKey(key) {
					return fmt.Errorf("secret-shaped field %q is forbidden", key)
				}
				if err := inspectValue(depth + 1); err != nil {
					return err
				}
			}
		case '[':
			for decoder.More() {
				if err := inspectValue(depth + 1); err != nil {
					return err
				}
			}
		default:
			return errors.New("unexpected JSON delimiter")
		}
		_, err = decoder.Token()
		return err
	}
	if err := inspectValue(1); err != nil {
		return err
	}
	if decoder.More() {
		return errors.New("multiple JSON values are not allowed")
	}
	return nil
}

func isSecretKey(key string) bool {
	normalized := strings.NewReplacer("_", "", "-", "", ".", "").Replace(strings.ToLower(key))
	_, forbidden := secretKeys[normalized]
	return forbidden
}

func requireEOF(decoder *json.Decoder) error {
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		if err == nil {
			return errors.New("multiple JSON values are not allowed")
		}
		return fmt.Errorf("decode trailing JSON: %w", err)
	}
	return nil
}
