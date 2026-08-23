package main

import (
	"bufio"
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/ken-kai/discord-control-experiments/internal/application"
	"github.com/ken-kai/discord-control-experiments/internal/approval"
	"github.com/ken-kai/discord-control-experiments/internal/audit"
	"github.com/ken-kai/discord-control-experiments/internal/buildinfo"
	"github.com/ken-kai/discord-control-experiments/internal/cli"
	"github.com/ken-kai/discord-control-experiments/internal/config"
	discordadapter "github.com/ken-kai/discord-control-experiments/internal/discord"
	"github.com/ken-kai/discord-control-experiments/internal/domain"
	interactionadapter "github.com/ken-kai/discord-control-experiments/internal/interactions"
	mcpadapter "github.com/ken-kai/discord-control-experiments/internal/mcp"
	"github.com/ken-kai/discord-control-experiments/internal/observability"
	"github.com/ken-kai/discord-control-experiments/internal/policy"
	"github.com/ken-kai/discord-control-experiments/internal/state"
)

type systemClock struct{}

func (systemClock) Now() time.Time { return time.Now() }

type correlationIDGenerator struct{}

func (correlationIDGenerator) NewCorrelationID() (domain.CorrelationID, error) {
	return observability.CorrelationID()
}

func main() { os.Exit(mainExit(os.Args[1:], os.Stderr)) }

func mainExit(args []string, stderr io.Writer) int {
	exitCode := cli.ExitInternal
	panicCode := guardedMain(stderr, func() error {
		exitCode = executeCLI(args, os.Stdout, stderr)
		return nil
	})
	if panicCode != 0 {
		return cli.ExitInternal
	}
	return exitCode
}

func guardedMain(stderr io.Writer, execute func() error) (code int) {
	defer func() {
		if recover() != nil {
			_, _ = fmt.Fprintln(stderr, "discord-control terminated unexpectedly")
			code = 1
		}
	}()
	if err := execute(); err != nil {
		_, _ = fmt.Fprintln(stderr, "discord-control failed")
		return 1
	}
	return 0
}

func executeCLI(args []string, stdout, stderr io.Writer) int {
	correlationID, correlationErr := observability.CorrelationID()
	if correlationErr != nil {
		correlationID = "correlation-unavailable"
	}
	command, err := cli.Parse(args)
	format := requestedFormat(args)
	if err != nil {
		_ = cli.Render(stdout, format, cli.FailureEnvelope(correlationID, err))
		return cli.ExitCode(err)
	}
	format = command.Format
	signalContext, stop := cli.SignalContext(context.Background())
	defer stop()
	commandContext := signalContext
	cancel := func() {}
	if command.Name != "run" {
		commandContext, cancel, err = cli.CommandContext(signalContext, command.Timeout)
		if err != nil {
			_ = cli.Render(stdout, format, cli.FailureEnvelope(correlationID, err))
			return cli.ExitCode(err)
		}
	}
	defer cancel()
	started := time.Now()
	result, runErr := run(commandContext, command, stdout, stderr, correlationID)
	runErr = cli.NormalizeCancellation(commandContext, signalContext, runErr)
	if command.Name == "run" {
		if runErr != nil {
			_, _ = fmt.Fprintln(stderr, "discord-control MCP runtime stopped with an error")
		}
		return cli.ExitCode(runErr)
	}
	var envelope cli.Envelope
	if runErr != nil {
		envelope = cli.FailureEnvelope(correlationID, runErr)
	} else {
		envelope = cli.SuccessEnvelope(result.Kind, correlationID, result.Value)
		if result.Partial != nil {
			envelope.Recovery = &cli.Recovery{Partial: true, Recoverable: true}
		}
	}
	envelope = cli.WithTiming(envelope, started)
	if renderErr := cli.Render(stdout, format, envelope); renderErr != nil {
		_, _ = fmt.Fprintln(stderr, "discord-control output failed")
		return cli.ExitInternal
	}
	return cli.ExitCode(runErr)
}

