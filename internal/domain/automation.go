package domain

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"
)

const AutomationVersion = 1

const (
	TriggerManual     = "manual"
	TriggerOnce       = "once"
	TriggerInterval   = "interval"
	TriggerCalendar   = "calendar"
	TriggerNews       = "news"
	TriggerMemberScan = "member_scan"
)

const (
	ConditionSourceKind = "source_kind"
	ConditionTextHas    = "text_contains"
	ConditionMemberRole = "member_has_role"
)

const (
	TransformRender    = "render"
	TransformSummarize = "summarize"
)

const (
	ActionMessage     = "message"
	ActionWebhook     = "webhook"
	ActionForumThread = "forum_thread"
	ActionMediaThread = "media_thread"
	ActionPoll        = "poll"
	ActionWelcome     = "welcome"
	ActionRoleAdd     = "role_add"
	ActionRoleRemove  = "role_remove"
	ActionCleanup     = "cleanup"
	ActionSnapshot    = "snapshot"
)

const (
	AutomationRunQueued          = "queued"
	AutomationRunLeased          = "leased"
	AutomationRunRunning         = "running"
	AutomationRunWaitingApproval = "waiting_approval"
	AutomationRunRetryWaiting    = "retry_waiting"
	AutomationRunPaused          = "paused"
	AutomationRunSucceeded       = "succeeded"
	AutomationRunFailed          = "failed"
	AutomationRunUncertain       = "uncertain"
	AutomationRunDeadLetter      = "dead_letter"
)

type AutomationTrigger struct {
	Kind      string   `json:"kind"`
	At        string   `json:"at,omitempty"`
	Every     string   `json:"every,omitempty"`
	Weekdays  []string `json:"weekdays,omitempty"`
	Time      string   `json:"time,omitempty"`
	Source    string   `json:"source,omitempty"`
	ScanEvery string   `json:"scanEvery,omitempty"`
	Limit     int      `json:"limit,omitempty"`
}

type AutomationCondition struct {
	Kind   string `json:"kind"`
	Value  string `json:"value,omitempty"`
	RoleID string `json:"roleId,omitempty"`
}

type AutomationTransform struct {
	Kind     string `json:"kind"`
	Template string `json:"template,omitempty"`
	MaxBytes int    `json:"maxBytes,omitempty"`
}

type AutomationAction struct {
	Kind          string   `json:"kind"`
	ChannelID     string   `json:"channelId,omitempty"`
	RoleID        string   `json:"roleId,omitempty"`
	UserID        string   `json:"userId,omitempty"`
	Webhook       string   `json:"webhook,omitempty"`
	Content       string   `json:"content,omitempty"`
	Title         string   `json:"title,omitempty"`
	Choices       []string `json:"choices,omitempty"`
	Age           string   `json:"age,omitempty"`
	Limit         int      `json:"limit,omitempty"`
	RequireReview bool     `json:"requireReview,omitempty"`
}

type AutomationStep struct {
	Conditions []AutomationCondition `json:"conditions,omitempty"`
	Transforms []AutomationTransform `json:"transforms,omitempty"`
	Action     AutomationAction      `json:"action"`
}

type AutomationRetry struct {
	MaxAttempts int    `json:"maxAttempts"`
	Initial     string `json:"initial"`
	Maximum     string `json:"maximum"`
}

type AutomationTargets struct {
	GuildIDs []string `json:"guildIds"`
}

type AutomationWorkflow struct {
	Version int               `json:"version"`
	ID      string            `json:"id"`
	Enabled bool              `json:"enabled"`
	Trigger AutomationTrigger `json:"trigger"`
	Targets AutomationTargets `json:"targets"`
	Retry   AutomationRetry   `json:"retry"`
	Steps   []AutomationStep  `json:"steps"`
}

type AutomationProvenance struct {
	WorkflowID string    `json:"workflowId"`
	Definition string    `json:"definitionHash"`
	TriggerID  string    `json:"triggerId"`
	SourceID   string    `json:"sourceId,omitempty"`
	SourceURL  string    `json:"sourceUrl,omitempty"`
	SourceTime time.Time `json:"sourceTime,omitempty"`
}

