package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	protocolVersion = "2026-07-28"
	coldSamples     = 50
	mcpSamples      = 50
	restWarmups     = 100
	restSamples     = 1000
	restRuns        = 5
	buildSamples    = 5
	processTimeout  = 30 * time.Second
)

type metric struct {
	Samples  []int64 `json:"samples"`
	MedianNS int64   `json:"medianNs"`
	P95NS    int64   `json:"p95Ns"`
}

type artifactMetric struct {
	AppOnlyBytes    int64 `json:"appOnlyBytes"`
	DeployableBytes int64 `json:"deployableBytes"`
}

type candidateResult struct {
	Name            string            `json:"name"`
	RuntimeVersion  string            `json:"runtimeVersion"`
	Dependencies    map[string]string `json:"dependencies"`
	FrozenAudit     string            `json:"frozenAudit"`
	Eligible        bool              `json:"eligible"`
	Failures        []string          `json:"failures"`
	ColdStart       metric            `json:"coldStart"`
	WarmREST        metric            `json:"warmRest"`
	MCPStartup      metric            `json:"mcpStartup"`
	PeakMemoryBytes []uint64          `json:"peakMemoryBytes"`
	Artifact        artifactMetric    `json:"artifact"`
	CleanBuild      metric            `json:"cleanBuild"`
	LeanScore       float64           `json:"leanScore"`
}

type parameters struct {
	ColdSamples  int `json:"coldSamples"`
	MCPStartups  int `json:"mcpStartups"`
	RESTWarmups  int `json:"restWarmupsPerRun"`
	RESTSamples  int `json:"restSamplesPerRun"`
	RESTRuns     int `json:"restRuns"`
	BuildSamples int `json:"buildSamples"`
	TimeoutMS    int `json:"timeoutMs"`
}

type benchmarkResult struct {
	SchemaVersion   int                `json:"schemaVersion"`
	Generated       string             `json:"generated"`
	SourceRevision  string             `json:"sourceRevision"`
	OS              string             `json:"os"`
	Architecture    string             `json:"architecture"`
	CPU             string             `json:"cpu"`
	ToolVersions    map[string]string  `json:"toolVersions"`
	ProtocolVersion string             `json:"protocolVersion"`
	Parameters      parameters         `json:"parameters"`
	Weights         map[string]float64 `json:"weights"`
	Candidates      []candidateResult  `json:"candidates"`
	Selected        string             `json:"selected"`
	TieBreakApplied bool               `json:"tieBreakApplied"`
	Limitations     []string           `json:"limitations"`
}

type candidateCommand struct {
	name string
	path string
	args []string
}

type restOutput struct {
	DurationsNS []int64 `json:"durationsNs"`
	UserID      string  `json:"userId"`
}

func main() {
	output := flag.String("output", "benchmarks/runtime/results/2026-08-21-windows-amd64.json", "result JSON path inside the repository")
	validate := flag.String("validate", "", "validate an existing result instead of benchmarking")
	flag.Parse()

	root, err := repositoryRoot()
	if err != nil {
		fatal(err)
	}
	if *validate != "" {
		path, err := containedPath(root, *validate)
		if err != nil {
			fatal(err)
		}
		if err := validateResultFile(path); err != nil {
			fatal(err)
		}
		fmt.Printf("validated %s\n", path)
		return
	}
	path, err := containedPath(root, *output)
	if err != nil {
		fatal(err)
	}
	if err := runBenchmark(root, path); err != nil {
		fatal(err)
	}
}

func fatal(err error) {
	fmt.Fprintln(os.Stderr, err)
	os.Exit(1)
}

func repositoryRoot() (string, error) {
	root, err := filepath.Abs(".")
	if err != nil {
		return "", err
	}
	if _, err := os.Stat(filepath.Join(root, "benchmarks", "runtime", "toolchains.json")); err != nil {
		return "", errors.New("run the benchmark from the repository root")
	}
	return filepath.Clean(root), nil
}

