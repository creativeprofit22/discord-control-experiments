package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/ken-kai/discord-control-experiments/internal/application"
	"github.com/ken-kai/discord-control-experiments/internal/cli"
	"github.com/ken-kai/discord-control-experiments/internal/config"
	"github.com/ken-kai/discord-control-experiments/internal/domain"
	"github.com/ken-kai/discord-control-experiments/internal/reconcile"
)

func reconciliationOperatorHandlers(command cli.Command, configured config.Resolved, configPath string, service *application.ReconciliationService) map[string]application.OperatorHandler {
	handlers := map[string]application.OperatorHandler{}
	for _, handlerName := range []string{"snapshot", "plan", "drift", "apply", "resume"} {
		handler := handlerName
		handlers[handler] = func(ctx context.Context, request application.OperatorRequest) (application.OperatorResult, error) {
			if request.Handler != handler || command.Handler != handler {
				return application.OperatorResult{}, &domain.InvariantError{Detail: "reconciliation handler mismatch"}
			}
			var rendered bytes.Buffer
			if err := runReconciliationCommand(ctx, command, configured, configPath, service, &rendered); err != nil {
				return application.OperatorResult{}, err
			}
			var value any = map[string]any{"completed": true}
			if payload := bytes.TrimSpace(rendered.Bytes()); len(payload) > 0 {
				if err := json.Unmarshal(payload, &value); err != nil {
					value = map[string]any{"summary": string(payload)}
				}
			}
			return application.OperatorResult{Kind: command.Definition.OutputKind, Value: value}, nil
		}
	}
	return handlers
}

func runReconciliationCommand(ctx context.Context, command cli.Command, configured config.Resolved, configPath string, service *application.ReconciliationService, stdout io.Writer) error {
	principal := domain.Principal{Adapter: "cli", Subject: "local-process", AuthenticationMethod: "os-process"}
	actorID, err := configuredActor(configured.Policy, principal)
	if err != nil {
		return err
	}
	correlationID, err := observabilityCorrelation()
	if err != nil {
		return err
	}
	baseDirectory := filepath.Dir(configPath)
	switch command.Name {
	case "snapshot":
		snapshot, err := service.Snapshot(ctx, application.SnapshotRequest{GuildID: command.GuildID, ApplicationID: command.ApplicationID, Principal: principal, CorrelationID: correlationID})
		if err != nil {
			return err
		}
		encoded, err := reconcile.CanonicalJSON(snapshot)
		if err != nil {
			return err
		}
		return writeContained(baseDirectory, command.OutputPath, encoded)
	case "plan":
		data, err := readContained(baseDirectory, command.DesiredPath, reconcile.MaxDesiredBytes)
		if err != nil {
			return err
		}
		plan, err := service.Plan(ctx, application.ReconciliationPlanRequest{Desired: data, ActorID: actorID, Principal: principal, CorrelationID: correlationID})
		if err != nil {
			return err
		}
		encoded, err := reconcile.RenderPlanJSON(plan)
		if err != nil {
			return err
		}
		if err := writeContained(baseDirectory, command.OutputPath, encoded); err != nil {
			return err
		}
		if command.Format == "json" {
			_, err = fmt.Fprintln(stdout, string(encoded))
		} else {
			_, err = fmt.Fprintln(stdout, plan.Summary)
		}
		return err
	case "drift":
		plan, err := readPlan(baseDirectory, command.PlanPath)
		if err != nil {
			return err
		}
		report, err := service.Drift(ctx, application.DriftRequest{Plan: plan, ActorID: actorID, Principal: principal, CorrelationID: correlationID})
		if err != nil {
			return err
		}
		if command.Format == "json" {
			return writeJSONLine(stdout, report)
		}
		_, err = fmt.Fprintf(stdout, "Plan %s: stale=%t; managed drift=%d; unmanaged conflicts=%d\n", prefixHash(string(report.PlanHash)), report.Stale, len(report.ManagedDrift), len(report.UnmanagedConflicts))
		return err
	case "apply":
		plan, err := readPlan(baseDirectory, command.PlanPath)
		if err != nil {
			return err
		}
		result, err := service.Apply(ctx, domain.ApplyRequest{Plan: plan, PlanHash: command.PlanHash, Principal: principal, ActorID: actorID, ApprovalID: command.ApprovalID, AllowDelete: command.AllowDelete, CorrelationID: correlationID})
		if err != nil {
			return err
		}
		return writeJSONLine(stdout, result)
	case "resume":
		result, err := service.Resume(ctx, application.ResumeRequest{GuildID: configuredGuildForActor(configured.Policy, actorID), RunID: command.RunID, PlanHash: command.PlanHash, ActorID: actorID, Principal: principal, ApprovalID: command.ApprovalID, AllowDelete: command.AllowDelete, CorrelationID: correlationID})
		if err != nil {
			return err
		}
		return writeJSONLine(stdout, result)
	default:
		return errors.New("unsupported reconciliation command")
	}
}