type AutomationChildResult struct {
	GuildID       string `json:"guildId"`
	State         string `json:"state"`
	CompletedStep int    `json:"completedStep"`
	Error         string `json:"error,omitempty"`
}

type AutomationSchedule struct {
	WorkflowID   string    `json:"workflowId"`
	WorkflowHash string    `json:"workflowHash"`
	Revision     uint64    `json:"revision"`
	NextRun      time.Time `json:"nextRun"`
	Cursor       string    `json:"cursor,omitempty"`
}

type AutomationLease struct {
	Token   string    `json:"token"`
	Owner   string    `json:"owner"`
	Expires time.Time `json:"expires"`
}

type AutomationSourcePayload struct {
	ID        string    `json:"id,omitempty"`
	Kind      string    `json:"kind,omitempty"`
	Title     string    `json:"title,omitempty"`
	Text      string    `json:"text,omitempty"`
	Link      string    `json:"link,omitempty"`
	Published time.Time `json:"published,omitempty"`
}

type AutomationRun struct {
	ID                    string                  `json:"id"`
	WorkflowHash          string                  `json:"workflowHash"`
	Revision              uint64                  `json:"revision"`
	State                 string                  `json:"state"`
	Attempt               int                     `json:"attempt"`
	IdempotencyKey        string                  `json:"idempotencyKey"`
	Lease                 AutomationLease         `json:"lease,omitempty"`
	NextAttempt           time.Time               `json:"nextAttempt,omitempty"`
	SourceCursor          string                  `json:"sourceCursor,omitempty"`
	SourcePayload         AutomationSourcePayload `json:"sourcePayload,omitempty"`
	TriggerMemberID       string                  `json:"triggerMemberId,omitempty"`
	TriggerMemberRoleIDs  []string                `json:"triggerMemberRoleIds,omitempty"`
	MemberCursor          string                  `json:"memberCursor,omitempty"`
	CompletedSteps        []string                `json:"completedSteps,omitempty"`
	RenderedOperationHash string                  `json:"renderedOperationHash,omitempty"`
	RenderedContent       string                  `json:"renderedContent,omitempty"`
	PendingUserID         string                  `json:"pendingUserId,omitempty"`
	PendingRoleIDs        []string                `json:"pendingRoleIds,omitempty"`
	Provenance            AutomationProvenance    `json:"provenance"`
	Children              []AutomationChildResult `json:"children,omitempty"`
	Error                 string                  `json:"error,omitempty"`
}

type AutomationJournalEvent struct {
	Sequence     uint64    `json:"sequence"`
	RunID        string    `json:"runId"`
	WorkflowHash string    `json:"workflowHash"`
	State        string    `json:"state"`
	StepID       string    `json:"stepId,omitempty"`
	At           time.Time `json:"at"`
	PreviousHash string    `json:"previousHash,omitempty"`
	Hash         string    `json:"hash"`
}

