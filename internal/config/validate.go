package config

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
)

func validate(file File) error {
	if file.Version != 1 {
		return errors.New("configuration version must be 1")
	}
	if file.Discord.Token.Source != "env" {
		return errors.New("discord token source must be env")
	}
	if file.MCP.Enabled == nil {
		return errors.New("mcp enabled field is required")
	}
	if _, ok := allowedEnvironmentSecrets[file.Discord.Token.Name]; !ok {
		return errors.New("discord token environment name is not allowlisted")
	}
	switch file.Observability.Exporter {
	case "none", "stderr":
		if file.Observability.Endpoint != "" {
			return errors.New("observability endpoint is allowed only for otlp-http")
		}
	case "otlp-http":
		u, err := url.Parse(file.Observability.Endpoint)
		if err != nil || u.Scheme != "https" || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
			return errors.New("otlp endpoint must be an explicit HTTPS URL without credentials, query, or fragment")
		}
	default:
		return errors.New("unsupported observability exporter")
	}
	return nil
}

func rejectDuplicateKeys(data []byte) error {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	if err := scanValue(decoder); err != nil {
		return fmt.Errorf("inspect configuration structure: %w", err)
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		return errors.New("configuration contains trailing JSON")
	}
	return nil
}

func scanValue(decoder *json.Decoder) error {
	token, err := decoder.Token()
	if err != nil {
		return err
	}
	delim, ok := token.(json.Delim)
	if !ok {
		return nil
	}
	switch delim {
	case '{':
		seen := map[string]struct{}{}
		for decoder.More() {
			keyToken, err := decoder.Token()
			if err != nil {
				return err
			}
			key, ok := keyToken.(string)
			if !ok {
				return errors.New("object key is not a string")
			}
			if _, exists := seen[key]; exists {
				return fmt.Errorf("duplicate object key %q", key)
			}
			seen[key] = struct{}{}
			if err := scanValue(decoder); err != nil {
				return err
			}
		}
		_, err := decoder.Token()
		return err
	case '[':
		for decoder.More() {
			if err := scanValue(decoder); err != nil {
				return err
			}
		}
		_, err := decoder.Token()
		return err
	default:
		return errors.New("unexpected JSON delimiter")
	}
}
