package docs

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestReleaseWorkflowContract(t *testing.T) {
	payload, err := os.ReadFile(filepath.Join("..", "..", ".github", "workflows", "release.yml"))
	if err != nil {
		t.Fatal(err)
	}
	workflow := string(payload)
	for _, required := range []string{
		"(cd dist && sha256sum smoke-*.json >> SHA256SUMS)",
		`gh attestation verify "dist/$name"`,
		`gh release create "$GITHUB_REF_NAME" --draft --verify-tag --generate-notes`,
		`gh release upload "$GITHUB_REF_NAME" dist/*`,
		`gh release edit "$GITHUB_REF_NAME" --draft=false`,
	} {
		if !strings.Contains(workflow, required) {
			t.Errorf("release workflow lacks %q", required)
		}
	}
	if strings.Contains(workflow, "sha256sum dist/smoke-*.json") {
		t.Error("release checksum manifest must not contain dist-prefixed paths")
	}
}
