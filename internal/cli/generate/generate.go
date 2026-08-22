package generate

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/ken-kai/discord-control-experiments/internal/cli"
)

func Generate(root string) error {
	definitions := cli.CommandDefinitions()
	sort.Slice(definitions, func(i, j int) bool { return definitions[i].Path < definitions[j].Path })
	artifacts := map[string][]byte{}
	artifacts["docs/cli/reference.md"] = []byte(reference(definitions))
	artifacts["docs/cli/json-v1.md"] = []byte(jsonDoc())
	artifacts["docs/cli/exit-codes.md"] = []byte(exitCodes())
	artifacts["docs/cli/completion.md"] = []byte("# Shell completion\n\nGenerate completion with `discord-control completion bash|zsh|fish|powershell`. Completion is generated from the same immutable command tree as help and the parser.\n")
	artifacts["docs/cli/examples.md"] = []byte("# Examples\n\n```console\ndiscord-control version --format json\ndiscord-control inspect --config config.json --guild 123 --format json\ndiscord-control message create --config config.json --guild 123 --channel 456 --input - --non-interactive true --format json\n```\n\nComplex or sensitive payloads use `--input` files or the explicit `-` stdin sentinel. Tokens, webhook URLs, message bodies, attachments, and approval secrets are never argv flags.\n")
	artifacts["docs/cli/recipes.md"] = []byte("# Operator recipes\n\n## Build server\n\nUse `--non-interactive true --format json` with contained input files. Destructive work requires a valid single-use approval reference or an exact policy-preapproved operation.\n\n## Emergency recovery\n\nInspect `job get`, confirm the plan-hash prefix, then use `job resume --plan-hash FULL_HASH`. Reconciliation lock, binding, deletion, and approval checks still apply.\n")
	artifacts["docs/cli/troubleshooting.md"] = []byte("# Troubleshooting\n\nEvery result carries a correlation ID. Keep it when investigating stderr diagnostics. Exit 2 means input; 3 policy/approval; 4 Discord; 5 reconciliation/conflict; 6 timeout; 7 internal; 130 interruption. Raw Discord bodies, local paths, content, tokens, webhook URLs, approval IDs, and audit reasons are intentionally omitted.\n")
	artifacts["docs/cli/credentials.md"] = []byte("# Credentials\n\nBot and webhook credentials are resolved only through allowlisted environment references in configuration. Never place tokens, webhook URLs, approval secrets, message content, or attachment bytes in argv. Restrict configuration, input, state, audit, and diagnostic files to the operator account.\n")
	artifacts["docs/cli/recovery.md"] = []byte("# Recovery\n\n`job list` and `job get` expose bounded durable metadata derived from saved plans and hash-validated journals. `job cancel` persists cooperative cancellation intent. `job resume` delegates to reconciliation recovery and requires an exact full plan hash; partial or uncertain operations remain conflict-gated.\n")
	schema, err := schemaJSON(definitions)
	if err != nil {
		return err
	}
	artifacts["docs/cli/schema/v1.schema.json"] = schema
	help, _ := cli.Help("")
	artifacts["internal/cli/testdata/help/root.txt"] = []byte(help)
	for _, definition := range definitions {
		help, err := cli.Help(definition.Path)
		if err != nil {
			return err
		}
		artifacts["internal/cli/testdata/help/"+strings.ReplaceAll(definition.Path, " ", "_")+".txt"] = []byte(help)
	}
	for _, shell := range []string{"bash", "zsh", "fish", "powershell"} {
		artifacts["internal/cli/testdata/completion/"+shell+".txt"] = []byte(cli.Completion(shell) + "\n")
	}
	artifacts["internal/cli/testdata/compat/v1/success.json"] = []byte("{\"apiVersion\":\"discord-control.cli/v1\",\"kind\":\"Version\",\"correlationId\":\"00000000000000000000000000000000\",\"result\":{\"version\":\"dev\",\"commit\":\"unknown\",\"apiVersion\":\"discord-control.cli/v1\"}}\n")
	artifacts["internal/cli/testdata/compat/v1/error.json"] = []byte("{\"apiVersion\":\"discord-control.cli/v1\",\"kind\":\"Error\",\"correlationId\":\"00000000000000000000000000000000\",\"error\":{\"code\":\"input_invalid\",\"message\":\"command input is invalid\"}}\n")
	for path, payload := range artifacts {
		target := filepath.Join(root, filepath.FromSlash(path))
		if err := os.MkdirAll(filepath.Dir(target), 0o700); err != nil {
			return err
		}
		if err := os.WriteFile(target, payload, 0o600); err != nil {
			return err
		}
	}
	return nil
}

