package cli

import (
	"context"
	"io"

	"github.com/ken-kai/discord-control-experiments/internal/domain"

	"github.com/ken-kai/discord-control-experiments/internal/application"
	"github.com/ken-kai/discord-control-experiments/internal/buildinfo"
)

type OperatorExecutor interface {
	Execute(context.Context, application.OperatorRequest) (application.OperatorResult, error)
}

type DestructivePreparer interface {
	PrepareDestructive(context.Context, application.OperatorRequest) (domain.OperationPlan, error)
}
type ApprovalCoordinator interface {
	Issue(context.Context, application.Invocation, domain.OperationPlan, application.ConfirmationSource) (domain.ApprovalGrant, error)
}
type Confirmation func(context.Context, domain.OperationPlan) (bool, error)

type DispatchOptions struct {
	Root            string
	Stdin           io.Reader
	Invocation      application.Invocation
	Version, Commit string
	Approvals       ApprovalCoordinator
	Confirm         Confirmation
}

func Dispatch(ctx context.Context, executor OperatorExecutor, command Command, options DispatchOptions) (application.OperatorResult, error) {
	switch {
	case command.Handler == "version":
		version, commit := options.Version, options.Commit
		if version == "" {
			version = buildinfo.Version
		}
		if commit == "" {
			commit = buildinfo.Commit
		}
		return application.OperatorResult{Kind: "Version", Value: struct {
			Version    string `json:"version"`
			Commit     string `json:"commit"`
			APIVersion string `json:"apiVersion"`
		}{version, commit, APIVersion}}, nil
	case command.Handler == "help":
		help, err := Help("")
		return application.OperatorResult{Kind: "Help", Value: help}, err
	case len(command.Handler) > len("completion.") && command.Handler[:len("completion.")] == "completion.":
		return application.OperatorResult{Kind: "Completion", Value: Completion(command.Handler[len("completion."):])}, nil
	}
	if executor == nil {
		return application.OperatorResult{}, &ParseError{Code: "service_unavailable", Detail: "operator service is unavailable"}
	}
	request := application.OperatorRequest{Handler: command.Handler, Invocation: options.Invocation, Values: cloneValues(command.Values)}
	if _, isDiscord := application.LookupCommandSpec(command.Handler); isDiscord {
		sealed, err := application.NewOperatorDiscordCommand(command.Handler, command.GuildID)
		if err != nil {
			return application.OperatorResult{}, err
		}
		if command.InputPath != "" {
			reader, openErr := OpenInput(options.Root, command.InputPath, options.Stdin)
			if openErr != nil {
				return application.OperatorResult{}, openErr
			}
			limit := DefaultInputLimit
			if command.Name == "message" {
				limit = MessageInputLimit
			}
			decodeErr := DecodeStrictJSON(reader, limit, sealed)
			closeErr := reader.Close()
			if decodeErr != nil {
				return application.OperatorResult{}, decodeErr
			}
			if closeErr != nil {
				return application.OperatorResult{}, &ParseError{Code: "read_input", Detail: "input file could not be closed"}
			}
		}
		if err := application.BindOperatorCommand(sealed, command.GuildID, command.Values); err != nil {
			return application.OperatorResult{}, err
		}
		if err := application.BindOperatorInvocation(sealed, options.Invocation.CorrelationID); err != nil {
			return application.OperatorResult{}, err
		}
		request.Command = sealed
		if command.Definition.Destructive && request.Invocation.RequestedMode == domain.ModeApply {
			preparer, ok := executor.(DestructivePreparer)
			if !ok {
				return application.OperatorResult{}, &domain.InvariantError{Detail: "destructive preparation unavailable"}
			}
			plan, prepareErr := preparer.PrepareDestructive(ctx, request)
			if prepareErr != nil {
				return application.OperatorResult{}, prepareErr
			}
			if request.Invocation.ApprovalID == "" {
				if options.Approvals == nil {
					return application.OperatorResult{}, &domain.AuthorizationError{Reason: domain.PolicyApprovalRequired}
				}
				source := application.ConfirmationInteractive
				if command.NonInteractive {
					if !plan.Preapproved {
						return application.OperatorResult{}, &domain.AuthorizationError{Reason: domain.PolicyApprovalRequired}
					}
					source = application.ConfirmationPolicyPreapproved
				} else {
					if options.Confirm == nil {
						return application.OperatorResult{}, &domain.AuthorizationError{Reason: domain.PolicyApprovalRequired}
					}
					confirmed, confirmErr := options.Confirm(ctx, plan)
					if confirmErr != nil {
						return application.OperatorResult{}, confirmErr
					}
					if !confirmed {
						return application.OperatorResult{}, &domain.AuthorizationError{Reason: domain.PolicyApprovalRequired}
					}
				}
				grant, issueErr := options.Approvals.Issue(ctx, request.Invocation, plan, source)
				if issueErr != nil {
					return application.OperatorResult{}, issueErr
				}
				request.Invocation.ApprovalID = grant.ID
			}
		}
	}
	return executor.Execute(ctx, request)
}

func cloneValues(values map[string]string) map[string]string {
	result := make(map[string]string, len(values))
	for key, value := range values {
		result[key] = value
	}
	return result
}

func Completion(shell string) string {
	paths := ""
	for _, definition := range CommandDefinitions() {
		paths += " " + definition.Path
	}
	switch shell {
	case "powershell":
		return "Register-ArgumentCompleter -Native -CommandName discord-control -ScriptBlock { param($wordToComplete) '" + paths[1:] + "'.Split(' ') | Where-Object { $_ -like \"$wordToComplete*\" } }"
	case "fish":
		return "complete -c discord-control -f -a '" + paths[1:] + "'"
	default:
		return "_discord_control_commands='" + paths[1:] + "'"
	}
}