func requestedFormat(args []string) string {
	for index := 0; index+1 < len(args); index++ {
		if args[index] == "--format" && args[index+1] == "json" {
			return "json"
		}
	}
	return "human"
}

func run(ctx context.Context, command cli.Command, stdout, stderr io.Writer, correlationID domain.CorrelationID) (application.OperatorResult, error) {
	if command.Name == "version" || command.Name == "help" || command.Name == "completion" {
		return cli.Dispatch(ctx, nil, command, cli.DispatchOptions{Version: buildinfo.Version, Commit: buildinfo.Commit})
	}
	file, err := os.Open(command.ConfigPath)
	if err != nil {
		return application.OperatorResult{}, fmt.Errorf("open configuration: %w", err)
	}
	defer file.Close()
	resolved, err := config.Load(file, config.EnvironmentResolver{Lookup: os.LookupEnv})
	if err != nil {
		return application.OperatorResult{}, err
	}

	redactor := observability.NewRedactor()
	resolved.SecretProvider.SetRegistrar(redactor)
	discordClient, err := discordadapter.New(discordadapter.TokenUse(resolved.DiscordToken.Use))
	if err != nil {
		return application.OperatorResult{}, fmt.Errorf("initialize Discord adapter: %w", err)
	}
	denialWindow, _ := time.ParseDuration(resolved.SecurityAlerts.DenialWindow)
	destructiveWindow, _ := time.ParseDuration(resolved.SecurityAlerts.DestructiveWindow)
	signatureWindow, _ := time.ParseDuration(resolved.SecurityAlerts.SignatureFailureWindow)
	ackMissWindow, _ := time.ParseDuration(resolved.SecurityAlerts.AckDeadlineMissWindow)
	callbackFailureWindow, _ := time.ParseDuration(resolved.SecurityAlerts.CallbackFailureWindow)
	saturationWindow, _ := time.ParseDuration(resolved.SecurityAlerts.HandlerSaturationWindow)
	securityOptions := observability.SecurityOptions{DenialThreshold: resolved.SecurityAlerts.DenialThreshold, DenialWindow: denialWindow, DestructiveThreshold: resolved.SecurityAlerts.DestructiveThreshold, DestructiveWindow: destructiveWindow, SignatureFailureThreshold: resolved.SecurityAlerts.SignatureFailureThreshold, SignatureFailureWindow: signatureWindow, AckDeadlineMissThreshold: resolved.SecurityAlerts.AckDeadlineMissThreshold, AckDeadlineMissWindow: ackMissWindow, CallbackFailureThreshold: resolved.SecurityAlerts.CallbackFailureThreshold, CallbackFailureWindow: callbackFailureWindow, HandlerSaturationThreshold: resolved.SecurityAlerts.HandlerSaturationThreshold, HandlerSaturationWindow: saturationWindow}
	provider, err := observability.New(ctx, observability.Options{Writer: stderr, Exporter: resolved.Observability.Exporter, Endpoint: resolved.Observability.Endpoint, Build: buildinfo.Current(), Redactor: redactor, Security: securityOptions})
	if err != nil {
		return application.OperatorResult{}, fmt.Errorf("initialize observability: %w", err)
	}
	policyService, err := compilePolicy(resolved.Policy)
	if err != nil {
		return application.OperatorResult{}, fmt.Errorf("initialize policy: %w", err)
	}
	approvalTTL, _ := time.ParseDuration(resolved.Approvals.MaxTTL)
	approvalStore, err := approval.NewStore(approvalTTL)
	if err != nil {
		return application.OperatorResult{}, fmt.Errorf("initialize approvals: %w", err)
	}
	configPath, err := filepath.Abs(command.ConfigPath)
	if err != nil {
		return application.OperatorResult{}, fmt.Errorf("resolve configuration path: %w", err)
	}
	auditSink, err := audit.Open(filepath.Dir(configPath), resolved.Audit.Path, resolved.Audit.MaxBytes, func(value string) (string, bool, error) {
		sanitized, changed := redactor.Sanitize(value)
		return sanitized, changed, nil
	})
	if err != nil {
		return application.OperatorResult{}, fmt.Errorf("initialize audit: %w", err)
	}
	defaultInvocation := application.Invocation{Principal: domain.Principal{Adapter: "mcp", Subject: "local-stdio", AuthenticationMethod: "os-process"}, RequestedMode: domain.ExecutionMode(resolved.Policy.Mode), CorrelationID: "local-stdio-runtime"}
	discordService, err := application.NewSecuredDiscordService(discordClient, application.NewInMemoryIdempotencyStore(), application.NewInMemoryResourceOwnershipStore(), provider, application.SecurityDependencies{Policy: policyService, Approvals: approvalStore, Audit: auditSink, Telemetry: provider, DefaultInvocation: defaultInvocation})
	if err != nil {
		_ = auditSink.Close()
		return application.OperatorResult{}, fmt.Errorf("initialize Discord service: %w", err)
	}
	statePaths, err := state.ResolvePaths(filepath.Dir(configPath), resolved.Reconciliation.StateDirectory)
	if err != nil {
		_ = auditSink.Close()
		return application.OperatorResult{}, fmt.Errorf("initialize reconciliation paths: %w", err)
	}
	stateStore := state.NewStore(statePaths)
	reconciliationService, err := application.NewReconciliationService(discordService, stateStore, systemClock{}, correlationIDGenerator{}, provider, application.ReconciliationLimits{MaxOperations: resolved.Reconciliation.MaxOperations, MaxDestructiveOperations: resolved.Reconciliation.MaxDestructiveOperations})
	if err != nil {
		_ = auditSink.Close()
		return application.OperatorResult{}, fmt.Errorf("initialize reconciliation service: %w", err)
	}
	handlers := reconciliationOperatorHandlers(command, resolved, configPath, reconciliationService)
	jobService, jobErr := application.NewJobService(stateStore, reconciliationService, systemClock{})
	if jobErr != nil {
		_ = auditSink.Close()
		return application.OperatorResult{}, jobErr
	}
	addJobOperatorHandlers(handlers, jobService)
	policyFacade, facadeErr := application.NewPolicyFacade(policyService)
	if facadeErr != nil {
		_ = auditSink.Close()
		return application.OperatorResult{}, facadeErr
	}
	healthService, healthErr := application.NewHealthService(discordService, func(context.Context) []application.HealthCheck {
		checks := []application.HealthCheck{{Name: "build", Status: "ok"}, {Name: "configuration", Status: "ok"}, {Name: "audit", Status: "ok"}}
		if info, statErr := os.Stat(statePaths.Root()); statErr != nil || !info.IsDir() {
			checks = append(checks, application.HealthCheck{Name: "state", Status: "failed"})
		} else {
			checks = append(checks, application.HealthCheck{Name: "state", Status: "ok"})
		}
		return checks
	})
	if healthErr != nil {
		_ = auditSink.Close()
		return application.OperatorResult{}, healthErr
	}
	addSupportOperatorHandlers(handlers, filepath.Dir(configPath), policyFacade, healthService, application.NewDiagnosticBundleService(), jobService)
	operatorService, operatorErr := application.NewOperatorService(discordService, handlers)
	if operatorErr != nil {
		_ = auditSink.Close()
		return application.OperatorResult{}, operatorErr
	}
	maximumApprovalTTL, ttlErr := time.ParseDuration(resolved.Approvals.MaxTTL)
	if ttlErr != nil {
		_ = auditSink.Close()
		return application.OperatorResult{}, ttlErr
	}
	approvalCoordinator, coordinatorErr := application.NewApprovalCoordinator(approvalStore, systemClock{}, maximumApprovalTTL)
	if coordinatorErr != nil {
		_ = auditSink.Close()
		return application.OperatorResult{}, coordinatorErr
	}
	if command.Name == "run" {
		if !resolved.MCPEnabled && !resolved.InteractionsEnabled {
			_ = auditSink.Close()
			return application.OperatorResult{}, errors.New("run requires MCP and/or interactions to be enabled")
		}
		var mcpLifecycle application.Lifecycle
		var interactionLifecycle application.Lifecycle
		if resolved.MCPEnabled {
			mcpPrincipal := domain.Principal{Adapter: "mcp", Subject: "local-stdio", AuthenticationMethod: "os-process"}
			mcpActor, actorErr := configuredActor(resolved.Policy, mcpPrincipal)
			if actorErr != nil {
				_ = auditSink.Close()
				return application.OperatorResult{}, actorErr
			}
			callTimeout, timeoutErr := time.ParseDuration(resolved.MCP.CallTimeout)
			if timeoutErr != nil {
				_ = auditSink.Close()
				return application.OperatorResult{}, timeoutErr
			}
			toolsets := make([]application.Toolset, len(resolved.MCP.Toolsets))
			for index, value := range resolved.MCP.Toolsets {
				toolsets[index] = application.Toolset(value)
			}
			mcpServer, mcpErr := mcpadapter.New(mcpadapter.Options{Enabled: true, ImplementationVersion: buildinfo.Version, GuildID: domain.GuildID(resolved.MCP.GuildID), ActorID: mcpActor, Mode: domain.ExecutionMode(resolved.MCP.Mode), Toolsets: toolsets, ReadOnly: *resolved.MCP.ReadOnly, MaxMessageBytes: resolved.MCP.MaxMessageBytes, MaxConcurrentCalls: resolved.MCP.MaxConcurrentCalls, CallTimeout: callTimeout, Logger: provider.Logger, Input: os.Stdin, Output: stdout, Policy: policyService, Operator: operatorService, Approval: approvalCoordinator, Observer: provider, Clock: time.Now, Correlation: func() domain.CorrelationID { id, _ := observability.CorrelationID(); return id }})
			if mcpErr != nil {
				_ = auditSink.Close()
				return application.OperatorResult{}, mcpErr
			}
			mcpLifecycle = mcpServer
		}
		if resolved.InteractionsEnabled {
			interactionTransport, interactionErr := newInteractionTransport(resolved, policyService, healthService, jobService, approvalCoordinator, discordClient, auditSink, provider)
			if interactionErr != nil {
				_ = auditSink.Close()
				return application.OperatorResult{}, interactionErr
			}
			interactionLifecycle = interactionTransport
		}
		lifecycles, lifecycleErr := runtimeLifecycles(discordClient, resolved.MCPEnabled, mcpLifecycle, resolved.InteractionsEnabled, interactionLifecycle)
		if lifecycleErr != nil {
			_ = auditSink.Close()
			return application.OperatorResult{}, lifecycleErr
		}
		app, appErr := application.NewWithLifecycles(lifecycles, provider, systemClock{}, observability.CorrelationID, discordService)
		if appErr != nil {
			_ = auditSink.Close()
			return application.OperatorResult{}, appErr
		}
		runErr := app.Run(ctx)
		shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer shutdownCancel()
		return application.OperatorResult{Kind: "Runtime", Value: map[string]string{"status": "stopped"}}, errors.Join(runErr, provider.Shutdown(shutdownCtx), auditSink.Close())
	}
	principal := domain.Principal{Adapter: "cli", Subject: "local-process", AuthenticationMethod: "os-process"}
	actorID, actorErr := configuredActor(resolved.Policy, principal)
	if actorErr != nil {
		_ = auditSink.Close()
		return application.OperatorResult{}, actorErr
	}
	invocation := application.Invocation{Principal: principal, ActorID: actorID, RequestedMode: configuredActorMode(resolved.Policy, actorID), ApprovalID: command.ApprovalID, CorrelationID: correlationID}
	result, dispatchErr := cli.Dispatch(ctx, operatorService, command, cli.DispatchOptions{Root: filepath.Dir(configPath), Stdin: os.Stdin, Invocation: invocation, Version: buildinfo.Version, Commit: buildinfo.Commit, Approvals: approvalCoordinator, Confirm: ttyConfirmation(os.Stdin, stderr)})
	if dispatchErr != nil {
		provider.RecordCLIFailure(ctx, cli.FailureClass(dispatchErr))
	}
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	return result, errors.Join(dispatchErr, provider.Shutdown(shutdownCtx), auditSink.Close())
}