func reference(definitions []cli.Definition) string {
	var b strings.Builder
	b.WriteString("# CLI command reference\n\nGenerated from `internal/cli` definitions. Do not edit by hand.\n\n")
	for _, d := range definitions {
		fmt.Fprintf(&b, "## `%s`\n\n%s\n\n- Handler: `%s`\n- Output kind: `%s`\n- Timeout: `%s`\n- Mutation: `%t`; destructive: `%t`\n", d.Path, d.Summary, d.Handler, d.OutputKind, d.TimeoutClass, d.Mutation, d.Destructive)
		if len(d.Flags) > 0 {
			b.WriteString("- Flags:\n")
			for _, f := range d.Flags {
				requirement := "optional"
				if f.Required {
					requirement = "required"
				}
				fmt.Fprintf(&b, "  - `%s` (%s, %s)\n", f.Name, f.Value, requirement)
			}
		}
		b.WriteString("\n")
	}
	return strings.TrimRight(b.String(), "\n") + "\n"
}
func jsonDoc() string {
	return "# JSON API v1\n\nEvery invocation emits one `discord-control.cli/v1` envelope. Field order is `apiVersion`, `kind`, `correlationId`, then `result` or `error`, followed by optional `timing` and `recovery`. JSON writes one document to stdout; sanitized diagnostics and logs use stderr. Within v1, only additive optional fields are compatible. Removals, renames, type changes, enum semantic changes, and changed omission behavior require a new API version.\n"
}
func exitCodes() string {
	return "# Exit codes\n\n| Code | Meaning |\n|---:|---|\n| 0 | Success |\n| 2 | Parse or input failure |\n| 3 | Policy or approval denial |\n| 4 | Discord failure |\n| 5 | Reconciliation or conflict |\n| 6 | Bounded timeout |\n| 7 | Internal failure |\n| 130 | Operator interruption |\n\nClassification uses typed errors (`errors.Is`/`errors.As`), never error text.\n"
}
func schemaJSON(definitions []cli.Definition) ([]byte, error) {
	paths := map[string]any{}
	for _, d := range definitions {
		flags := map[string]any{}
		required := []string{}
		for _, f := range d.Flags {
			name := strings.TrimPrefix(f.Name, "--")
			entry := map[string]any{"type": "string"}
			if f.Value == cli.ValueBool {
				entry["type"] = "boolean"
			}
			if len(f.Enum) > 0 {
				entry["enum"] = f.Enum
			}
			flags[name] = entry
			if f.Required {
				required = append(required, name)
			}
		}
		paths[d.Path] = map[string]any{"type": "object", "additionalProperties": false, "properties": flags, "required": required}
	}
	schema := map[string]any{"$schema": "https://json-schema.org/draft/2020-12/schema", "$id": "https://discord-control.local/schema/cli/v1", "title": "discord-control CLI v1", "oneOf": []any{map[string]any{"$ref": "#/$defs/success"}, map[string]any{"$ref": "#/$defs/failure"}}, "$defs": map[string]any{"base": map[string]any{"type": "object", "required": []string{"apiVersion", "kind", "correlationId"}, "properties": map[string]any{"apiVersion": map[string]any{"const": cli.APIVersion}, "kind": map[string]any{"type": "string"}, "correlationId": map[string]any{"type": "string", "minLength": 1}}}, "success": map[string]any{"allOf": []any{map[string]any{"$ref": "#/$defs/base"}, map[string]any{"type": "object", "required": []string{"result"}, "properties": map[string]any{"result": map[string]any{}}}}}, "failure": map[string]any{"allOf": []any{map[string]any{"$ref": "#/$defs/base"}, map[string]any{"type": "object", "required": []string{"error"}, "properties": map[string]any{"error": map[string]any{"type": "object", "required": []string{"code", "message"}, "properties": map[string]any{"code": map[string]any{"enum": []string{"input_invalid", "policy_denied", "approval_required", "discord_failed", "reconciliation_conflict", "timeout", "interrupted", "internal"}}, "message": map[string]any{"type": "string"}, "retryable": map[string]any{"type": "boolean"}}}}}}}, "commandInputs": map[string]any{"type": "object", "properties": paths}}}
	payload, err := json.MarshalIndent(schema, "", "  ")
	return append(payload, '\n'), err
}
