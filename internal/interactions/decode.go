package interactions

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"regexp"

	"github.com/ken-kai/discord-control-experiments/internal/domain"
)

var snowflakePattern = regexp.MustCompile(`^[1-9][0-9]{0,19}$`)

type VerifiedInteraction struct {
	Envelope      Envelope
	Command       *CommandData
	Component     *ComponentData
	Modal         *ModalData
	RawBodyDigest [32]byte
	KeySlot       int
	CorrelationID domain.CorrelationID
	TraceContext  context.Context
}

func DecodeVerified(body []byte, configuredApplicationID string) (VerifiedInteraction, error) {
	var envelope Envelope
	if err := strictJSON(body, &envelope); err != nil {
		return VerifiedInteraction{}, err
	}
	if !snowflakePattern.MatchString(envelope.ID) || !snowflakePattern.MatchString(envelope.ApplicationID) || envelope.ApplicationID != configuredApplicationID || len(envelope.Token) < 1 || len(envelope.Token) > 256 {
		return VerifiedInteraction{}, errors.New("invalid interaction envelope")
	}
	result := VerifiedInteraction{Envelope: envelope}
	if envelope.Type == InteractionPing {
		if len(envelope.Data) != 0 && string(envelope.Data) != "null" && string(envelope.Data) != "{}" {
			return result, errors.New("ping data is not empty")
		}
		return result, nil
	}
	if !snowflakePattern.MatchString(envelope.GuildID) || !snowflakePattern.MatchString(envelope.Member.User.ID) || (envelope.Member.GuildID != "" && envelope.Member.GuildID != envelope.GuildID) {
		return result, errors.New("guild interaction identity is invalid")
	}
	switch envelope.Type {
	case InteractionApplicationCommand, InteractionAutocomplete:
		var data CommandData
		if err := strictJSON(envelope.Data, &data); err != nil || !snowflakePattern.MatchString(data.ID) || data.Name != "discord-control" || data.Type != 1 || len(data.Options) > 25 {
			return result, errors.New("invalid application command data")
		}
		if err := validateCommandOptions(data.Options, 0); err != nil {
			return result, err
		}
		result.Command = &data
	case InteractionMessageComponent:
		var data ComponentData
		if err := strictJSON(envelope.Data, &data); err != nil || data.ComponentType < 2 || data.ComponentType > 8 || len(data.CustomID) < 3 || len(data.CustomID) > 100 || len(data.Values) > 25 {
			return result, errors.New("invalid component data")
		}
		for _, value := range data.Values {
			if len(value) < 1 || len(value) > 100 {
				return result, errors.New("invalid component value")
			}
		}
		result.Component = &data
	case InteractionModalSubmit:
		var data ModalData
		if err := strictJSON(envelope.Data, &data); err != nil || len(data.CustomID) < 3 || len(data.CustomID) > 100 || len(data.Components) > 5 || !validModalComponents(data.Components, 0) {
			return result, errors.New("invalid modal data")
		}
		result.Modal = &data
	default:
		return result, errors.New("unsupported interaction type")
	}
	return result, nil
}

func strictJSON(body []byte, target any) error {
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return err
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return errors.New("trailing JSON")
	}
	return nil
}

func validateCommandOptions(options []CommandOption, depth int) error {
	if depth > 2 || len(options) > 25 {
		return errors.New("command option limits exceeded")
	}
	seen := map[string]bool{}
	for _, option := range options {
		if !commandNamePattern.MatchString(option.Name) || seen[option.Name] || option.Type < 1 || option.Type > 11 || len(option.Value) > 1024 {
			return errors.New("invalid command option")
		}
		seen[option.Name] = true
		if len(option.Options) > 0 {
			if err := validateCommandOptions(option.Options, depth+1); err != nil {
				return err
			}
		}
	}
	return nil
}

func validModalComponents(components []ModalComponent, depth int) bool {
	if depth > 2 || len(components) > 5 {
		return false
	}
	for _, component := range components {
		if component.Type < 1 || component.Type > 18 || len(component.CustomID) > 100 || len(component.Value) > 4000 || !validModalComponents(component.Components, depth+1) {
			return false
		}
	}
	return true
}
