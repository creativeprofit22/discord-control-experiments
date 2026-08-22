package discord

import (
	"bytes"
	"encoding/json"
	"strconv"

	"github.com/ken-kai/discord-control-experiments/internal/domain"
)

type discordErrorPayload struct {
	Code       int            `json:"code"`
	Message    string         `json:"message"`
	Errors     map[string]any `json:"errors"`
	RetryAfter json.Number    `json:"retry_after"`
	Global     bool           `json:"global"`
}

func decodeDiscordError(status int, body []byte) error {
	var payload discordErrorPayload
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.UseNumber()
	if err := decoder.Decode(&payload); err != nil {
		return &domain.DiscordAPIError{Status: status}
	}
	fields := make([]domain.DiscordFieldError, 0)
	flattenFields(payload.Errors, "", &fields)
	return &domain.DiscordAPIError{Status: status, Code: payload.Code, Fields: fields}
}
func flattenFields(value any, path string, out *[]domain.DiscordFieldError) {
	node, ok := value.(map[string]any)
	if !ok {
		return
	}
	for key, child := range node {
		next := key
		if path != "" {
			next = path + "." + key
		}
		if key == "_errors" {
			items, ok := child.([]any)
			if !ok {
				continue
			}
			for _, item := range items {
				entry, ok := item.(map[string]any)
				if !ok {
					continue
				}
				*out = append(*out, domain.DiscordFieldError{Path: path, Code: stringValue(entry["code"])})
			}
			continue
		}
		flattenFields(child, next, out)
	}
}
func stringValue(v any) string {
	switch x := v.(type) {
	case string:
		return x
	case json.Number:
		return x.String()
	case float64:
		return strconv.FormatFloat(x, 'f', -1, 64)
	default:
		return ""
	}
}