var observabilityCorrelation = func() (domain.CorrelationID, error) { return correlationIDGenerator{}.NewCorrelationID() }

func configuredActor(policy config.Policy, principal domain.Principal) (domain.ActorID, error) {
	for _, actor := range policy.Actors {
		for _, candidate := range actor.Principals {
			if candidate.Adapter == string(principal.Adapter) && candidate.Subject == principal.Subject && candidate.AuthenticationMethod == principal.AuthenticationMethod {
				return domain.ActorID(actor.ID), nil
			}
		}
	}
	return "", errors.New("CLI principal is not configured in policy")
}
func configuredActorMode(policy config.Policy, actorID domain.ActorID) domain.ExecutionMode {
	for _, actor := range policy.Actors {
		if actor.ID == string(actorID) {
			return domain.ExecutionMode(actor.Mode)
		}
	}
	return domain.ModeReadOnly
}
func configuredGuildForActor(policy config.Policy, actorID domain.ActorID) domain.GuildID {
	for _, actor := range policy.Actors {
		if actor.ID == string(actorID) && len(actor.Guilds) == 1 {
			return domain.GuildID(actor.Guilds[0])
		}
	}
	return ""
}

func readPlan(base, path string) (domain.Plan, error) {
	data, err := readContained(base, path, reconcile.MaxPlanBytes)
	if err != nil {
		return domain.Plan{}, err
	}
	return reconcile.ParsePlan(data)
}
func readContained(base, configured string, maximum int) ([]byte, error) {
	path, err := containedPath(base, configured, false)
	if err != nil {
		return nil, err
	}
	// #nosec G304 -- containedPath rejects absolute paths, traversal, and symlink escapes beneath the configuration directory.
	file, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open input file: %w", err)
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, int64(maximum)+1))
	if err != nil {
		return nil, fmt.Errorf("read input file: %w", err)
	}
	if len(data) > maximum {
		return nil, errors.New("input file exceeds size limit")
	}
	return data, nil
}
func writeContained(base, configured string, data []byte) error {
	path, err := containedPath(base, configured, true)
	if err != nil {
		return err
	}
	temporary, err := os.CreateTemp(filepath.Dir(path), ".discord-control-*.tmp")
	if err != nil {
		return fmt.Errorf("create output file: %w", err)
	}
	name := temporary.Name()
	defer os.Remove(name)
	if err := temporary.Chmod(0o600); err != nil {
		return errors.Join(err, temporary.Close())
	}
	if _, err := temporary.Write(data); err != nil {
		return errors.Join(err, temporary.Close())
	}
	if _, err := temporary.Write([]byte{'\n'}); err != nil {
		return errors.Join(err, temporary.Close())
	}
	if err := temporary.Sync(); err != nil {
		return errors.Join(err, temporary.Close())
	}
	if err := temporary.Close(); err != nil {
		return err
	}
	if runtime.GOOS == "windows" {
		_ = os.Remove(path)
	}
	return os.Rename(name, path)
}
func containedPath(base, configured string, output bool) (string, error) {
	if configured == "" || filepath.IsAbs(configured) {
		return "", errors.New("file path must be relative to the configuration directory")
	}
	root, err := filepath.Abs(base)
	if err != nil {
		return "", err
	}
	target, err := filepath.Abs(filepath.Join(root, filepath.Clean(configured)))
	if err != nil {
		return "", err
	}
	relative, err := filepath.Rel(root, target)
	if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return "", errors.New("file path escapes the configuration directory")
	}
	parent := filepath.Dir(target)
	if output {
		if err := os.MkdirAll(parent, 0o700); err != nil {
			return "", err
		}
	}
	resolvedParent, err := filepath.EvalSymlinks(parent)
	if err != nil {
		return "", fmt.Errorf("resolve file parent: %w", err)
	}
	if !strings.HasPrefix(strings.ToLower(resolvedParent+string(filepath.Separator)), strings.ToLower(root+string(filepath.Separator))) && !strings.EqualFold(resolvedParent, root) {
		return "", errors.New("file path crosses a symbolic link")
	}
	if info, err := os.Lstat(target); err == nil && info.Mode()&os.ModeSymlink != 0 {
		return "", errors.New("file path is a symbolic link")
	} else if err != nil && !os.IsNotExist(err) {
		return "", err
	}
	return target, nil
}
func writeJSONLine(writer io.Writer, value any) error {
	encoder := json.NewEncoder(writer)
	encoder.SetEscapeHTML(false)
	return encoder.Encode(value)
}
func prefixHash(value string) string {
	if len(value) > 12 {
		return value[:12]
	}
	return value
}