func containedPath(root, requested string) (string, error) {
	path := requested
	if !filepath.IsAbs(path) {
		path = filepath.Join(root, path)
	}
	path = filepath.Clean(path)
	rel, err := filepath.Rel(root, path)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", errors.New("output path escapes the repository")
	}
	return path, nil
}

func runBenchmark(root, outputPath string) error {
	paths, err := prepare(root)
	if err != nil {
		return err
	}
	fixture, err := os.ReadFile(filepath.Join(root, "benchmarks", "runtime", "fixture", "discord-user.json"))
	if err != nil {
		return err
	}
	fixtureURL, closeServer, err := startFixtureServer(fixture)
	if err != nil {
		return err
	}
	defer closeServer()

	goResult := measureCandidate(paths.goCandidate, fixtureURL)
	tsResult := measureCandidate(paths.tsCandidate, fixtureURL)

	goResult.CleanBuild = measureGoBuilds(paths)
	tsResult.CleanBuild = measureTSBuilds(paths)
	if len(goResult.CleanBuild.Samples) != buildSamples {
		goResult.Failures = append(goResult.Failures, "clean Go build metric incomplete")
	}
	if len(tsResult.CleanBuild.Samples) != buildSamples {
		tsResult.Failures = append(tsResult.Failures, "clean TypeScript build metric incomplete")
	}
	goResult.Eligible = len(goResult.Failures) == 0
	tsResult.Eligible = len(tsResult.Failures) == 0

	results := []candidateResult{goResult, tsResult}
	selected, tie, err := score(results)
	if err != nil {
		return err
	}
	result := benchmarkResult{
		SchemaVersion:  1,
		Generated:      "2026-08-21",
		SourceRevision: gitRevision(root),
		OS:             runtime.GOOS,
		Architecture:   runtime.GOARCH,
		CPU:            cpuDescription(),
		ToolVersions: map[string]string{
			"go":   strings.TrimSpace(commandOutput(root, paths.goTool, "version")),
			"node": strings.TrimSpace(commandOutput(root, paths.nodeTool, "--version")),
			"pnpm": "10.26.1",
		},
		ProtocolVersion: protocolVersion,
		Parameters:      parameters{coldSamples, mcpSamples, restWarmups, restSamples, restRuns, buildSamples, int(processTimeout.Milliseconds())},
		Weights:         map[string]float64{"peakMemory": .25, "deployableArtifact": .20, "coldStart": .20, "mcpStartup": .15, "warmREST": .10, "cleanBuild": .10},
		Candidates:      results,
		Selected:        selected,
		TieBreakApplied: tie,
		Limitations: []string{
			"Single Windows host; background load, filesystem cache, antivirus, and power policy can affect samples.",
			"Loopback REST isolates client overhead and JSON decoding; it does not model Discord network latency or response-driven rate limiting.",
			"Artifact size is uncompressed deployment bytes and is not an installer or transfer-size comparison.",
		},
	}
	if err := os.MkdirAll(filepath.Dir(outputPath), 0o755); err != nil {
		return err
	}
	encoded, err := json.MarshalIndent(result, "", "  ")
	if err != nil {
		return err
	}
	encoded = append(encoded, '\n')
	if err := os.WriteFile(outputPath, encoded, 0o644); err != nil {
		return err
	}
	if err := os.WriteFile(strings.TrimSuffix(outputPath, filepath.Ext(outputPath))+".md", []byte(markdownSummary(result)), 0o644); err != nil {
		return err
	}
	return validateResultFile(outputPath)
}

type preparedPaths struct {
	root        string
	goTool      string
	nodeTool    string
	pnpmScript  string
	goDir       string
	tsDir       string
	workDir     string
	goCandidate candidateCommand
	tsCandidate candidateCommand
}

