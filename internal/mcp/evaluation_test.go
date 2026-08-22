package mcp

import (
	_ "embed"
	"testing"
)

//go:embed testdata/evaluation.v1.json
var evaluationCorpusJSON []byte

func TestEvaluationCorpusCoversDomainsAndReportsDeterministically(t *testing.T) {
	corpus, err := ParseEvaluationCorpus(evaluationCorpusJSON)
	if err != nil {
		t.Fatal(err)
	}
	domains := map[string]bool{}
	observations, err := runDeterministicEvaluation(corpus)
	if err != nil {
		t.Fatal(err)
	}
	for _, task := range corpus.Tasks {
		domains[task.Domain] = true
	}
	for _, domain := range []string{"build", "moderation", "news", "diagnostic"} {
		if !domains[domain] {
			t.Fatalf("missing %s", domain)
		}
	}
	server, err := New(testOptions(&fakeOperator{}))
	if err != nil {
		t.Fatal(err)
	}
	documents := []map[string]any{}
	for _, tool := range server.Inventory() {
		documents = append(documents, tool.Input.Document)
	}
	first := Evaluate(corpus, observations, documents)
	second := Evaluate(corpus, observations, documents)
	if first != second || first.SchemaBytes <= 0 || first.EstimatedSchemaTokens != (first.SchemaBytes+3)/4 || first.ToolPrecision != 1 || first.ToolRecall != 1 || first.UnsafeRefusalRate != 1 || first.ExcessRoundTrips != 0 || first.LatencyP50MS <= 0 || first.LatencyP95MS < first.LatencyP50MS {
		t.Fatalf("report %#v second %#v", first, second)
	}
}
