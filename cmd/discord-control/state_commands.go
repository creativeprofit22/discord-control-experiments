package main

import (
	"context"
	"os"
	"path/filepath"

	"github.com/ken-kai/discord-control-experiments/internal/application"
	"github.com/ken-kai/discord-control-experiments/internal/cli"
	"github.com/ken-kai/discord-control-experiments/internal/config"
	"github.com/ken-kai/discord-control-experiments/internal/state"
	"github.com/ken-kai/discord-control-experiments/internal/upgrade"
)

func runPortableStateCommand(command cli.Command) (application.OperatorResult, error) {
	switch command.Handler {
	case "state.verify":
		manifest, err := state.VerifyBackup(command.Values["--backup"])
		return application.OperatorResult{Kind: "StateBackupVerification", Value: manifest}, err
	case "state.restore":
		manifest, err := state.RestoreBackup(command.Values["--backup"], command.Values["--staging"])
		return application.OperatorResult{Kind: "StateRestore", Value: manifest}, err
	default:
		return application.OperatorResult{}, nil
	}
}

func runConfiguredStateCommand(_ context.Context, command cli.Command, resolved config.Resolved) (application.OperatorResult, error) {
	configPath, err := filepath.Abs(command.ConfigPath)
	if err != nil {
		return application.OperatorResult{}, err
	}
	root := filepath.Dir(configPath)
	switch command.Handler {
	case "state.backup":
		manifest, err := state.CreateBackup(state.BackupOptions{ConfigDirectory: root, ConfigPath: configPath, WorkflowPaths: resolved.Automation.WorkflowPaths, AuditPath: resolved.Audit.Path, StateDirectory: resolved.Reconciliation.StateDirectory, Destination: command.Values["--output"]})
		return application.OperatorResult{Kind: "StateBackup", Value: manifest}, err
	case "state.validate":
		report, err := upgrade.Validate(upgrade.Options{ConfigPath: configPath, Resolver: config.EnvironmentResolver{Lookup: os.LookupEnv}, BackupPath: command.Values["--backup"]})
		return application.OperatorResult{Kind: "StateValidation", Value: report}, err
	default:
		return application.OperatorResult{}, nil
	}
}