func prepare(root string) (preparedPaths, error) {
	exe := func(name string) string {
		if runtime.GOOS == "windows" {
			return name + ".exe"
		}
		return name
	}
	paths := preparedPaths{
		root:       root,
		goTool:     filepath.Join(root, ".tools", "go", "bin", exe("go")),
		nodeTool:   filepath.Join(root, ".tools", "node-v24.19.0-win-x64", exe("node")),
		pnpmScript: filepath.Join(root, ".tools", "corepack", "v1", "pnpm", "10.26.1", "bin", "pnpm.cjs"),
		goDir:      filepath.Join(root, "benchmarks", "runtime", "go-candidate"),
		tsDir:      filepath.Join(root, "benchmarks", "runtime", "typescript-candidate"),
		workDir:    filepath.Join(root, "benchmarks", "runtime", "work"),
	}
	for _, required := range []string{paths.goTool, paths.nodeTool, paths.pnpmScript} {
		if _, err := os.Stat(required); err != nil {
			return paths, fmt.Errorf("missing pinned tool %s: %w", required, err)
		}
	}
	if err := os.RemoveAll(paths.workDir); err != nil {
		return paths, err
	}
	if err := os.MkdirAll(paths.workDir, 0o755); err != nil {
		return paths, err
	}
	if err := runCommand(paths.goDir, 2*time.Minute, nil, paths.goTool, "mod", "download"); err != nil {
		return paths, fmt.Errorf("prefetch Go dependencies: %w", err)
	}
	if err := runCommand(paths.tsDir, 3*time.Minute, nil, paths.nodeTool, paths.pnpmScript, "install", "--frozen-lockfile", "--ignore-scripts"); err != nil {
		return paths, fmt.Errorf("frozen TypeScript install: %w", err)
	}
	if err := runCommand(paths.tsDir, 2*time.Minute, nil, paths.nodeTool, paths.pnpmScript, "audit", "--prod", "--audit-level", "high"); err != nil {
		return paths, fmt.Errorf("TypeScript production audit: %w", err)
	}

	goBinary := filepath.Join(paths.workDir, exe("go-candidate"))
	if err := runCommand(paths.goDir, 2*time.Minute, nil, paths.goTool, "build", "-trimpath", "-ldflags=-s -w", "-o", goBinary, "."); err != nil {
		return paths, err
	}
	if err := runCommand(paths.tsDir, 2*time.Minute, nil, paths.nodeTool, filepath.Join(paths.tsDir, "node_modules", "typescript", "bin", "tsc"), "-p", "tsconfig.json"); err != nil {
		return paths, err
	}

	deploy := filepath.Join(paths.workDir, "ts-deploy")
	if err := os.MkdirAll(deploy, 0o755); err != nil {
		return paths, err
	}
	for _, name := range []string{"package.json", "pnpm-lock.yaml"} {
		if err := copyFile(filepath.Join(paths.tsDir, name), filepath.Join(deploy, name)); err != nil {
			return paths, err
		}
	}
	if err := runCommand(deploy, 3*time.Minute, nil, paths.nodeTool, paths.pnpmScript, "install", "--prod", "--frozen-lockfile", "--ignore-scripts"); err != nil {
		return paths, fmt.Errorf("production deployment install: %w", err)
	}

	goSize, err := fileSize(goBinary)
	if err != nil {
		return paths, err
	}
	tsApp, err := directorySize(filepath.Join(paths.tsDir, "dist"))
	if err != nil {
		return paths, err
	}
	tsDeps, err := directorySize(filepath.Join(deploy, "node_modules"))
	if err != nil {
		return paths, err
	}
	nodeRuntime, err := directorySize(filepath.Dir(paths.nodeTool))
	if err != nil {
		return paths, err
	}
	paths.goCandidate = candidateCommand{"go", goBinary, nil}
	paths.tsCandidate = candidateCommand{"typescript", paths.nodeTool, []string{filepath.Join(paths.tsDir, "dist", "main.js")}}
	artifactCache.Store("go", artifactMetric{AppOnlyBytes: goSize, DeployableBytes: goSize})
	artifactCache.Store("typescript", artifactMetric{AppOnlyBytes: tsApp, DeployableBytes: tsApp + tsDeps + nodeRuntime})
	return paths, nil
}

