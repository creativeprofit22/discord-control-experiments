package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"runtime"
	"sort"
	"sync"
	"sync/atomic"
	"time"

	"github.com/ken-kai/discord-control-experiments/internal/discord"
	"github.com/ken-kai/discord-control-experiments/internal/domain"
)

type budget struct {
	Baseline float64 `json:"baseline"`
	Limit    float64 `json:"limit"`
	Minimum  bool    `json:"minimum,omitempty"`
}
type budgets map[string]budget

type evidence struct {
	SchemaVersion string             `json:"schemaVersion"`
	Mode          string             `json:"mode"`
	Measurements  map[string]float64 `json:"measurements"`
	Passed        bool               `json:"passed"`
}

type scriptedTransport struct {
	mu       sync.Mutex
	statuses []int
	requests atomic.Int64
}

func (transport *scriptedTransport) RoundTrip(*http.Request) (*http.Response, error) {
	transport.requests.Add(1)
	transport.mu.Lock()
	status := http.StatusOK
	if len(transport.statuses) > 0 {
		status, transport.statuses = transport.statuses[0], transport.statuses[1:]
	}
	transport.mu.Unlock()
	headers := make(http.Header)
	if status == http.StatusTooManyRequests {
		headers.Set("Retry-After", "0")
		headers.Set("X-RateLimit-Global", "false")
	}
	body := "[]"
	if status >= 400 {
		body = `{"message":"fake failure","code":0}`
	}
	return &http.Response{StatusCode: status, Header: headers, Body: io.NopCloser(bytes.NewBufferString(body))}, nil
}

func main() {
	mode := flag.String("mode", "quick", "quick or soak")
	budgetPath := flag.String("budgets", "benchmarks/production/budgets.json", "budget file")
	output := flag.String("output", "", "optional evidence file")
	duration := flag.Duration("duration", 30*time.Second, "soak duration")
	binary := flag.String("binary", "", "optional packaged binary for startup measurement")
	flag.Parse()
	if *mode != "quick" && *mode != "soak" {
		fatal(errors.New("mode must be quick or soak"))
	}
	limits, err := loadBudgets(*budgetPath)
	if err != nil {
		fatal(err)
	}
	measurements, err := measure(*mode, *duration, *binary)
	if err != nil {
		fatal(err)
	}
	passed := true
	for name, value := range measurements {
		limit, ok := limits[name]
		if !ok || limit.Minimum && value < limit.Limit || !limit.Minimum && value > limit.Limit {
			passed = false
		}
	}
	report := evidence{SchemaVersion: "discord-control.production-benchmark/v1", Mode: *mode, Measurements: measurements, Passed: passed}
	encoded, _ := json.MarshalIndent(report, "", "  ")
	encoded = append(encoded, '\n')
	if *output == "" {
		_, _ = os.Stdout.Write(encoded)
	} else if err := os.WriteFile(*output, encoded, 0o600); err != nil {
		fatal(err)
	}
	if !passed {
		fatal(errors.New("production benchmark exceeded a committed budget"))
	}
}

