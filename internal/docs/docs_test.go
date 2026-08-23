package docs

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

var required = []string{
	"docs/overview/product.md", "docs/architecture/index.md", "docs/architecture/modules.md",
	"docs/adr/README.md", "docs/adr/0001-runtime-selection.md", "docs/adr/0002-control-surface-boundaries.md", "docs/adr/0003-dependency-update-policy.md", "docs/adr/0004-discord-rest-transport.md",
	"docs/reference/configuration.md", "docs/reference/discord-rest-core.md", "docs/reference/capability-policy.md", "docs/reference/discord-permissions.md", "docs/reference/approval-model.md", "docs/reference/audit-retention.md", "docs/security/model.md", "docs/operations/operator-guide.md",
	"docs/contributing/contributor-guide.md", "docs/testing/strategy.md", "docs/testing/discord-fixtures.md", "docs/testing/security-catalog.md", "docs/observability/guide.md",
	"docs/runbooks/README.md", "docs/runbooks/startup-failure.md", "docs/runbooks/secret-rotation.md", "docs/runbooks/security-incident.md",
	"docs/reconciliation/schema-v1.md", "docs/reconciliation/migrations.md", "docs/reconciliation/snapshots.md", "docs/reconciliation/planning.md", "docs/reconciliation/recovery.md",
}

var markdownLink = regexp.MustCompile(`\[[^]]*\]\(([^)]+)\)`)

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
		expectedReviewDate := "2026-08-21"
		if relative == "docs/reference/configuration.md" || relative == "docs/reference/capability-policy.md" {
			expectedReviewDate = "2026-08-22"
		}
		if relative == "docs/adr/0003-dependency-update-policy.md" {
			expectedReviewDate = "2026-08-23"
		}
		if !strings.Contains(text, "Owner: ") || !strings.Contains(text, "Last reviewed: "+expectedReviewDate) {
			t.Errorf("%s lacks current ownership metadata", relative)
		}
		for _, match := range markdownLink.FindAllStringSubmatch(text, -1) {
			target := strings.Split(match[1], "#")[0]
			if target == "" || strings.Contains(target, "://") || strings.HasPrefix(target, "mailto:") {
				continue
			}
			resolved := filepath.Clean(filepath.Join(filepath.Dir(path), filepath.FromSlash(target)))
			rel, err := filepath.Rel(root, resolved)
			if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
				t.Errorf("%s link escapes repository: %s", relative, target)
				continue
			}
			if _, err := os.Stat(resolved); err != nil {
				t.Errorf("%s has broken link %s", relative, target)
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