func runtimeLifecycles(discord application.Lifecycle, mcpEnabled bool, mcp application.Lifecycle, interactionsEnabled bool, interactions application.Lifecycle) ([]application.Lifecycle, error) {
	if discord == nil || mcpEnabled != (mcp != nil) || interactionsEnabled != (interactions != nil) {
		return nil, errors.New("runtime lifecycle composition is inconsistent")
	}
	lifecycles := []application.Lifecycle{discord}
	if mcpEnabled {
		lifecycles = append(lifecycles, mcp)
	}
	if interactionsEnabled {
		lifecycles = append(lifecycles, interactions)
	}
	return lifecycles, nil
}

type interactionRuntimeTelemetry interface {
	interactionadapter.InteractionTelemetry
	interactionadapter.CallbackTelemetry
	application.InteractionRegistrationTelemetry
}

func newInteractionTransport(resolved config.Resolved, policyService application.PolicyEvaluator, health *application.HealthService, jobs *application.JobService, approvals *application.ApprovalCoordinator, discordClient *discordadapter.Client, auditSink application.AuditSink, telemetry interactionRuntimeTelemetry) (application.Lifecycle, error) {
	interactionService, err := application.NewInteractionService(policyService, health, jobs)
	if err != nil {
		return nil, err
	}
	ackDeadline, _ := time.ParseDuration(resolved.Interactions.AckDeadline)
	workTimeout, _ := time.ParseDuration(resolved.Interactions.WorkTimeout)
	callbackTimeout, _ := time.ParseDuration(resolved.Interactions.CallbackTimeout)
	dedupTTL, _ := time.ParseDuration(resolved.Interactions.DedupTTL)
	skew, _ := time.ParseDuration(resolved.Interactions.TimestampSkew)
	verifier, err := interactionadapter.NewVerifier(resolved.Interactions.SigningPublicKeys, skew, time.Now)
	if err != nil {
		return nil, err
	}
	capacity := resolved.Interactions.QueueCapacity * 4
	if capacity < resolved.Interactions.MaxConcurrentRequests {
		capacity = resolved.Interactions.MaxConcurrentRequests
	}
	store, err := interactionadapter.NewInteractionStore(capacity, dedupTTL, time.Now)
	if err != nil {
		return nil, err
	}
	pending, err := interactionadapter.NewPendingStore(capacity, time.Now)
	if err != nil {
		return nil, err
	}
	executor, err := interactionadapter.NewExecutor(resolved.Interactions.QueueCapacity, resolved.Interactions.WorkerCount, workTimeout)
	if err != nil {
		return nil, err
	}
	callbackClient, err := interactionadapter.NewCallbackClient(&http.Client{Transport: http.DefaultTransport}, resolved.Interactions.ApplicationID, callbackTimeout, 64<<10, 64<<10, telemetry)
	if err != nil {
		return nil, err
	}
	renderer, err := interactionadapter.NewRenderer(callbackClient)
	if err != nil {
		return nil, err
	}
	router, err := interactionadapter.NewRouter(interactionService, approvals, store, pending, executor, renderer, telemetry, 14*time.Minute, time.Now)
	if err != nil {
		return nil, err
	}
	var certificate *tls.Certificate
	if resolved.Interactions.TLS.Mode == "direct" {
		var parsed tls.Certificate
		err = resolved.InteractionCertificate.Use(func(certificatePEM string) error {
			return resolved.InteractionPrivateKey.Use(func(privateKeyPEM string) error {
				value, parseErr := tls.X509KeyPair([]byte(certificatePEM), []byte(privateKeyPEM))
				if parseErr == nil {
					parsed = value
				}
				return parseErr
			})
		})
		if err != nil {
			return nil, errors.New("load interactions TLS certificate")
		}
		certificate = &parsed
	}
	trusted := make([]*net.IPNet, 0, len(resolved.Interactions.TLS.TrustedProxyCIDRs))
	for _, configured := range resolved.Interactions.TLS.TrustedProxyCIDRs {
		_, network, parseErr := net.ParseCIDR(configured)
		if parseErr != nil {
			return nil, errors.New("parse trusted proxy CIDR")
		}
		trusted = append(trusted, network)
	}
	server, err := interactionadapter.NewServer(interactionadapter.ServerOptions{ListenAddress: resolved.Interactions.ListenAddress, Path: resolved.Interactions.Path, ApplicationID: resolved.Interactions.ApplicationID, Verifier: verifier, Responder: router, MaxBodyBytes: resolved.Interactions.MaxBodyBytes, MaxConcurrentRequests: resolved.Interactions.MaxConcurrentRequests, AckDeadline: ackDeadline, TLSMode: resolved.Interactions.TLS.Mode, TLSCertificate: certificate, TrustedProxyCIDRs: trusted, Telemetry: telemetry, CorrelationID: observability.CorrelationID})
	if err != nil {
		return nil, err
	}
	registration, err := application.NewCommandRegistrationService(discordClient, auditSink, systemClock{}, telemetry)
	if err != nil {
		return nil, err
	}
	scope := application.CommandRegistrationScope{Kind: resolved.Interactions.Registration.Scope, ApplicationID: resolved.Interactions.ApplicationID, GuildID: resolved.Interactions.Registration.GuildID}
	return interactionadapter.NewTransport(server, executor, registration, scope, observability.CorrelationID)
}

