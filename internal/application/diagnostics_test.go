package application

import (
	"bytes"
	"testing"
)

func TestDiagnosticBundleSanitization(t *testing.T) {
	const canary = "token-content-webhook-canary"
	service := NewDiagnosticBundleService()
	input := DiagnosticInput{Build: map[string]string{"version": canary, "commit": canary}, ConfigChecks: []HealthCheck{{Name: "state", Status: "failed", Detail: canary}}, Health: HealthResult{Status: "degraded", Checks: []HealthCheck{{Name: "discord", Status: "failed", Detail: canary}}}, Jobs: []JobRecord{{RunID: canary, GuildID: "1", PlanHashPrefix: canary, FailureClass: canary}}, FailureClasses: map[string]int{canary: 1}}
	first, err := service.Build(input)
	if err != nil {
		t.Fatal(err)
	}
	second, err := service.Build(input)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(first, second) {
		t.Fatal("diagnostic ZIP is not deterministic")
	}
	if bytes.Contains(first, []byte(canary)) {
		t.Fatal("canary leaked into diagnostic ZIP")
	}
	if len(first) > 4<<20 {
		t.Fatal("diagnostic ZIP exceeds bound")
	}
}
