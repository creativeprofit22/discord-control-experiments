package docs

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

var required = []string{
	"docs/overview/product.md", "docs/architecture/index.md", "docs/architecture/modules.md",
	"docs/adr/README.md", "docs/adr/0001-runtime-selection.md", "docs/adr/0002-control-surface-boundaries.md", "docs/adr/0003-dependency-update-policy.md",
	"docs/reference/configuration.md", "docs/security/model.md", "docs/operations/operator-guide.md",
	"docs/contributing/contributor-guide.md", "docs/testing/strategy.md", "docs/observability/guide.md",
	"docs/runbooks/README.md", "docs/runbooks/startup-failure.md",
}

var markdownLink = regexp.MustCompile(`\[[^]]*\]\(([^)]+)\)`)

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
		if !strings.Contains(text, "Owner: ") || !strings.Contains(text, "Last reviewed: 2026-08-21") {
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
	index, err := os.ReadFile(filepath.Join(root, "docs", "adr", "README.md"))
	if err == nil {
		for _, adr := range []string{"0001-runtime-selection.md", "0002-control-surface-boundaries.md", "0003-dependency-update-policy.md"} {
			if !strings.Contains(string(index), adr) {
				t.Errorf("ADR index does not link %s", adr)
			}
		}
	}
}