var artifactCache sync.Map

func measureCandidate(candidate candidateCommand, fixtureURL string) candidateResult {
	result := candidateResult{
		Name:         candidate.name,
		Dependencies: map[string]string{},
		FrozenAudit:  "passed",
	}
	if candidate.name == "go" {
		result.RuntimeVersion = "go1.27.0"
		result.Dependencies["github.com/bwmarrin/discordgo"] = "v0.29.0"
		result.Dependencies["github.com/modelcontextprotocol/go-sdk"] = "v1.7.0"
	} else {
		result.RuntimeVersion = "node-v24.19.0"
		result.Dependencies["@modelcontextprotocol/server"] = "2.0.0"
		result.Dependencies["discord.js"] = "14.27.0"
		result.Dependencies["typescript"] = "6.0.3"
	}
	if artifact, ok := artifactCache.Load(candidate.name); ok {
		result.Artifact = artifact.(artifactMetric)
	}

	for i := 0; i < coldSamples; i++ {
		duration, line, extra, err := runLine(candidate, []string{"cold"}, "")
		if err != nil {
			result.Failures = append(result.Failures, fmt.Sprintf("cold start %d: %v", i+1, err))
			break
		}
		var ready struct {
			Ready           bool   `json:"ready"`
			ProtocolVersion string `json:"protocolVersion"`
		}
		if json.Unmarshal(line, &ready) != nil || !ready.Ready || ready.ProtocolVersion != protocolVersion || strings.TrimSpace(extra) != "" {
			result.Failures = append(result.Failures, fmt.Sprintf("cold start %d returned malformed or extra stdout", i+1))
			break
		}
		result.ColdStart.Samples = append(result.ColdStart.Samples, duration.Nanoseconds())
	}
	result.ColdStart = summarize(result.ColdStart.Samples)

	discover := `{"jsonrpc":"2.0","id":1,"method":"server/discover","params":{"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientInfo":{"name":"benchmark-runner","version":"1.0.0"},"io.modelcontextprotocol/clientCapabilities":{}}}}` + "\n"
	for i := 0; i < mcpSamples; i++ {
		duration, line, extra, err := runLine(candidate, []string{"mcp"}, discover)
		if err != nil {
			result.Failures = append(result.Failures, fmt.Sprintf("MCP startup %d: %v", i+1, err))
			break
		}
		if err := validateDiscover(line, extra); err != nil {
			result.Failures = append(result.Failures, fmt.Sprintf("MCP startup %d: %v", i+1, err))
			break
		}
		result.MCPStartup.Samples = append(result.MCPStartup.Samples, duration.Nanoseconds())
	}
	result.MCPStartup = summarize(result.MCPStartup.Samples)

	for run := 0; run < restRuns; run++ {
		output, peak, err := runREST(candidate, fixtureURL)
		if err != nil {
			result.Failures = append(result.Failures, fmt.Sprintf("REST run %d: %v", run+1, err))
			break
		}
		if len(output.DurationsNS) != restSamples || output.UserID == "" {
			result.Failures = append(result.Failures, fmt.Sprintf("REST run %d returned invalid sample data", run+1))
			break
		}
		result.WarmREST.Samples = append(result.WarmREST.Samples, output.DurationsNS...)
		result.PeakMemoryBytes = append(result.PeakMemoryBytes, peak)
	}
	result.WarmREST = summarize(result.WarmREST.Samples)
	if len(result.ColdStart.Samples) != coldSamples || len(result.MCPStartup.Samples) != mcpSamples || len(result.WarmREST.Samples) != restRuns*restSamples || len(result.PeakMemoryBytes) != restRuns {
		result.Failures = append(result.Failures, "one or more required metrics are incomplete")
	}
	return result
}

