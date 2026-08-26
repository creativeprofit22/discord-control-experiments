package docs

import (
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"
	"unicode"
)

var required = []string{
	"docs/overview/product.md", "docs/architecture/index.md", "docs/architecture/modules.md",
	"docs/adr/README.md", "docs/adr/0001-runtime-selection.md", "docs/adr/0002-control-surface-boundaries.md", "docs/adr/0003-dependency-update-policy.md", "docs/adr/0004-discord-rest-transport.md",
	"docs/reference/configuration.md", "docs/reference/discord-rest-core.md", "docs/reference/capability-policy.md", "docs/reference/discord-permissions.md", "docs/reference/approval-model.md", "docs/reference/audit-retention.md", "docs/security/model.md", "docs/operations/operator-guide.md",
	"docs/contributing/contributor-guide.md", "docs/testing/strategy.md", "docs/testing/discord-fixtures.md", "docs/testing/security-catalog.md", "docs/observability/guide.md", "docs/observability/slos.md",
	"docs/operations/deployment.md", "docs/operations/upgrade.md", "docs/operations/troubleshooting.md", "docs/reference/api-discord.md", "docs/security/readiness-review.md",
	"docs/runbooks/README.md", "docs/runbooks/startup-failure.md", "docs/runbooks/discord-outage.md", "docs/runbooks/stuck-workflow.md", "docs/runbooks/reconciliation-recovery.md", "docs/runbooks/audit-investigation.md", "docs/runbooks/readiness-drills.md", "docs/runbooks/secret-rotation.md", "docs/runbooks/security-incident.md",
	"docs/reconciliation/schema-v1.md", "docs/reconciliation/schema-v2.md", "docs/reconciliation/migrations.md", "docs/reconciliation/snapshots.md", "docs/reconciliation/planning.md", "docs/reconciliation/recovery.md",
	"docs/moderation/guide.md", "docs/moderation/action-matrix.md", "docs/moderation/automod-reference.md", "docs/moderation/escalation-policy.md", "docs/moderation/evidence-privacy-retention.md", "docs/moderation/appeal-export.md", "docs/moderation/lockdown-checklist.md", "docs/moderation/runbook.md",
	"docs/automation/schema.md", "docs/automation/sources-and-ai.md", "docs/automation/runtime.md", "docs/automation/recipes.md", "docs/automation/operations.md",
}

var (
	markdownLink = regexp.MustCompile(`\[[^]]*\]\(([^)]+)\)`)
	reviewDate   = regexp.MustCompile(`(?m)^Last reviewed: (\d{4}-\d{2}-\d{2})\s*$`)
	heading      = regexp.MustCompile(`(?m)^#{1,6}\s+(.+?)\s*$`)
)

func TestCLIDocumentationContract(t *testing.T) {
	root := filepath.Clean(filepath.Join("..", ".."))
	for _, relative := range []string{"docs/cli/reference.md", "docs/cli/json-v1.md", "docs/cli/exit-codes.md", "docs/cli/completion.md", "docs/cli/examples.md", "docs/cli/recipes.md", "docs/cli/troubleshooting.md", "docs/cli/credentials.md", "docs/cli/recovery.md", "docs/cli/schema/v1.schema.json"} {
		payload, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(relative)))
		if err != nil || len(payload) == 0 {
			t.Errorf("CLI document %s: %v", relative, err)
		}
		if strings.HasSuffix(relative, ".json") {
			var document any
			if json.Unmarshal(payload, &document) != nil {
				t.Errorf("invalid CLI JSON schema %s", relative)
			}
		}
	}
}

func TestDocumentationContract(t *testing.T) {
	root := filepath.Clean(filepath.Join("..", ".."))
	for _, relative := range required {
		path := filepath.Join(root, filepath.FromSlash(relative))
		content, err := os.ReadFile(path)
		if err != nil {
			t.Errorf("required document %s: %v", relative, err)
			continue
		}
		text := string(content)
		if !strings.Contains(text, "Owner: ") {
			t.Errorf("%s lacks ownership metadata", relative)
		}
		match := reviewDate.FindStringSubmatch(text)
		if len(match) != 2 {
			t.Errorf("%s lacks a parseable review date", relative)
		} else if reviewed, parseErr := time.Parse("2006-01-02", match[1]); parseErr != nil || reviewed.After(time.Now().UTC().Add(24*time.Hour)) || reviewed.Before(time.Now().UTC().AddDate(0, 0, -120)) {
			t.Errorf("%s review date %q is outside the 120-day freshness window", relative, match[1])
		}
		for _, link := range markdownLink.FindAllStringSubmatch(text, -1) {
			if err := validateLocalLink(root, path, link[1]); err != nil {
				t.Errorf("%s: %v", relative, err)
			}
		}
	}
	securityModel, err := os.ReadFile(filepath.Join(root, "docs", "security", "model.md"))
	if err == nil {
		for _, term := range []string{"confused deputy", "Cross-guild substitution", "Approval replay", "Audit fail-open", "Secret disclosure"} {
			if !strings.Contains(string(securityModel), term) {
				t.Errorf("security model lacks %q", term)
			}
		}
	}
	index, err := os.ReadFile(filepath.Join(root, "docs", "adr", "README.md"))
	if err == nil {
		for _, adr := range []string{"0001-runtime-selection.md", "0002-control-surface-boundaries.md", "0003-dependency-update-policy.md", "0004-discord-rest-transport.md"} {
			if !strings.Contains(string(index), adr) {
				t.Errorf("ADR index does not link %s", adr)
			}
		}
	}
	for _, name := range []string{"small", "complex"} {
		for _, suffix := range []string{".json", ".expected.json"} {
			path := filepath.Join(root, "docs", "reconciliation", "examples", name+suffix)
			content, err := os.ReadFile(path)
			if err != nil {
				t.Errorf("required reconciliation example %s: %v", path, err)
				continue
			}
			var document any
			if err := json.Unmarshal(content, &document); err != nil {
				t.Errorf("reconciliation example %s is invalid JSON: %v", path, err)
			}
		}
	}
}

