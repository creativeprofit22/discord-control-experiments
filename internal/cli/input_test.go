package cli

import (
	"bytes"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type strictFixture struct {
	Name string `json:"name"`
}

func TestStrictInputLimits(t *testing.T) {
	var value strictFixture
	if err := DecodeStrictJSON(strings.NewReader(`{"name":"ok"}`), 13, &value); err != nil {
		t.Fatal(err)
	}
	if err := DecodeStrictJSON(strings.NewReader(`{"name":"too-big"}`), 4, &value); err == nil {
		t.Fatal("expected size rejection")
	}
	if err := DecodeStrictJSON(bytes.NewReader([]byte{0xff}), 10, &value); err == nil {
		t.Fatal("expected UTF-8 rejection")
	}
}

func TestMalformedInput(t *testing.T) {
	inputs := []string{`{"name":"a","name":"b"}`, `{"unknown":true}`, `{"name":"ok"} {}`, `{"name":`}
	for _, input := range inputs {
		var value strictFixture
		if err := DecodeStrictJSON(strings.NewReader(input), 1024, &value); err == nil {
			t.Fatalf("accepted %s", input)
		}
	}
}

func TestContainedAtomicOutput(t *testing.T) {
	root := t.TempDir()
	if _, err := ContainedPath(root, filepath.Join("..", "escape"), true); err == nil {
		t.Fatal("accepted escaping path")
	}
	if err := AtomicWrite(root, "state/result.json", 0o600, func(writer io.Writer) error { _, err := writer.Write([]byte("ok")); return err }); err != nil {
		t.Fatal(err)
	}
	payload, err := os.ReadFile(filepath.Join(root, "state", "result.json"))
	if err != nil || string(payload) != "ok" {
		t.Fatalf("payload=%q err=%v", payload, err)
	}
	if err := AtomicWrite(root, "unsafe", 0o644, func(writer io.Writer) error { return nil }); err == nil {
		t.Fatal("accepted broad permissions")
	}
}

func TestExplicitStdinOnly(t *testing.T) {
	reader, err := OpenInput(t.TempDir(), "-", strings.NewReader("{}"))
	if err != nil {
		t.Fatal(err)
	}
	_ = reader.Close()
	_, err = OpenInput(t.TempDir(), "-", nil)
	var parse *ParseError
	if !errors.As(err, &parse) {
		t.Fatalf("error=%v", err)
	}
}
