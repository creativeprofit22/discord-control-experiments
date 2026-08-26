package observability

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
)

func TestOperationalAssetsDeclareSupportedSignalsAndGatewayBoundary(t *testing.T) {
	dashboard, err := os.ReadFile("../../deploy/observability/grafana/dashboard.json")
	if err != nil {
		t.Fatal(err)
	}
	var parsed any
	if err := json.Unmarshal(dashboard, &parsed); err != nil {
		t.Fatalf("invalid dashboard JSON: %v", err)
	}
	rules, err := os.ReadFile("../../deploy/observability/prometheus/rules.yml")
	if err != nil {
		t.Fatal(err)
	}
	combined := string(dashboard) + string(rules)
	for _, signal := range []string{"availability", "latency", "rate-limit", "ack", "queue", "scheduler", "denial", "destructive", "lockdown", "drift", "dead_letter", "redaction", "audit", "export", "build", "Unsupported", "Gateway"} {
		if !strings.Contains(combined, signal) {
			t.Errorf("operational assets missing %q", signal)
		}
	}
}