func TestAllMarkdownLocalLinksAndAnchors(t *testing.T) {
	root := filepath.Clean(filepath.Join("..", ".."))
	err := filepath.WalkDir(filepath.Join(root, "docs"), func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".md") {
			return nil
		}
		content, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		for _, link := range markdownLink.FindAllStringSubmatch(string(content), -1) {
			if err := validateLocalLink(root, path, link[1]); err != nil {
				t.Errorf("%s: %v", path, err)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestDocumentedCommandAndSchemaManifests(t *testing.T) {
	root := filepath.Clean(filepath.Join("..", ".."))
	for _, relative := range []string{"docs/testing/commands.json", "docs/cli/schema/v1.schema.json", "deploy/observability/grafana/dashboard.json"} {
		data, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(relative)))
		if err != nil {
			t.Fatal(err)
		}
		var value map[string]any
		if err := json.Unmarshal(data, &value); err != nil {
			t.Fatalf("%s is invalid JSON: %v", relative, err)
		}
		if len(value) == 0 {
			t.Fatalf("%s is empty", relative)
		}
	}
	data, _ := os.ReadFile(filepath.Join(root, "docs", "testing", "commands.json"))
	var commands struct {
		SchemaVersion string `json:"schemaVersion"`
		Commands      []struct {
			Name string   `json:"name"`
			Argv []string `json:"argv"`
		} `json:"commands"`
	}
	if json.Unmarshal(data, &commands) != nil || commands.SchemaVersion != "discord-control.documented-commands/v1" || len(commands.Commands) == 0 {
		t.Fatal("documented command manifest is malformed or unsupported")
	}
	for _, command := range commands.Commands {
		if command.Name == "" || len(command.Argv) == 0 {
			t.Fatal("documented command is incomplete")
		}
	}
}

func validateLocalLink(root, source, raw string) error {
	parsed, err := url.Parse(raw)
	if err != nil {
		return fmt.Errorf("malformed link %q", raw)
	}
	if parsed.IsAbs() || parsed.Scheme == "mailto" {
		return nil
	}
	resolved := source
	if parsed.Path != "" {
		resolved = filepath.Clean(filepath.Join(filepath.Dir(source), filepath.FromSlash(parsed.Path)))
	}
	relative, err := filepath.Rel(root, resolved)
	if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return fmt.Errorf("link escapes repository: %s", raw)
	}
	content, err := os.ReadFile(resolved)
	if err != nil {
		return fmt.Errorf("broken link %s", raw)
	}
	if parsed.Fragment == "" || !strings.HasSuffix(resolved, ".md") {
		return nil
	}
	fragment, err := url.PathUnescape(parsed.Fragment)
	if err != nil {
		return fmt.Errorf("invalid anchor %s", raw)
	}
	if _, ok := markdownAnchors(string(content))[fragment]; !ok {
		return fmt.Errorf("broken anchor %s", raw)
	}
	return nil
}

func markdownAnchors(content string) map[string]struct{} {
	anchors := map[string]struct{}{}
	counts := map[string]int{}
	for _, match := range heading.FindAllStringSubmatch(content, -1) {
		base := markdownSlug(match[1])
		if base == "" {
			continue
		}
		anchor := base
		if counts[base] > 0 {
			anchor = fmt.Sprintf("%s-%d", base, counts[base])
		}
		counts[base]++
		anchors[anchor] = struct{}{}
	}
	return anchors
}

func markdownSlug(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	var result strings.Builder
	lastHyphen := false
	for _, character := range value {
		switch {
		case unicode.IsLetter(character), unicode.IsNumber(character), character == '_':
			result.WriteRune(character)
			lastHyphen = false
		case unicode.IsSpace(character), character == '-':
			if result.Len() > 0 && !lastHyphen {
				result.WriteByte('-')
				lastHyphen = true
			}
		}
	}
	return strings.Trim(result.String(), "-")
}
