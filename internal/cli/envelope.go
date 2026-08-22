package cli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"time"

	"github.com/ken-kai/discord-control-experiments/internal/domain"
)

const (
	ExitSuccess        = 0
	ExitInput          = 2
	ExitPolicy         = 3
	ExitDiscord        = 4
	ExitReconciliation = 5
	ExitTimeout        = 6
	ExitInternal       = 7
	ExitInterrupted    = 130
)

type ErrorCode string

const (
	ErrorInput          ErrorCode = "input_invalid"
	ErrorPolicy         ErrorCode = "policy_denied"
	ErrorApproval       ErrorCode = "approval_required"
	ErrorDiscord        ErrorCode = "discord_failed"
	ErrorReconciliation ErrorCode = "reconciliation_conflict"
	ErrorTimeout        ErrorCode = "timeout"
	ErrorInterrupted    ErrorCode = "interrupted"
	ErrorInternal       ErrorCode = "internal"
)

type CLIError struct {
	Code      ErrorCode `json:"code"`
	Message   string    `json:"message"`
	Retryable bool      `json:"retryable,omitempty"`
}

type Timing struct {
	DurationMS int64 `json:"durationMs"`
}
type Recovery struct {
	Partial     bool `json:"partial"`
	Recoverable bool `json:"recoverable"`
}

type Envelope struct {
	APIVersion    string               `json:"apiVersion"`
	Kind          string               `json:"kind"`
	CorrelationID domain.CorrelationID `json:"correlationId"`
	Result        any                  `json:"result,omitempty"`
	Error         *CLIError            `json:"error,omitempty"`
	Timing        *Timing              `json:"timing,omitempty"`
	Recovery      *Recovery            `json:"recovery,omitempty"`
}

func SuccessEnvelope(kind string, correlationID domain.CorrelationID, result any) Envelope {
	return Envelope{APIVersion: APIVersion, Kind: kind, CorrelationID: correlationID, Result: result}
}

func FailureEnvelope(correlationID domain.CorrelationID, err error) Envelope {
	code, message, retryable := classify(err)
	return Envelope{APIVersion: APIVersion, Kind: "Error", CorrelationID: correlationID, Error: &CLIError{Code: code, Message: message, Retryable: retryable}}
}

func classify(err error) (ErrorCode, string, bool) {
	var parse *ParseError
	var authorization *domain.AuthorizationError
	var cancellation *domain.CancellationError
	switch {
	case errors.Is(err, ErrInterrupted), errors.Is(err, context.Canceled):
		return ErrorInterrupted, "operation interrupted", false
	case errors.Is(err, context.DeadlineExceeded):
		return ErrorTimeout, "operation timed out", true
	case errors.As(err, &parse):
		return ErrorInput, "command input is invalid", false
	case errors.As(err, &authorization):
		if authorization.Reason == domain.PolicyApprovalRequired || authorization.Reason == domain.PolicyApprovalInvalid {
			return ErrorApproval, "approval is required or invalid", false
		}
		return ErrorPolicy, "operation is not permitted", false
	case errors.As(err, &cancellation):
		if errors.Is(cancellation.Err, context.DeadlineExceeded) {
			return ErrorTimeout, "operation timed out", true
		}
		return ErrorInterrupted, "operation interrupted", false
	}
	// Typed package errors that implement stable classes can opt in without text matching.
	var classified interface{ CLIErrorClass() string }
	if errors.As(err, &classified) {
		switch classified.CLIErrorClass() {
		case "discord":
			return ErrorDiscord, "Discord operation failed", true
		case "reconciliation", "conflict":
			return ErrorReconciliation, "reconciliation could not complete", false
		case "policy":
			return ErrorPolicy, "operation is not permitted", false
		}
	}
	return ErrorInternal, "operation failed", false
}

func FailureClass(err error) string {
	code, _, _ := classify(err)
	switch code {
	case ErrorInput:
		return "parse"
	case ErrorPolicy, ErrorApproval:
		return "policy"
	case ErrorDiscord:
		return "discord"
	case ErrorReconciliation:
		return "reconciliation"
	case ErrorTimeout:
		return "timeout"
	case ErrorInterrupted:
		return "interrupted"
	default:
		return "internal"
	}
}

func ExitCode(err error) int {
	if err == nil {
		return ExitSuccess
	}
	code, _, _ := classify(err)
	switch code {
	case ErrorInput:
		return ExitInput
	case ErrorPolicy, ErrorApproval:
		return ExitPolicy
	case ErrorDiscord:
		return ExitDiscord
	case ErrorReconciliation:
		return ExitReconciliation
	case ErrorTimeout:
		return ExitTimeout
	case ErrorInterrupted:
		return ExitInterrupted
	default:
		return ExitInternal
	}
}

func Render(w io.Writer, format string, envelope Envelope) error {
	if format == "json" {
		encoder := json.NewEncoder(w)
		encoder.SetEscapeHTML(true)
		return encoder.Encode(envelope)
	}
	if envelope.Error != nil {
		_, err := fmt.Fprintf(w, "Error [%s] (%s): %s\n", envelope.CorrelationID, envelope.Error.Code, envelope.Error.Message)
		return err
	}
	if stringValue, ok := envelope.Result.(string); ok {
		_, err := fmt.Fprintln(w, stringValue)
		return err
	}
	payload, err := json.MarshalIndent(envelope.Result, "", "  ")
	if err != nil {
		return err
	}
	_, err = fmt.Fprintf(w, "%s\n", payload)
	return err
}

func WithTiming(envelope Envelope, started time.Time) Envelope {
	envelope.Timing = &Timing{DurationMS: time.Since(started).Milliseconds()}
	return envelope
}
