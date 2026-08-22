package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ken-kai/discord-control-experiments/internal/domain"
)

func TestJSONEnvelopeCompatibility(t *testing.T) {
	fixture, err := os.ReadFile(filepath.Join("testdata", "compat", "v1", "success.json"))
	if err != nil {
		t.Fatal(err)
	}
	envelope := SuccessEnvelope("Version", "00000000000000000000000000000000", struct {
		Version    string `json:"version"`
		Commit     string `json:"commit"`
		APIVersion string `json:"apiVersion"`
	}{"dev", "unknown", APIVersion})
	var rendered bytes.Buffer
	if err := Render(&rendered, "json", envelope); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(rendered.Bytes(), fixture) {
		t.Fatalf("rendered=%s fixture=%s", rendered.Bytes(), fixture)
	}
}
func TestHumanRendersSameResult(t *testing.T) {
	result := struct {
		Name string `json:"name"`
	}{"same"}
	envelope := SuccessEnvelope("Fixture", "id", result)
	var human, structured bytes.Buffer
	_ = Render(&human, "human", envelope)
	_ = Render(&structured, "json", envelope)
	if !strings.Contains(human.String(), "same") || !strings.Contains(structured.String(), "same") {
		t.Fatal("renderers did not consume same result")
	}
}
func TestExitCodeContract(t *testing.T) {
	if ExitCode(&ParseError{}) != ExitInput || ExitCode(context.DeadlineExceeded) != ExitTimeout || ExitCode(ErrInterrupted) != ExitInterrupted || ExitCode(errors.New("x")) != ExitInternal {
		t.Fatal("exit mapping changed")
	}
}
func TestCorrelationAndCancellation(t *testing.T) {
	envelope := FailureEnvelope("correlation", ErrInterrupted)
	if envelope.CorrelationID != "correlation" || envelope.Error.Code != ErrorInterrupted {
		t.Fatalf("envelope=%#v", envelope)
	}
}
func TestBoundedTimeouts(t *testing.T) {
	if _, _, err := CommandContext(context.Background(), 31*time.Minute); err == nil {
		t.Fatal("unbounded timeout accepted")
	}
}
func TestParserTables(t *testing.T) {
	for _, definition := range CommandDefinitions() {
		if definition.Path == "" || definition.Handler == "" {
			t.Fatalf("invalid definition %#v", definition)
		}
	}
}
func TestInterruption(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if !errors.Is(NormalizeCancellation(ctx, ctx, ctx.Err()), ErrInterrupted) {
		t.Fatal("signal cancellation not normalized")
	}
}
func TestRedaction(t *testing.T) {
	secret := "token-canary"
	envelope := FailureEnvelope("id", errors.New(secret))
	payload, _ := json.Marshal(envelope)
	if bytes.Contains(payload, []byte(secret)) {
		t.Fatal("error secret leaked")
	}
}
func TestShellMetacharactersAreData(t *testing.T) {
	command, err := Parse([]string{"inspect", "--config", "config.json", "--guild", "123; echo inert"})
	if err != nil {
		t.Fatal(err)
	}
	if command.GuildID != domain.GuildID("123; echo inert") {
		t.Fatal("argument was interpreted")
	}
}
func TestGoldenHelp(t *testing.T) {
	expected, err := os.ReadFile(filepath.Join("testdata", "help", "root.txt"))
	if err != nil {
		t.Fatal(err)
	}
	actual, _ := Help("")
	if actual != string(expected) {
		t.Fatal("root help drift")
	}
}