func runLine(candidate candidateCommand, mode []string, stdin string) (time.Duration, []byte, string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), processTimeout)
	defer cancel()
	args := append(append([]string{}, candidate.args...), mode...)
	cmd := exec.CommandContext(ctx, candidate.path, args...)
	configureProcess(cmd)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return 0, nil, "", err
	}
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	input, err := cmd.StdinPipe()
	if err != nil {
		return 0, nil, "", err
	}
	start := time.Now()
	if err := cmd.Start(); err != nil {
		return 0, nil, "", err
	}
	if stdin != "" {
		if _, err := io.WriteString(input, stdin); err != nil {
			terminateProcessTree(cmd)
			return 0, nil, "", err
		}
	} else {
		_ = input.Close()
	}
	reader := bufio.NewReader(io.LimitReader(stdout, 1<<20))
	line, err := reader.ReadBytes('\n')
	duration := time.Since(start)
	if err != nil {
		terminateProcessTree(cmd)
		_ = cmd.Wait()
		return 0, nil, "", fmt.Errorf("read readiness: %w; stderr=%s", err, strings.TrimSpace(stderr.String()))
	}
	_ = input.Close()
	remainder, readErr := io.ReadAll(reader)
	waitErr := cmd.Wait()
	if ctx.Err() != nil {
		terminateProcessTree(cmd)
		return 0, nil, "", fmt.Errorf("process timeout: %w", ctx.Err())
	}
	if readErr != nil {
		return 0, nil, "", readErr
	}
	if waitErr != nil {
		return 0, nil, "", fmt.Errorf("process failed: %w; stderr=%s", waitErr, strings.TrimSpace(stderr.String()))
	}
	return duration, bytes.TrimSpace(line), string(remainder), nil
}

func runREST(candidate candidateCommand, fixtureURL string) (restOutput, uint64, error) {
	ctx, cancel := context.WithTimeout(context.Background(), processTimeout)
	defer cancel()
	args := append(append([]string{}, candidate.args...), "rest", fixtureURL, strconv.Itoa(restWarmups), strconv.Itoa(restSamples))
	cmd := exec.CommandContext(ctx, candidate.path, args...)
	configureProcess(cmd)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Start(); err != nil {
		return restOutput{}, 0, err
	}
	var peak uint64
	done := make(chan struct{})
	sampled := make(chan struct{})
	go func() {
		defer close(sampled)
		ticker := time.NewTicker(time.Millisecond)
		defer ticker.Stop()
		for {
			if rss, err := residentBytes(cmd.Process.Pid); err == nil && rss > peak {
				peak = rss
			}
			select {
			case <-done:
				return
			case <-ticker.C:
			}
		}
	}()
	waitErr := cmd.Wait()
	close(done)
	<-sampled
	if ctx.Err() != nil {
		terminateProcessTree(cmd)
		return restOutput{}, peak, fmt.Errorf("process timeout: %w", ctx.Err())
	}
	if waitErr != nil {
		return restOutput{}, peak, fmt.Errorf("process failed: %w; stderr=%s", waitErr, strings.TrimSpace(stderr.String()))
	}
	var output restOutput
	decoder := json.NewDecoder(bytes.NewReader(stdout.Bytes()))
	if err := decoder.Decode(&output); err != nil {
		return restOutput{}, peak, err
	}
	if decoder.Decode(&struct{}{}) != io.EOF {
		return restOutput{}, peak, errors.New("extra REST stdout")
	}
	return output, peak, nil
}

