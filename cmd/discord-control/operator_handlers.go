package main

import (
	"context"
	"io"

	"github.com/ken-kai/discord-control-experiments/internal/application"
	"github.com/ken-kai/discord-control-experiments/internal/buildinfo"
	"github.com/ken-kai/discord-control-experiments/internal/cli"
	"github.com/ken-kai/discord-control-experiments/internal/domain"
)

func addJobOperatorHandlers(handlers map[string]application.OperatorHandler, jobs *application.JobService) {
	handlers["job.list"] = func(ctx context.Context, request application.OperatorRequest) (application.OperatorResult, error) {
		records, err := jobs.List(ctx)
		if err != nil {
			return application.OperatorResult{}, err
		}
		if guild := domain.GuildID(request.Values["--guild"]); guild != "" {
			filtered := records[:0]
			for _, record := range records {
				if record.GuildID == guild {
					filtered = append(filtered, record)
				}
			}
			records = filtered
		}
		return application.OperatorResult{Kind: "JobList", Value: records}, nil
	}
	handlers["job.get"] = func(ctx context.Context, request application.OperatorRequest) (application.OperatorResult, error) {
		record, err := jobs.Get(ctx, domain.RunID(request.Values["--run"]))
		return application.OperatorResult{Kind: "Job", Value: record}, err
	}
	handlers["job.cancel"] = func(ctx context.Context, request application.OperatorRequest) (application.OperatorResult, error) {
		record, err := jobs.Cancel(ctx, domain.RunID(request.Values["--run"]))
		return application.OperatorResult{Kind: "Job", Value: record}, err
	}
	handlers["job.resume"] = func(ctx context.Context, request application.OperatorRequest) (application.OperatorResult, error) {
		runID := domain.RunID(request.Values["--run"])
		record, err := jobs.Get(ctx, runID)
		if err != nil {
			return application.OperatorResult{}, err
		}
		result, err := jobs.Resume(ctx, application.ResumeRequest{GuildID: record.GuildID, RunID: runID, PlanHash: domain.PlanHash(request.Values["--plan-hash"]), ActorID: request.Invocation.ActorID, Principal: request.Invocation.Principal, ApprovalID: request.Invocation.ApprovalID, CorrelationID: request.Invocation.CorrelationID})
		return application.OperatorResult{Kind: "Job", Value: result}, err
	}
}

func addSupportOperatorHandlers(handlers map[string]application.OperatorHandler, root string, policyFacade *application.PolicyFacade, health *application.HealthService, diagnostics *application.DiagnosticBundleService, jobs *application.JobService) {
	policyHandler := func(ctx context.Context, request application.OperatorRequest) (application.OperatorResult, error) {
		view, err := policyFacade.Check(ctx, request.Invocation, domain.GuildID(request.Values["--guild"]), domain.Capability(request.Values["--capability"]), domain.ExecutionMode(request.Values["--mode"]))
		return application.OperatorResult{Kind: "PolicyDecision", Value: view}, err
	}
	handlers["policy.check"], handlers["policy.explain"] = policyHandler, policyHandler
	handlers["policy.catalog"] = func(context.Context, application.OperatorRequest) (application.OperatorResult, error) {
		return application.OperatorResult{Kind: "PolicyCatalog", Value: policyFacade.Catalog()}, nil
	}
	handlers["health.local"] = func(ctx context.Context, _ application.OperatorRequest) (application.OperatorResult, error) {
		return application.OperatorResult{Kind: "Health", Value: health.Local(ctx)}, nil
	}
	handlers["health.discord"] = func(ctx context.Context, request application.OperatorRequest) (application.OperatorResult, error) {
		result, err := health.Discord(ctx, request.Invocation, domain.GuildID(request.Values["--guild"]))
		return application.OperatorResult{Kind: "Health", Value: result}, err
	}
	handlers["diagnostics.bundle"] = func(ctx context.Context, request application.OperatorRequest) (application.OperatorResult, error) {
		healthResult := health.Local(ctx)
		jobRecords, err := jobs.List(ctx)
		if err != nil {
			return application.OperatorResult{}, err
		}
		bundle, err := diagnostics.Build(application.DiagnosticInput{Build: map[string]string{"version": buildinfo.Version, "commit": buildinfo.Commit}, ConfigChecks: healthResult.Checks, Health: healthResult, Jobs: jobRecords, FailureClasses: map[string]int{}})
		if err != nil {
			return application.OperatorResult{}, err
		}
		err = cli.AtomicWrite(root, request.Values["--output"], 0o600, func(writer io.Writer) error { _, writeErr := writer.Write(bundle); return writeErr })
		return application.OperatorResult{Kind: "Diagnostics", Value: map[string]any{"written": err == nil, "bytes": len(bundle), "schemaVersion": "discord-control.diagnostics/v1"}}, err
	}
}
