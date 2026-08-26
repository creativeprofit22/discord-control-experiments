package e2e

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestProductionReadinessFakeDiscordMatrix(t *testing.T) {
	root := filepath.Clean(filepath.Join("..", ".."))
	goBinary, err := exec.LookPath("go")
	if err != nil {
		t.Fatal(err)
	}
	tests := strings.Join([]string{
		"TestRunMCPOnlyActorStartup",
		"TestRuntimeCompositionStartsAndStopsInteractionsModesWithoutGateway",
		"TestBlackBoxStdioDiscoveryUsesPinnedWireAndPureStdout",
		"TestRealSignedStackRoutesAllInteractionTypesAndDeduplicatesDeferredWork",
		"TestSignedInteractionCarriesOneTraceAndCorrelationThroughCallback",
		"TestReconciliationApplyRequiresExactHashAndFreshSnapshot",
		"TestReconciliationDriftReportsManagedAndUnmanagedConflicts",
		"TestLockdownProtectedAccessPartialApplyResumeAndDriftRefusal",
		"TestModerationWarningRedactionDuplicateAndExpiredTimeoutRemoval",
		"TestAutomationIntegrationRestartAndDuplicateWorkers",
		"TestRSSConditionalDedupAndRenderer",
		"TestAutoModerationPlanCreatesDisabledBeforeEnableAndReconciles",
	}, "|")
	command := exec.Command(goBinary, "test", "./cmd/discord-control", "./internal/mcp", "./internal/interactions", "./internal/application", "./internal/jobs", "./internal/reconcile", "-run", "^("+tests+")$", "-count=1")
	command.Dir = root
	command.Env = append(os.Environ(), "GOWORK=off")
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("fake-Discord readiness matrix failed: %v\n%s", err, output)
	}
}

func TestProductionReadinessFaultMatrix(t *testing.T) {
	root := filepath.Clean(filepath.Join("..", ".."))
	goBinary, err := exec.LookPath("go")
	if err != nil {
		t.Fatal(err)
	}
	tests := strings.Join([]string{
		"TestClientRetries429AndRedactsCapture",
		"TestTransientFailureRetriesAndExhaustion",
		"TestUncertainCreateReconcilesWithoutDuplicateAndReturnsPartial",
		"TestAutomationIntegrationUncertainMutationDoesNotReplay",
		"TestRealSignedStackQueueSaturationHasNoExecutionOrCallback",
		"TestAutomationJournalCorruptionAndCrashTail",
		"TestAtomicAutomationJSONReplacementFailurePreservesProjection",
		"TestExporterFailureAndSamplingDropsAreObservableAndContained",
		"TestMalformedArgumentsNeverReachOperator",
	}, "|")
	command := exec.Command(goBinary, "test", "./internal/discord", "./internal/application", "./internal/interactions", "./internal/state", "./internal/observability", "./internal/mcp", "-run", "^("+tests+")$", "-count=1")
	command.Dir = root
	command.Env = append(os.Environ(), "GOWORK=off")
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("fault-injection matrix failed: %v\n%s", err, output)
	}
}

func TestGatewaySurfaceRemainsUncomposed(t *testing.T) {
	files, err := filepath.Glob(filepath.Join("..", "..", "cmd", "discord-control", "*.go"))
	if err != nil {
		t.Fatal(err)
	}
	for _, filename := range files {
		data, err := os.ReadFile(filename)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(data), "internal/gateway") || strings.Contains(string(data), "gateway.New") {
			t.Fatalf("unsupported Gateway was composed in %s", filename)
		}
	}
}