func validateDiscover(line []byte, extra string) error {
	if strings.TrimSpace(extra) != "" {
		return errors.New("non-MCP stdout after discovery response")
	}
	var response struct {
		JSONRPC string `json:"jsonrpc"`
		ID      int    `json:"id"`
		Result  struct {
			SupportedVersions []string `json:"supportedVersions"`
		} `json:"result"`
		Error json.RawMessage `json:"error"`
	}
	if err := json.Unmarshal(line, &response); err != nil {
		return fmt.Errorf("malformed JSON-RPC: %w", err)
	}
	if response.JSONRPC != "2.0" || response.ID != 1 || len(response.Error) != 0 {
		return errors.New("invalid JSON-RPC discovery envelope")
	}
	for _, version := range response.Result.SupportedVersions {
		if version == protocolVersion {
			return nil
		}
	}
	return fmt.Errorf("discovery omitted protocol %s", protocolVersion)
}

func measureGoBuilds(paths preparedPaths) metric {
	var samples []int64
	for i := 0; i < buildSamples; i++ {
		cache := filepath.Join(paths.workDir, fmt.Sprintf("gocache-%d", i))
		output := filepath.Join(paths.workDir, fmt.Sprintf("go-build-%d.exe", i))
		_ = os.RemoveAll(cache)
		start := time.Now()
		err := runCommand(paths.goDir, 2*time.Minute, []string{"GOCACHE=" + cache}, paths.goTool, "build", "-trimpath", "-ldflags=-s -w -buildid=", "-buildvcs=false", "-o", output, ".")
		if err != nil {
			break
		}
		samples = append(samples, time.Since(start).Nanoseconds())
	}
	return summarize(samples)
}

func measureTSBuilds(paths preparedPaths) metric {
	var samples []int64
	for i := 0; i < buildSamples; i++ {
		_ = os.RemoveAll(filepath.Join(paths.tsDir, "dist"))
		start := time.Now()
		err := runCommand(paths.tsDir, 2*time.Minute, nil, paths.nodeTool, filepath.Join(paths.tsDir, "node_modules", "typescript", "bin", "tsc"), "-p", "tsconfig.json")
		if err != nil {
			break
		}
		samples = append(samples, time.Since(start).Nanoseconds())
	}
	return summarize(samples)
}

func summarize(samples []int64) metric {
	copySamples := append([]int64(nil), samples...)
	if len(copySamples) == 0 {
		return metric{Samples: samples}
	}
	sorted := append([]int64(nil), copySamples...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i] < sorted[j] })
	median := sorted[len(sorted)/2]
	if len(sorted)%2 == 0 {
		median = (sorted[len(sorted)/2-1] + sorted[len(sorted)/2]) / 2
	}
	p95Index := (95*len(sorted)+99)/100 - 1
	return metric{Samples: copySamples, MedianNS: median, P95NS: sorted[p95Index]}
}

func score(results []candidateResult) (string, bool, error) {
	eligible := make([]*candidateResult, 0, len(results))
	for i := range results {
		if results[i].Eligible {
			eligible = append(eligible, &results[i])
		}
	}
	if len(eligible) == 0 {
		return "", false, errors.New("no candidate completed every required metric")
	}
	value := func(candidate *candidateResult, dimension string) float64 {
		switch dimension {
		case "memory":
			var peak uint64
			for _, sample := range candidate.PeakMemoryBytes {
				if sample > peak {
					peak = sample
				}
			}
			return float64(peak)
		case "artifact":
			return float64(candidate.Artifact.DeployableBytes)
		case "cold":
			return .7*float64(candidate.ColdStart.MedianNS) + .3*float64(candidate.ColdStart.P95NS)
		case "mcp":
			return .7*float64(candidate.MCPStartup.MedianNS) + .3*float64(candidate.MCPStartup.P95NS)
		case "rest":
			return .7*float64(candidate.WarmREST.MedianNS) + .3*float64(candidate.WarmREST.P95NS)
		case "build":
			return float64(candidate.CleanBuild.MedianNS)
		default:
			panic("unknown dimension")
		}
	}
	weights := map[string]float64{"memory": .25, "artifact": .20, "cold": .20, "mcp": .15, "rest": .10, "build": .10}
	for dimension, weight := range weights {
		best := value(eligible[0], dimension)
		for _, candidate := range eligible[1:] {
			if v := value(candidate, dimension); v < best {
				best = v
			}
		}
		if best <= 0 {
			return "", false, fmt.Errorf("invalid zero value for %s", dimension)
		}
		for _, candidate := range eligible {
			candidate.LeanScore += weight * value(candidate, dimension) / best
		}
	}
	sort.Slice(eligible, func(i, j int) bool { return eligible[i].LeanScore < eligible[j].LeanScore })
	selected := eligible[0].Name
	tie := false
	if len(eligible) > 1 && (eligible[1].LeanScore-eligible[0].LeanScore)/eligible[0].LeanScore <= .05 {
		selected = "go"
		tie = true
	}
	return selected, tie, nil
}

