package mcp

import "testing"

func TestValidTraceContextRejectsMalformedAndBoundsValues(t *testing.T) {
	valid := map[string]any{
		"traceparent": "00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01",
		"tracestate":  "vendor=value",
		"baggage":     "tenant=correlation-only",
	}
	if got := ValidTraceContext(valid); len(got) != 3 {
		t.Fatalf("valid context rejected: %#v", got)
	}
	invalid := map[string]any{
		"traceparent": "00-00000000000000000000000000000000-0000000000000000-01",
		"tracestate":  string(make([]byte, maxTraceState+1)),
		"baggage":     string([]byte{0x00}),
	}
	if got := ValidTraceContext(invalid); len(got) != 0 {
		t.Fatalf("invalid context accepted: %#v", got)
	}
}
