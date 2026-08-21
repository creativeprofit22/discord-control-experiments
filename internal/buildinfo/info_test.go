package buildinfo

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPinnedDependenciesMatchRuntimeDecision(t *testing.T) {
	root := filepath.Clean(filepath.Join("..", ".."))
	files := map[string][]string{
		"go.mod": {"go 1.27.0", "github.com/bwmarrin/discordgo v0.29.0", "github.com/modelcontextprotocol/go-sdk v1.7.0", "go.opentelemetry.io/otel v1.44.0"},
		"benchmarks/runtime/typescript-candidate/package.json": {`"@modelcontextprotocol/server": "2.0.0"`, `"discord.js": "14.27.0"`, `"typescript": "6.0.3"`, `"packageManager": "pnpm@10.26.1"`},
		"docs/adr/0001-runtime-selection.md":                   {"Select Go", "Go | yes | 1.061063", "v0.29.0", "v1.7.0", "14.27.0", "2.0.0"},
	}
	for name, expected := range files {
		content, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(name)))
		if err != nil {
			t.Fatal(err)
		}
		for _, value := range expected {
			if !strings.Contains(string(content), value) {
				t.Errorf("%s does not contain pin %q", name, value)
			}
		}
	}
	for _, lock := range []string{"go.sum", "benchmarks/runtime/go-candidate/go.sum", "benchmarks/runtime/typescript-candidate/pnpm-lock.yaml"} {
		info, err := os.Stat(filepath.Join(root, filepath.FromSlash(lock)))
		if err != nil || info.Size() == 0 {
			t.Errorf("missing checksum lock %s", lock)
		}
	}
	resultBytes, err := os.ReadFile(filepath.Join(root, "benchmarks", "runtime", "results", "2026-08-21-windows-amd64.json"))
	if err != nil {
		t.Fatal(err)
	}
	var result struct {
		Selected       string `json:"selected"`
		SourceRevision string `json:"sourceRevision"`
	}
	if err := json.Unmarshal(resultBytes, &result); err != nil {
		t.Fatal(err)
	}
	if result.Selected != "go" || len(result.SourceRevision) != 40 {
		t.Fatalf("invalid selected runtime evidence: %#v", result)
	}
}

func TestBuildInfoHasNoWallClockTimestamp(t *testing.T) {
	info := Current()
	encoded, err := json.Marshal(info)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(strings.ToLower(string(encoded)), "timestamp") || strings.Contains(strings.ToLower(string(encoded)), "builtat") {
		t.Fatalf("build metadata includes wall-clock field: %s", encoded)
	}
}