func startFixtureServer(fixture []byte) (string, func(), error) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return "", nil, err
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v10/users/@me", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("X-RateLimit-Limit", "1000000")
		w.Header().Set("X-RateLimit-Remaining", "999999")
		w.Header().Set("X-RateLimit-Reset-After", "0.001")
		_, _ = w.Write(fixture)
	})
	server := &http.Server{Handler: mux, ReadHeaderTimeout: 2 * time.Second}
	go func() { _ = server.Serve(listener) }()
	closeFn := func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = server.Shutdown(ctx)
	}
	return "http://" + listener.Addr().String(), closeFn, nil
}

func runCommand(dir string, timeout time.Duration, extraEnv []string, executable string, args ...string) error {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, executable, args...)
	configureProcess(cmd)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), extraEnv...)
	var output bytes.Buffer
	cmd.Stdout = &output
	cmd.Stderr = &output
	if err := cmd.Run(); err != nil {
		terminateProcessTree(cmd)
		return fmt.Errorf("%s %s: %w: %s", filepath.Base(executable), strings.Join(args, " "), err, strings.TrimSpace(output.String()))
	}
	return nil
}

func commandOutput(dir, executable string, args ...string) string {
	cmd := exec.Command(executable, args...)
	cmd.Dir = dir
	configureProcess(cmd)
	output, _ := cmd.CombinedOutput()
	return string(output)
}

func copyFile(source, destination string) error {
	input, err := os.Open(source)
	if err != nil {
		return err
	}
	defer input.Close()
	output, err := os.OpenFile(destination, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	if _, err := io.Copy(output, input); err != nil {
		_ = output.Close()
		return err
	}
	return output.Close()
}

func fileSize(path string) (int64, error) {
	info, err := os.Stat(path)
	if err != nil {
		return 0, err
	}
	return info.Size(), nil
}

func directorySize(root string) (int64, error) {
	var total int64
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.Type().IsRegular() {
			info, err := entry.Info()
			if err != nil {
				return err
			}
			total += info.Size()
		}
		return nil
	})
	return total, err
}

func gitRevision(root string) string {
	value := strings.TrimSpace(commandOutput(root, "git", "rev-parse", "HEAD"))
	if len(value) == 40 {
		return value
	}
	return "uncommitted-working-tree"
}

func cpuDescription() string {
	for _, key := range []string{"PROCESSOR_IDENTIFIER", "PROCESSOR_ARCHITECTURE"} {
		if value := strings.TrimSpace(os.Getenv(key)); value != "" {
			return value
		}
	}
	return runtime.GOARCH
}

