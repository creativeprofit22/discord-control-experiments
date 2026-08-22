package main

import (
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"sort"
	"time"

	"github.com/ken-kai/discord-control-experiments/internal/application"
	"github.com/ken-kai/discord-control-experiments/internal/cli"
)

type fakeOperator struct{}

func (fakeOperator) Execute(context.Context, application.OperatorRequest) (application.OperatorResult, error) {
	return application.OperatorResult{Kind: "Health", Value: map[string]string{"status": "ok"}}, nil
}

type samples struct {
	Samples []int64 `json:"samplesNs"`
	Median  int64   `json:"medianNs"`
	P95     int64   `json:"p95Ns"`
}
type result struct {
	VersionCold samples `json:"versionCold"`
	HelpCold    samples `json:"helpCold"`
	Warm        samples `json:"warmParseDispatchRender"`
}

func main() {
	artifact := flag.String("artifact", "", "packaged CLI executable")
	flag.Parse()
	output := result{Warm: measureWarm()}
	if *artifact != "" {
		output.VersionCold = measureCold(*artifact, []string{"version", "--format", "json"})
		output.HelpCold = measureCold(*artifact, []string{"help", "--format", "json"})
	}
	payload, _ := json.Marshal(output)
	fmt.Println(string(payload))
}
func measureWarm() samples {
	values := make([]int64, 1000)
	for index := range values {
		started := time.Now()
		command, err := cli.Parse([]string{"health", "local", "--config", "config.json", "--format", "json"})
		if err != nil {
			fail(err)
		}
		value, err := cli.Dispatch(context.Background(), fakeOperator{}, command, cli.DispatchOptions{})
		if err != nil {
			fail(err)
		}
		var output bytes.Buffer
		if err := cli.Render(&output, "json", cli.SuccessEnvelope(value.Kind, "benchmark-correlation", value.Value)); err != nil {
			fail(err)
		}
		values[index] = time.Since(started).Nanoseconds()
	}
	return summarize(values)
}
func measureCold(artifact string, args []string) samples {
	values := make([]int64, 50)
	for index := range values {
		started := time.Now()
		command := exec.Command(artifact, args...)
		payload, err := command.Output()
		values[index] = time.Since(started).Nanoseconds()
		if err != nil {
			fail(err)
		}
		var envelope struct {
			APIVersion string `json:"apiVersion"`
		}
		if json.Unmarshal(payload, &envelope) != nil || envelope.APIVersion != cli.APIVersion {
			fail(fmt.Errorf("invalid CLI envelope"))
		}
	}
	return summarize(values)
}
func summarize(values []int64) samples {
	sorted := append([]int64(nil), values...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i] < sorted[j] })
	return samples{Samples: values, Median: sorted[len(sorted)/2], P95: sorted[(len(sorted)*95+99)/100-1]}
}
func fail(err error) { _, _ = fmt.Fprintln(os.Stderr, err); os.Exit(1) }
