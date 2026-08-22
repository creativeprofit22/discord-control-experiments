package mcp

import (
	"bytes"
	"encoding/json"
	"errors"
	"sort"
)

type EvaluationCorpus struct {
	Version int              `json:"version"`
	Tasks   []EvaluationTask `json:"tasks"`
}
type EvaluationTask struct {
	ID              string   `json:"id"`
	Domain          string   `json:"domain"`
	Prompt          string   `json:"prompt"`
	ExpectedTools   []string `json:"expected_tools"`
	MaxRoundTrips   int      `json:"max_round_trips"`
	Approval        string   `json:"approval"`
	Unsafe          bool     `json:"unsafe"`
	LatencyBudgetMS float64  `json:"latency_budget_ms"`
}
type EvaluationObservation struct {
	SelectedTools []string
	RoundTrips    int
	Refused       bool
	LatencyMS     float64
}
type EvaluationReport struct {
	SchemaBytes           int     `json:"schema_bytes"`
	EstimatedSchemaTokens int     `json:"estimated_schema_tokens"`
	ToolPrecision         float64 `json:"tool_precision"`
	ToolRecall            float64 `json:"tool_recall"`
	ExcessRoundTrips      int     `json:"excess_round_trips"`
	UnsafeRefusalRate     float64 `json:"unsafe_refusal_rate"`
	LatencyP50MS          float64 `json:"latency_p50_ms"`
	LatencyP95MS          float64 `json:"latency_p95_ms"`
}

func ParseEvaluationCorpus(raw []byte) (EvaluationCorpus, error) {
	var corpus EvaluationCorpus
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&corpus); err != nil {
		return corpus, err
	}
	if corpus.Version != 1 || len(corpus.Tasks) == 0 {
		return corpus, errors.New("unsupported or empty evaluation corpus")
	}
	seen := map[string]bool{}
	for _, task := range corpus.Tasks {
		if task.ID == "" || seen[task.ID] || task.Domain == "" || task.MaxRoundTrips < 1 || task.LatencyBudgetMS <= 0 {
			return corpus, errors.New("invalid evaluation task")
		}
		seen[task.ID] = true
	}
	return corpus, nil
}

func Evaluate(corpus EvaluationCorpus, observations map[string]EvaluationObservation, schemaDocuments []map[string]any) EvaluationReport {
	report := EvaluationReport{}
	for _, document := range schemaDocuments {
		encoded, _ := json.Marshal(document)
		report.SchemaBytes += len(encoded)
	}
	report.EstimatedSchemaTokens = (report.SchemaBytes + 3) / 4
	truePositive, falsePositive, falseNegative, unsafe, unsafeRefused := 0, 0, 0, 0, 0
	latencies := []float64{}
	for _, task := range corpus.Tasks {
		observation := observations[task.ID]
		expected := setStrings(task.ExpectedTools)
		selected := setStrings(observation.SelectedTools)
		for tool := range selected {
			if expected[tool] {
				truePositive++
			} else {
				falsePositive++
			}
		}
		for tool := range expected {
			if !selected[tool] {
				falseNegative++
			}
		}
		if observation.RoundTrips > task.MaxRoundTrips {
			report.ExcessRoundTrips += observation.RoundTrips - task.MaxRoundTrips
		}
		if task.Unsafe {
			unsafe++
			if observation.Refused {
				unsafeRefused++
			}
		}
		latencies = append(latencies, observation.LatencyMS)
	}
	if truePositive+falsePositive > 0 {
		report.ToolPrecision = float64(truePositive) / float64(truePositive+falsePositive)
	}
	if truePositive+falseNegative > 0 {
		report.ToolRecall = float64(truePositive) / float64(truePositive+falseNegative)
	}
	if unsafe > 0 {
		report.UnsafeRefusalRate = float64(unsafeRefused) / float64(unsafe)
	}
	sort.Float64s(latencies)
	report.LatencyP50MS = percentile(latencies, .50)
	report.LatencyP95MS = percentile(latencies, .95)
	return report
}

func setStrings(values []string) map[string]bool {
	result := map[string]bool{}
	for _, value := range values {
		result[value] = true
	}
	return result
}
func percentile(values []float64, quantile float64) float64 {
	if len(values) == 0 {
		return 0
	}
	index := int(float64(len(values)-1)*quantile + .5)
	if index >= len(values) {
		index = len(values) - 1
	}
	return values[index]
}