func markdownSummary(result benchmarkResult) string {
	var builder strings.Builder
	builder.WriteString("# Runtime benchmark — Windows amd64\n\n")
	builder.WriteString("Owner: Maintainers  \nLast reviewed: 2026-08-21\n\n")
	fmt.Fprintf(&builder, "**Selected:** `%s` using the predeclared weighted lean score.\n\n", result.Selected)
	builder.WriteString("| Candidate | Eligible | Lean score | Cold median / p95 | MCP median / p95 | REST median / p95 | Peak RSS | Deployable | Build median |\n")
	builder.WriteString("|---|---:|---:|---:|---:|---:|---:|---:|---:|\n")
	for _, c := range result.Candidates {
		fmt.Fprintf(&builder, "| %s | %t | %.6f | %.3f / %.3f ms | %.3f / %.3f ms | %.3f / %.3f ms | %.2f MiB | %.2f MiB | %.3f ms |\n",
			c.Name, c.Eligible, c.LeanScore, ms(c.ColdStart.MedianNS), ms(c.ColdStart.P95NS), ms(c.MCPStartup.MedianNS), ms(c.MCPStartup.P95NS), ms(c.WarmREST.MedianNS), ms(c.WarmREST.P95NS), float64(maxUint64(c.PeakMemoryBytes))/(1024*1024), float64(c.Artifact.DeployableBytes)/(1024*1024), ms(c.CleanBuild.MedianNS))
	}
	builder.WriteString("\n## Method\n\n")
	fmt.Fprintf(&builder, "- Host: `%s/%s`; CPU: `%s`; source: `%s`.\n", result.OS, result.Architecture, result.CPU, result.SourceRevision)
	fmt.Fprintf(&builder, "- Cold and MCP startup: %d fresh processes each. REST: %d warmups plus %d measured keep-alive requests across %d runs. Clean builds: %d.\n", result.Parameters.ColdSamples, result.Parameters.RESTWarmups, result.Parameters.RESTSamples, result.Parameters.RESTRuns, result.Parameters.BuildSamples)
	builder.WriteString("- Peak memory is externally sampled resident working set. TypeScript deployable size includes compiled output, production dependencies, and the pinned portable Node runtime.\n")
	builder.WriteString("- Score weights: memory 25%, artifact 20%, cold 20%, MCP 15%, REST 10%, build 10%; each value is normalized to the best eligible candidate. Within 5%, Go wins the single-binary tie-break.\n")
	builder.WriteString("\n## Limitations\n\n")
	for _, limitation := range result.Limitations {
		fmt.Fprintf(&builder, "- %s\n", limitation)
	}
	builder.WriteString("\nRaw samples and score inputs are in the adjacent JSON file.\n")
	return builder.String()
}

func ms(ns int64) float64 { return float64(ns) / 1e6 }
func maxInt64(values []int64) int64 {
	var max int64
	for _, value := range values {
		if value > max {
			max = value
		}
	}
	return max
}

func maxUint64(values []uint64) uint64 {
	var max uint64
	for _, value := range values {
		if value > max {
			max = value
		}
	}
	return max
}

func validateResultFile(path string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	var result benchmarkResult
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&result); err != nil {
		return err
	}
	if decoder.Decode(&struct{}{}) != io.EOF {
		return errors.New("trailing JSON in benchmark result")
	}
	if result.SchemaVersion != 1 || result.ProtocolVersion != protocolVersion || len(result.Candidates) != 2 || result.Selected == "" {
		return errors.New("invalid benchmark result metadata")
	}
	for _, candidate := range result.Candidates {
		if !candidate.Eligible || len(candidate.ColdStart.Samples) != coldSamples || len(candidate.MCPStartup.Samples) != mcpSamples || len(candidate.WarmREST.Samples) != restRuns*restSamples || len(candidate.PeakMemoryBytes) != restRuns || len(candidate.CleanBuild.Samples) != buildSamples {
			return fmt.Errorf("candidate %s has incomplete samples", candidate.Name)
		}
	}
	summaryPath := strings.TrimSuffix(path, filepath.Ext(path)) + ".md"
	summary, err := os.ReadFile(summaryPath)
	if err != nil {
		return err
	}
	if !bytes.Contains(summary, []byte("**Selected:** `"+result.Selected+"`")) {
		return errors.New("Markdown summary does not match selected runtime")
	}
	return nil
}