func ttyConfirmation(stdin *os.File, stderr io.Writer) cli.Confirmation {
	info, err := stdin.Stat()
	if err != nil || info.Mode()&os.ModeCharDevice == 0 {
		return nil
	}
	return func(ctx context.Context, plan domain.OperationPlan) (bool, error) {
		prefix := plan.OperationHash
		if len(prefix) > 12 {
			prefix = prefix[:12]
		}
		_, _ = fmt.Fprintf(stderr, "Destructive %s intent: %v\nType 'confirm %s' to continue: ", plan.Operation, plan.Intent, prefix)
		reader := bufio.NewReader(io.LimitReader(stdin, 512))
		answer, readErr := reader.ReadString('\n')
		if readErr != nil && !errors.Is(readErr, io.EOF) {
			return false, readErr
		}
		select {
		case <-ctx.Done():
			return false, ctx.Err()
		default:
		}
		return strings.TrimSpace(answer) == "confirm "+prefix, nil
	}
}

func compilePolicy(configured config.Policy) (*policy.Service, error) {
	rules := make([]policy.ActorRule, 0, len(configured.Actors))
	for _, actor := range configured.Actors {
		rule := policy.ActorRule{ActorID: domain.ActorID(actor.ID), Mode: domain.ExecutionMode(actor.Mode)}
		for _, principal := range actor.Principals {
			rule.Principals = append(rule.Principals, domain.Principal{Adapter: domain.Adapter(principal.Adapter), Subject: principal.Subject, AuthenticationMethod: principal.AuthenticationMethod})
		}
		for _, guild := range actor.Guilds {
			rule.Guilds = append(rule.Guilds, domain.GuildID(guild))
		}
		for _, capability := range actor.Capabilities {
			rule.Capabilities = append(rule.Capabilities, domain.Capability(capability))
		}
		for _, operation := range actor.PreapprovedOperations {
			rule.PreapprovedOperations = append(rule.PreapprovedOperations, domain.OperationName(operation))
		}
		rules = append(rules, rule)
	}
	return policy.NewWithRules(domain.ExecutionMode(configured.Mode), rules)
}