func DecodeAutomationWorkflow(reader io.Reader, maxBytes int64) (AutomationWorkflow, error) {
	if reader == nil || maxBytes < 1 {
		return AutomationWorkflow{}, errors.New("workflow reader and positive byte limit are required")
	}
	data, err := io.ReadAll(io.LimitReader(reader, maxBytes+1))
	if err != nil {
		return AutomationWorkflow{}, fmt.Errorf("read workflow: %w", err)
	}
	if int64(len(data)) > maxBytes {
		return AutomationWorkflow{}, errors.New("workflow exceeds configured byte limit")
	}
	if err := rejectDuplicateAutomationKeys(data); err != nil {
		return AutomationWorkflow{}, err
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	var workflow AutomationWorkflow
	if err := decoder.Decode(&workflow); err != nil {
		return AutomationWorkflow{}, fmt.Errorf("decode workflow: %w", err)
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		return AutomationWorkflow{}, errors.New("workflow contains trailing JSON")
	}
	if err := workflow.Validate(100, 16); err != nil {
		return AutomationWorkflow{}, err
	}
	return workflow, nil
}

func (w AutomationWorkflow) Validate(maxSteps, maxGuilds int) error {
	if w.Version != AutomationVersion || !automationIdentifier(w.ID, 64) {
		return errors.New("workflow version must be 1 and id must be bounded")
	}
	if len(w.Steps) < 1 || len(w.Steps) > maxSteps || len(w.Targets.GuildIDs) < 1 || len(w.Targets.GuildIDs) > maxGuilds {
		return errors.New("workflow steps or guild targets exceed limits")
	}
	seenGuilds := map[string]bool{}
	for _, id := range w.Targets.GuildIDs {
		if !automationSnowflake(id) || seenGuilds[id] {
			return errors.New("workflow contains invalid or duplicate guild target")
		}
		seenGuilds[id] = true
	}
	if err := validateAutomationTrigger(w.Trigger); err != nil {
		return err
	}
	if err := validateAutomationRetry(w.Retry); err != nil {
		return err
	}
	for _, step := range w.Steps {
		if len(step.Conditions) > 16 || len(step.Transforms) > 8 {
			return errors.New("workflow step exceeds condition or transform limits")
		}
		for _, condition := range step.Conditions {
			switch condition.Kind {
			case ConditionSourceKind, ConditionTextHas:
				if condition.Value == "" || len(condition.Value) > 256 || condition.RoleID != "" {
					return errors.New("invalid workflow condition")
				}
			case ConditionMemberRole:
				if !automationSnowflake(condition.RoleID) || condition.Value != "" {
					return errors.New("invalid member role condition")
				}
			default:
				return errors.New("unknown workflow condition kind")
			}
		}
		for _, transform := range step.Transforms {
			switch transform.Kind {
			case TransformRender:
				if len(transform.Template) > 4000 || strings.Contains(transform.Template, "{{") || transform.MaxBytes != 0 {
					return errors.New("render transform must be static text")
				}
			case TransformSummarize:
				if transform.Template != "" || transform.MaxBytes < 1 || transform.MaxBytes > 4000 {
					return errors.New("invalid summarize transform limit")
				}
			default:
				return errors.New("unknown workflow transform kind")
			}
		}
		if err := validateAutomationAction(step.Action); err != nil {
			return err
		}
	}
	return nil
}

func validateAutomationTrigger(trigger AutomationTrigger) error {
	baseExtra := trigger.At != "" || trigger.Every != "" || len(trigger.Weekdays) != 0 || trigger.Time != "" || trigger.Source != "" || trigger.ScanEvery != "" || trigger.Limit != 0
	switch trigger.Kind {
	case TriggerManual:
		if baseExtra {
			return errors.New("manual trigger has unsupported fields")
		}
	case TriggerOnce:
		if trigger.Every != "" || len(trigger.Weekdays) != 0 || trigger.Time != "" || trigger.Source != "" || trigger.ScanEvery != "" || trigger.Limit != 0 {
			return errors.New("once trigger has unsupported fields")
		}
		if _, err := time.Parse(time.RFC3339, trigger.At); err != nil {
			return errors.New("once trigger requires RFC3339 at")
		}
	case TriggerInterval:
		if trigger.At != "" || len(trigger.Weekdays) != 0 || trigger.Time != "" || trigger.Source != "" || trigger.ScanEvery != "" || trigger.Limit != 0 {
			return errors.New("interval trigger has unsupported fields")
		}
		d, err := time.ParseDuration(trigger.Every)
		if err != nil || d < time.Minute || d > 30*24*time.Hour {
			return errors.New("interval must be between 1m and 720h")
		}
	case TriggerCalendar:
		if trigger.At != "" || trigger.Every != "" || trigger.Source != "" || trigger.ScanEvery != "" || trigger.Limit != 0 {
			return errors.New("calendar trigger has unsupported fields")
		}
		if _, err := time.Parse("15:04", trigger.Time); err != nil || len(trigger.Weekdays) > 7 {
			return errors.New("calendar trigger requires HH:MM UTC and bounded weekdays")
		}
		seen := map[string]bool{}
		valid := map[string]bool{"monday": true, "tuesday": true, "wednesday": true, "thursday": true, "friday": true, "saturday": true, "sunday": true}
		for _, weekday := range trigger.Weekdays {
			weekday = strings.ToLower(weekday)
			if !valid[weekday] || seen[weekday] {
				return errors.New("calendar contains invalid or duplicate weekday")
			}
			seen[weekday] = true
		}
	case TriggerNews:
		if trigger.At != "" || trigger.Every != "" || len(trigger.Weekdays) != 0 || trigger.Time != "" || trigger.ScanEvery != "" {
			return errors.New("news trigger has unsupported fields")
		}
		if !automationIdentifier(trigger.Source, 64) || trigger.Limit < 1 || trigger.Limit > 100 {
			return errors.New("news trigger requires configured source and bounded limit")
		}
	case TriggerMemberScan:
		if trigger.At != "" || trigger.Every != "" || len(trigger.Weekdays) != 0 || trigger.Time != "" || trigger.Source != "" {
			return errors.New("member scan trigger has unsupported fields")
		}
		d, err := time.ParseDuration(trigger.ScanEvery)
		if err != nil || d < 5*time.Minute || d > 24*time.Hour || trigger.Limit < 1 || trigger.Limit > 1000 {
			return errors.New("member scan trigger exceeds bounds")
		}
	default:
		return errors.New("unknown workflow trigger kind")
	}
	return nil
}

func validateAutomationRetry(retry AutomationRetry) error {
	initial, e1 := time.ParseDuration(retry.Initial)
	maximum, e2 := time.ParseDuration(retry.Maximum)
	if retry.MaxAttempts < 1 || retry.MaxAttempts > 10 || e1 != nil || e2 != nil || initial < time.Second || maximum < initial || maximum > 24*time.Hour {
		return errors.New("invalid workflow retry policy")
	}
	return nil
}

func validateAutomationAction(action AutomationAction) error {
	if len(action.Content) > 4000 || len(action.Title) > 100 || len(action.Choices) > 10 || action.Limit < 0 || action.Limit > 1000 {
		return errors.New("workflow action exceeds limits")
	}
	channel := automationSnowflake(action.ChannelID)
	switch action.Kind {
	case ActionMessage, ActionWelcome:
		if !channel || action.RoleID != "" || action.UserID != "" || action.Webhook != "" || action.Title != "" || len(action.Choices) != 0 || action.Age != "" || action.Limit != 0 {
			return errors.New("message action has unsupported or invalid fields")
		}
	case ActionForumThread, ActionMediaThread:
		if !channel || action.Title == "" || action.RoleID != "" || action.UserID != "" || action.Webhook != "" || len(action.Choices) != 0 || action.Age != "" || action.Limit != 0 {
			return errors.New("thread action has unsupported or invalid fields")
		}
	case ActionPoll:
		if !channel || action.Title == "" || len(action.Choices) < 2 || len(action.Choices) > 10 || action.RoleID != "" || action.UserID != "" || action.Webhook != "" || action.Age != "" || action.Limit != 0 {
			return errors.New("poll action has unsupported or invalid fields")
		}
	case ActionWebhook:
		if !automationIdentifier(action.Webhook, 64) || action.ChannelID != "" || action.RoleID != "" || action.UserID != "" || action.Title != "" || len(action.Choices) != 0 || action.Age != "" || action.Limit != 0 {
			return errors.New("webhook action has unsupported or invalid fields")
		}
	case ActionRoleAdd, ActionRoleRemove:
		if !automationSnowflake(action.RoleID) || (action.UserID != "" && !automationSnowflake(action.UserID)) || action.ChannelID != "" || action.Webhook != "" || action.Content != "" || action.Title != "" || len(action.Choices) != 0 || action.Age != "" || action.Limit != 0 {
			return errors.New("role action has unsupported or invalid fields")
		}
	case ActionCleanup:
		age, err := time.ParseDuration(action.Age)
		if !channel || err != nil || age < time.Hour || action.Limit < 1 || action.Limit > 100 || action.RoleID != "" || action.UserID != "" || action.Webhook != "" || action.Content != "" || action.Title != "" || len(action.Choices) != 0 {
			return errors.New("cleanup action has unsupported or invalid fields")
		}
	case ActionSnapshot:
		if action.ChannelID != "" || action.RoleID != "" || action.UserID != "" || action.Webhook != "" || action.Content != "" || action.Title != "" || len(action.Choices) != 0 || action.Age != "" || action.Limit != 0 || action.RequireReview {
			return errors.New("snapshot action has unsupported fields")
		}
	default:
		return errors.New("unknown workflow action kind")
	}
	return nil
}

func (w AutomationWorkflow) Hash() (string, error) {
	if err := w.Validate(100, 16); err != nil {
		return "", err
	}
	encoded, err := json.Marshal(w)
	if err != nil {
		return "", err
	}
	digest := sha256.Sum256(encoded)
	return hex.EncodeToString(digest[:]), nil
}

func ValidAutomationTransition(from, to string) bool {
	allowed := map[string]map[string]bool{
		AutomationRunQueued:          {AutomationRunLeased: true},
		AutomationRunLeased:          {AutomationRunRunning: true, AutomationRunQueued: true},
		AutomationRunRunning:         {AutomationRunWaitingApproval: true, AutomationRunRetryWaiting: true, AutomationRunPaused: true, AutomationRunSucceeded: true, AutomationRunFailed: true, AutomationRunUncertain: true, AutomationRunDeadLetter: true},
		AutomationRunWaitingApproval: {AutomationRunQueued: true, AutomationRunFailed: true},
		AutomationRunRetryWaiting:    {AutomationRunQueued: true, AutomationRunDeadLetter: true},
		AutomationRunPaused:          {AutomationRunQueued: true, AutomationRunFailed: true},
		AutomationRunUncertain:       {AutomationRunQueued: true, AutomationRunFailed: true},
		AutomationRunFailed:          {AutomationRunQueued: true, AutomationRunDeadLetter: true},
	}
	return allowed[from][to]
}

func rejectDuplicateAutomationKeys(data []byte) error {
	decoder := json.NewDecoder(bytes.NewReader(data))
	var walk func() error
	walk = func() error {
		token, err := decoder.Token()
		if err != nil {
			return err
		}
		delimiter, ok := token.(json.Delim)
		if !ok {
			return nil
		}
		switch delimiter {
		case '{':
			seen := map[string]bool{}
			for decoder.More() {
				keyToken, keyErr := decoder.Token()
				if keyErr != nil {
					return keyErr
				}
				key, ok := keyToken.(string)
				if !ok || seen[key] {
					return errors.New("workflow contains duplicate JSON key")
				}
				seen[key] = true
				if err := walk(); err != nil {
					return err
				}
			}
			_, err = decoder.Token()
			return err
		case '[':
			for decoder.More() {
				if err := walk(); err != nil {
					return err
				}
			}
			_, err = decoder.Token()
			return err
		default:
			return errors.New("workflow contains invalid JSON delimiter")
		}
	}
	if err := walk(); err != nil {
		return fmt.Errorf("decode workflow: %w", err)
	}
	if _, err := decoder.Token(); !errors.Is(err, io.EOF) {
		return errors.New("workflow contains trailing JSON")
	}
	return nil
}

func automationIdentifier(value string, max int) bool {
	return value != "" && len(value) <= max && strings.TrimSpace(value) == value && !strings.ContainsAny(value, "*\\/\r\n\x00")
}
func automationSnowflake(value string) bool {
	if value == "" || value[0] < '1' || value[0] > '9' {
		return false
	}
	for _, r := range value {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}
