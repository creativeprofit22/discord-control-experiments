package main

import (
	"testing"
	"time"
)

func TestSoakIgnoresLifetimePeakAndGatesFlatSteadyState(t *testing.T) {
	limits := budgets{
		"peak_rss_bytes":               {Limit: 100},
		"memory_growth_bytes_per_hour": {Limit: 10},
	}
	samples := []memorySample{
		{elapsed: 0, rss: 50, heapAlloc: 20},
		{elapsed: time.Second, rss: 55, heapAlloc: 20},
		{elapsed: 2 * time.Second, rss: 52, heapAlloc: 20},
	}

	measurements := memoryMeasurements("soak", 200, samples)
	if _, exists := measurements["peak_rss_bytes"]; exists {
		t.Fatal("soak must not gate the lifetime peak")
	}
	if got := measurements["steady_state_rss_bytes"]; got != 55 {
		t.Fatalf("steady-state RSS = %v, want 55", got)
	}
	if !passesBudgets("soak", measurements, limits) {
		t.Fatal("an early transient peak must not fail a flat soak")
	}

	quick := memoryMeasurements("quick", 200, nil)
	if passesBudgets("quick", quick, limits) {
		t.Fatal("quick mode must retain the lifetime peak-RSS gate")
	}
}

func TestSoakFailsSustainedHeapGrowth(t *testing.T) {
	limits := budgets{
		"peak_rss_bytes":               {Limit: 100},
		"memory_growth_bytes_per_hour": {Limit: 1000},
	}
	samples := []memorySample{
		{elapsed: 0, rss: 50, heapAlloc: 10},
		{elapsed: time.Second, rss: 50, heapAlloc: 20},
		{elapsed: 2 * time.Second, rss: 50, heapAlloc: 30},
	}

	measurements := memoryMeasurements("soak", 50, samples)
	if got := measurements["memory_growth_bytes_per_hour"]; got != 36000 {
		t.Fatalf("memory-growth slope = %v, want 36000", got)
	}
	if passesBudgets("soak", measurements, limits) {
		t.Fatal("sustained heap growth must fail the soak budget")
	}
}
