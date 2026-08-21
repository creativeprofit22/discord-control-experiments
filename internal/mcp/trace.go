package mcp

import "regexp"

const (
	maxTraceState = 512
	maxBaggage    = 1024
)

var (
	traceParentPattern = regexp.MustCompile(`^00-[0-9a-f]{32}-[0-9a-f]{16}-[0-9a-f]{2}$`)
	traceStatePattern  = regexp.MustCompile(`^[\x21-\x2b\x2d-\x7e ]*$`)
	baggagePattern     = regexp.MustCompile(`^[\x20-\x7e]*$`)
)

func ValidTraceContext(meta map[string]any) map[string]string {
	result := map[string]string{}
	if value, ok := meta["traceparent"].(string); ok && traceParentPattern.MatchString(value) && value[3:35] != "00000000000000000000000000000000" && value[36:52] != "0000000000000000" {
		result["traceparent"] = value
	}
	if value, ok := meta["tracestate"].(string); ok && len(value) <= maxTraceState && traceStatePattern.MatchString(value) {
		result["tracestate"] = value
	}
	if value, ok := meta["baggage"].(string); ok && len(value) <= maxBaggage && baggagePattern.MatchString(value) {
		result["baggage"] = value
	}
	return result
}
