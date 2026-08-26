package upgrade

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/ken-kai/discord-control-experiments/internal/audit"
	"github.com/ken-kai/discord-control-experiments/internal/config"
	"github.com/ken-kai/discord-control-experiments/internal/domain"
	"github.com/ken-kai/discord-control-experiments/internal/state"
)

type Options struct {
	ConfigPath string
	Resolver   config.SecretResolver
	BackupPath string
}

type Report struct {
	ConfigVersion int `json:"configVersion"`
	WorkflowCount int `json:"workflowCount"`
	BackupVersion int `json:"backupVersion,omitempty"`
}

func Validate(options Options) (Report, error) {
	configPath, err := filepath.Abs(options.ConfigPath)
	if err != nil || options.Resolver == nil {
		return Report{}, errors.New("config path and secret resolver are required")
	}
	file, closeFile, err := openScoped(configPath)
	if err != nil {
		return Report{}, err
	}
	resolved, loadErr := config.Load(file, options.Resolver)
	closeErr := closeFile()
	if loadErr != nil || closeErr != nil {
		return Report{}, errors.Join(loadErr, closeErr)
	}
	root := filepath.Dir(configPath)
	report := Report{ConfigVersion: resolved.Version}
	for _, configured := range resolved.Automation.WorkflowPaths {
		filename, err := containedPath(root, configured)
		if err != nil {
			return Report{}, err
		}
		workflow, err := openWorkflow(filename, resolved.Automation.MaxWorkflowBytes)
		if err != nil {
			return Report{}, err
		}
		if _, err := workflow.Hash(); err != nil {
			return Report{}, fmt.Errorf("validate workflow: %w", err)
		}
		report.WorkflowCount++
	}
	auditPath, err := containedPath(root, resolved.Audit.Path)
	if err != nil {
		return Report{}, err
	}
	if _, err := os.Stat(auditPath); err != nil {
		return Report{}, fmt.Errorf("validate audit path: %w", err)
	}
	sink, err := audit.Open(root, resolved.Audit.Path, resolved.Audit.MaxBytes, nil)
	if err != nil {
		return Report{}, fmt.Errorf("validate audit chain: %w", err)
	}
	if err := sink.Close(); err != nil {
		return Report{}, err
	}
	stateRoot, err := containedPath(root, resolved.Reconciliation.StateDirectory)
	if err != nil {
		return Report{}, err
	}
	if err := state.ValidateStateTree(stateRoot); err != nil {
		return Report{}, fmt.Errorf("validate state: %w", err)
	}
	if options.BackupPath != "" {
		manifest, err := state.VerifyBackup(options.BackupPath)
		if err != nil {
			return Report{}, err
		}
		report.BackupVersion = manifest.SchemaVersion
	}
	return report, nil
}

func containedPath(root, configured string) (string, error) {
	if configured == "" || filepath.IsAbs(configured) {
		return "", errors.New("validated paths must be relative to the configuration directory")
	}
	candidate, err := filepath.Abs(filepath.Join(root, filepath.Clean(configured)))
	if err != nil {
		return "", err
	}
	relative, err := filepath.Rel(root, candidate)
	if err != nil || relative == ".." || len(relative) > 3 && relative[:3] == ".."+string(filepath.Separator) {
		return "", errors.New("validated path escaped configuration directory")
	}
	resolvedRoot, rootErr := filepath.EvalSymlinks(root)
	resolved, resolveErr := filepath.EvalSymlinks(candidate)
	if rootErr != nil || resolveErr != nil {
		return "", errors.Join(rootErr, resolveErr)
	}
	resolvedRelative, err := filepath.Rel(resolvedRoot, resolved)
	if err != nil || resolvedRelative == ".." || len(resolvedRelative) > 3 && resolvedRelative[:3] == ".."+string(filepath.Separator) {
		return "", errors.New("validated path escaped configuration directory through a symbolic link")
	}
	return resolved, nil
}

func openWorkflow(filename string, maximum int64) (domain.AutomationWorkflow, error) {
	file, closeFile, err := openScoped(filename)
	if err != nil {
		return domain.AutomationWorkflow{}, err
	}
	workflow, decodeErr := domain.DecodeAutomationWorkflow(file, maximum)
	return workflow, errors.Join(decodeErr, closeFile())
}

func openScoped(filename string) (*os.File, func() error, error) {
	absolute, err := filepath.Abs(filename)
	if err != nil {
		return nil, nil, err
	}
	root, err := os.OpenRoot(filepath.Dir(absolute))
	if err != nil {
		return nil, nil, err
	}
	file, err := root.Open(filepath.Base(absolute))
	if err != nil {
		_ = root.Close()
		return nil, nil, err
	}
	return file, func() error { return errors.Join(file.Close(), root.Close()) }, nil
}
