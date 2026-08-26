package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"time"
)

type drill struct {
	Name, Package, Test string
}

type result struct {
	Name       string `json:"name"`
	Status     string `json:"status"`
	DurationMS int64  `json:"durationMs"`
}

type evidence struct {
	SchemaVersion string   `json:"schemaVersion"`
	GeneratedAt   string   `json:"generatedAt"`
	Results       []result `json:"results"`
}

var drills = []drill{
	{"backup-restore", "./internal/state", "TestBackupIsDeterministicVerifiedAndSecretFree"},
	{"desired-state-migration", "./internal/reconcile", "TestParseDesiredMigratesV1ToV2WithEmptyAutoModeration"},
	{"config-rollback", "./internal/upgrade", "TestValidateChecksConfigSecretsAuditStateAndBackupCompatibility"},
	{"credential-rotation", "./internal/config", "TestSecretReferencesRotateWithoutExposure"},
	{"discord-outage", "./internal/discord", "TestClientRetries429AndRedactsCapture"},
	{"stuck-workflow", "./internal/application", "TestAutomationDeadLetterAfterRetryExhaustion"},
	{"audit-investigation", "./internal/audit", "TestAuditDetectsTamperReorderAndTruncation"},
	{"callback-webhook-compromise", "./internal/interactions", "TestRealSignedStackCallbackOutageIsContainedAndRedacted"},
	{"gateway-identify-unsupported", "./internal/e2e", "TestGatewaySurfaceRemainsUncomposed"},
}

func main() {
	if len(os.Args) != 2 {
		fail(errors.New("usage: drills EVIDENCE.json"))
	}
	for _, name := range []string{"DISCORD_BOT_TOKEN", "DISCORD_WEBHOOK_URL", "DISCORD_INTERACTIONS_PUBLIC_KEY", "DISCORD_INTERACTIONS_PRIVATE_KEY"} {
		if os.Getenv(name) != "" {
			fail(fmt.Errorf("live Discord credential environment %s must be unset", name))
		}
	}
	goBinary, err := exec.LookPath("go")
	if err != nil {
		fail(err)
	}
	report := evidence{SchemaVersion: "discord-control.drills/v1", GeneratedAt: time.Now().UTC().Format(time.RFC3339)}
	failed := false
	for _, item := range drills {
		started := time.Now()
		command := exec.Command(goBinary, "test", item.Package, "-run", "^"+item.Test+"$", "-count=1")
		command.Env = os.Environ()
		err := command.Run()
		status := "passed"
		if err != nil {
			status, failed = "failed", true
		}
		report.Results = append(report.Results, result{Name: item.Name, Status: status, DurationMS: time.Since(started).Milliseconds()})
	}
	data, err := json.MarshalIndent(report, "", "  ")
	if err == nil {
		data = append(data, '\n')
		err = atomicWrite(os.Args[1], data)
	}
	if err != nil {
		fail(err)
	}
	if failed {
		fail(errors.New("one or more drills failed; inspect evidence"))
	}
}

func atomicWrite(filename string, data []byte) error {
	if err := os.MkdirAll(filepath.Dir(filename), 0o700); err != nil {
		return err
	}
	file, err := os.CreateTemp(filepath.Dir(filename), ".drill-evidence-*")
	if err != nil {
		return err
	}
	name := file.Name()
	defer os.Remove(name)
	writeErr := file.Chmod(0o600)
	if writeErr == nil {
		_, writeErr = file.Write(data)
	}
	err = errors.Join(writeErr, file.Sync(), file.Close())
	if err != nil {
		return err
	}
	if _, err := os.Lstat(filename); err == nil || !os.IsNotExist(err) {
		return errors.New("evidence destination already exists")
	}
	return os.Rename(name, filename)
}

func fail(err error) {
	_, _ = fmt.Fprintln(os.Stderr, err)
	os.Exit(1)
}