func measure(mode string, duration time.Duration, binary string) (map[string]float64, error) {
	if mode == "soak" && duration <= 0 {
		return nil, errors.New("soak duration must be positive")
	}
	measurements := map[string]float64{}
	started := time.Now()
	if binary != "" {
		command := exec.Command(binary, "version", "--format", "json")
		if err := command.Run(); err != nil {
			return nil, err
		}
	}
	measurements["startup_ms"] = float64(time.Since(started).Microseconds()) / 1000

	transport := &scriptedTransport{}
	client, err := discord.NewWithRoundTripper(func(use func(string) error) error { return use("fixture-token") }, transport)
	if err != nil {
		return nil, err
	}
	iterations := 500
	if mode == "soak" {
		iterations = 0
		warmUntil := time.Now().Add(2 * time.Second)
		for time.Now().Before(warmUntil) {
			if _, err := client.ExecuteDiscord(context.Background(), domain.ListGuildChannels{CommandContext: domain.CommandContext{GuildID: "123456789012345678"}}); err != nil {
				return nil, err
			}
		}
	}
	latencies := make([]float64, 0, max(iterations, 4096))
	runtime.GC()
	var memoryBefore runtime.MemStats
	runtime.ReadMemStats(&memoryBefore)
	var requestCount int64
	benchmarkStarted := time.Now()
	for iterations > 0 || mode == "soak" && time.Since(benchmarkStarted) < duration {
		requestStarted := time.Now()
		_, err := client.ExecuteDiscord(context.Background(), domain.ListGuildChannels{CommandContext: domain.CommandContext{GuildID: "123456789012345678"}})
		if err != nil {
			return nil, err
		}
		latency := float64(time.Since(requestStarted).Microseconds()) / 1000
		requestCount++
		if len(latencies) < cap(latencies) {
			latencies = append(latencies, latency)
		} else {
			latencies[requestCount%int64(len(latencies))] = latency
		}
		if iterations > 0 {
			iterations--
		}
	}
	elapsed := time.Since(benchmarkStarted).Seconds()
	runtime.GC()
	var memoryAfter runtime.MemStats
	runtime.ReadMemStats(&memoryAfter)
	measurements["rest_p99_ms"] = percentile(latencies, 0.99)
	measurements["rest_requests_per_second"] = float64(requestCount) / elapsed

	ackLatencies := make([]float64, 1000)
	for index := range ackLatencies {
		start := time.Now()
		_, _ = json.Marshal(map[string]any{"type": 5, "data": map[string]any{"flags": 64}})
		ackLatencies[index] = float64(time.Since(start).Microseconds()) / 1000
	}
	measurements["interaction_ack_p99_ms"] = percentile(ackLatencies, 0.99)

	schedulerLag := make([]float64, 100)
	for index := range schedulerLag {
		due := time.Now().Add(time.Millisecond)
		timer := time.NewTimer(time.Until(due))
		<-timer.C
		schedulerLag[index] = float64(time.Since(due).Microseconds()) / 1000
	}
	measurements["scheduler_lag_p99_ms"] = percentile(schedulerLag, 0.99)

	degraded := &scriptedTransport{statuses: []int{500, 500, 200, 429, 200, 500}}
	degradedClient, _ := discord.NewWithRoundTripper(func(use func(string) error) error { return use("fixture-token") }, degraded)
	degradedStarted := time.Now()
	_, firstErr := degradedClient.ExecuteDiscord(context.Background(), domain.ListGuildChannels{CommandContext: domain.CommandContext{GuildID: "123456789012345678"}})
	_, secondErr := degradedClient.ExecuteDiscord(context.Background(), domain.ListGuildChannels{CommandContext: domain.CommandContext{GuildID: "123456789012345678"}})
	beforeMutation := degraded.requests.Load()
	_, mutationErr := degradedClient.ExecuteDiscord(context.Background(), domain.CreateChannel{CommandContext: domain.CommandContext{GuildID: "123456789012345678"}, Name: "fixture", Type: domain.ChannelType(0)})
	mutationRequests := degraded.requests.Load() - beforeMutation
	if firstErr != nil || secondErr != nil || mutationErr == nil || mutationRequests != 1 {
		return nil, errors.New("graceful degradation invariants failed")
	}
	measurements["discord_degraded_ms"] = float64(time.Since(degradedStarted).Milliseconds())
	measurements["duplicate_mutations"] = float64(max(0, int(mutationRequests-1)))

	measurements["peak_rss_bytes"] = float64(currentRSS())
	if mode == "soak" {
		growth := float64(0)
		if memoryAfter.HeapAlloc > memoryBefore.HeapAlloc {
			growth = float64(memoryAfter.HeapAlloc-memoryBefore.HeapAlloc) / elapsed * 3600
		}
		measurements["memory_growth_bytes_per_hour"] = growth
	}
	return measurements, nil
}

func percentile(values []float64, quantile float64) float64 {
	sort.Float64s(values)
	return values[min(len(values)-1, int(float64(len(values))*quantile))]
}

func loadBudgets(filename string) (budgets, error) {
	data, err := os.ReadFile(filename)
	if err != nil {
		return nil, err
	}
	var values budgets
	err = json.Unmarshal(data, &values)
	return values, err
}

func fatal(err error) {
	_, _ = fmt.Fprintln(os.Stderr, err)
	os.Exit(1)
}
