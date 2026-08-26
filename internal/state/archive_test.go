package state

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestBackupIsDeterministicVerifiedAndSecretFree(t *testing.T) {
	root := t.TempDir()
	for name, data := range map[string]string{
		"config.json":               `{"discord":{"token":{"source":"env","name":"DISCORD_BOT_TOKEN"}}}`,
		"workflows/news.json":       `{"version":1}`,
		"audit/events.jsonl":        `{"kind":"decision"}\n`,
		"state/guilds/1/state.json": `{"version":1}`,
	} {
		filename := filepath.Join(root, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(filename), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filename, []byte(data), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	options := BackupOptions{ConfigDirectory: root, ConfigPath: "config.json", WorkflowPaths: []string{"workflows/news.json"}, AuditPath: "audit/events.jsonl", StateDirectory: "state"}
	first, second := filepath.Join(t.TempDir(), "first.zip"), filepath.Join(t.TempDir(), "second.zip")
	options.Destination = first
	manifest, err := CreateBackup(options)
	if err != nil {
		t.Fatal(err)
	}
	options.Destination = second
	if _, err := CreateBackup(options); err != nil {
		t.Fatal(err)
	}
	firstData, _ := os.ReadFile(first)
	secondData, _ := os.ReadFile(second)
	if !bytes.Equal(firstData, secondData) {
		t.Fatal("identical backup inputs produced different archives")
	}
	verified, err := VerifyBackup(first)
	if err != nil || len(verified.Entries) != len(manifest.Entries) {
		t.Fatalf("verify = %#v, %v", verified, err)
	}
	if bytes.Contains(firstData, []byte("resolved-secret-canary")) {
		t.Fatal("backup contains resolved secret")
	}
	staging := filepath.Join(t.TempDir(), "staging")
	if _, err := RestoreBackup(first, staging); err != nil {
		t.Fatal(err)
	}
	restored, err := os.ReadFile(filepath.Join(staging, "state", "guilds", "1", "state.json"))
	if err != nil || string(restored) != `{"version":1}` {
		t.Fatalf("restored state = %q, %v", restored, err)
	}
	if _, err := RestoreBackup(first, staging); err == nil {
		t.Fatal("restore overwrote an existing staging directory")
	}
}

func TestValidateStateTreeRejectsTrailingMalformedData(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "state.json"), []byte(`{} trailing`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := ValidateStateTree(root); err == nil {
		t.Fatal("state validator accepted trailing malformed data")
	}
}

func TestVerifyBackupRejectsTraversalLinksDuplicatesAndDigestMismatch(t *testing.T) {
	tests := []struct {
		name    string
		entries []testZipEntry
	}{
		{name: "traversal", entries: []testZipEntry{{name: "manifest.json", data: manifestFor("../escape")}, {name: "../escape", data: []byte("x")}}},
		{name: "windows traversal", entries: []testZipEntry{{name: "manifest.json", data: manifestFor(`..\\escape`)}, {name: `..\\escape`, data: []byte("x")}}},
		{name: "drive path", entries: []testZipEntry{{name: "manifest.json", data: manifestFor("C:escape")}, {name: "C:escape", data: []byte("x")}}},
		{name: "link", entries: []testZipEntry{{name: "manifest.json", data: manifestFor("link")}, {name: "link", data: []byte("x"), mode: os.ModeSymlink | 0o777}}},
		{name: "duplicate", entries: []testZipEntry{{name: "manifest.json", data: manifestFor("file")}, {name: "file", data: []byte("x")}, {name: "file", data: []byte("x")}}},
		{name: "digest", entries: []testZipEntry{{name: "manifest.json", data: manifestFor("file")}, {name: "file", data: []byte("x")}}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			filename := filepath.Join(t.TempDir(), "unsafe.zip")
			writeTestZip(t, filename, test.entries)
			if _, err := VerifyBackup(filename); err == nil {
				t.Fatal("unsafe archive accepted")
			}
		})
	}
}

type testZipEntry struct {
	name string
	data []byte
	mode os.FileMode
}

func manifestFor(name string) []byte {
	data, _ := json.Marshal(BackupManifest{SchemaVersion: BackupSchemaVersion, Entries: []BackupEntry{{Path: name, Size: 1, SHA256: "00", Mode: 0o600}}})
	return data
}

func FuzzVerifyBackupArchive(f *testing.F) {
	f.Add([]byte("not-a-zip"))
	f.Add([]byte("PK\x05\x06\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00"))
	f.Fuzz(func(t *testing.T, data []byte) {
		if len(data) > 1<<20 {
			return
		}
		filename := filepath.Join(t.TempDir(), "input.zip")
		if err := os.WriteFile(filename, data, 0o600); err != nil {
			t.Fatal(err)
		}
		_, _ = VerifyBackup(filename)
	})
}

func writeTestZip(t *testing.T, filename string, entries []testZipEntry) {
	t.Helper()
	file, err := os.Create(filename)
	if err != nil {
		t.Fatal(err)
	}
	writer := zip.NewWriter(file)
	for _, item := range entries {
		header := &zip.FileHeader{Name: item.name, Method: zip.Store}
		mode := item.mode
		if mode == 0 {
			mode = 0o600
		}
		header.SetMode(mode)
		entry, err := writer.CreateHeader(header)
		if err == nil {
			_, err = entry.Write(item.data)
		}
		if err != nil {
			t.Fatal(err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
}
