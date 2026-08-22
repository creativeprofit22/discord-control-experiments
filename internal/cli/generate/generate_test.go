package generate

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestGeneratedArtifactsCurrent(t *testing.T) {
	root := t.TempDir()
	if err := Generate(root); err != nil {
		t.Fatal(err)
	}
	repository := filepath.Clean(filepath.Join("..", "..", ".."))
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			return nil
		}
		relative, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		generated, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		committed, err := os.ReadFile(filepath.Join(repository, relative))
		if err != nil {
			t.Errorf("missing generated artifact %s: %v", relative, err)
			return nil
		}
		if !bytes.Equal(generated, committed) {
			t.Errorf("generated artifact differs: %s", relative)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestJSONSchemaFixtures(t *testing.T) {
	root := t.TempDir()
	if err := Generate(root); err != nil {
		t.Fatal(err)
	}
	payload, err := os.ReadFile(filepath.Join(root, "docs", "cli", "schema", "v1.schema.json"))
	if err != nil {
		t.Fatal(err)
	}
	var schema map[string]any
	if err := json.Unmarshal(payload, &schema); err != nil {
		t.Fatal(err)
	}
	if schema["$schema"] == nil || schema["$defs"] == nil {
		t.Fatal("schema lacks versioned definitions")
	}
}
func TestCompletionFixtures(t *testing.T) {
	root := t.TempDir()
	if err := Generate(root); err != nil {
		t.Fatal(err)
	}
	for _, shell := range []string{"bash", "zsh", "fish", "powershell"} {
		payload, err := os.ReadFile(filepath.Join(root, "internal", "cli", "testdata", "completion", shell+".txt"))
		if err != nil || len(payload) == 0 {
			t.Fatalf("%s completion: %v", shell, err)
		}
	}
}
