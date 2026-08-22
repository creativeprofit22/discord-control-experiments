package mcp

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/ken-kai/discord-control-experiments/internal/application"
	"github.com/ken-kai/discord-control-experiments/internal/domain"
)

func TestAllContractSchemasAreClosedBoundedAndForbiddenSurfacesAreAbsent(t *testing.T) {
	for _, contract := range application.CommandContracts() {
		input, output, err := GenerateSchemas(contract)
		if err != nil {
			t.Fatalf("%s: %v", contract.Name, err)
		}
		for label, document := range map[string]map[string]any{"input": input.Document, "output": output.Document} {
			encoded, err := json.Marshal(document)
			if err != nil {
				t.Fatal(err)
			}
			text := string(encoded)
			for _, forbidden := range []string{`"guild_id"`, `"audit_reason"`, `"idempotency_key"`, `"reconciliation_key"`, `"uploads"`, `"reader"`, `"url"`, `"method"`, `"path"`, `"script"`} {
				if label == "input" && strings.Contains(text, forbidden) {
					t.Fatalf("%s %s exposes %s", contract.Name, label, forbidden)
				}
			}
			if !strings.Contains(text, `"additionalProperties":false`) {
				t.Fatalf("%s %s is open", contract.Name, label)
			}
		}
	}
}

func TestStrictSchemaRejectsUnknownDuplicateWrongAndExcessiveArguments(t *testing.T) {
	contract, _ := application.LookupCommandContract("create_channel")
	input, _, err := GenerateSchemas(contract)
	if err != nil {
		t.Fatal(err)
	}
	bad := []string{
		`{"name":"general","type":0,"guild_id":"999"}`,
		`{"name":"general","name":"other","type":0}`,
		`{"name":"general","type":"text"}`,
		`{"name":"` + strings.Repeat("x", maxStringLength+1) + `","type":0}`,
		`{"name":"general","type":0,"url":"https://example.invalid"}`,
	}
	for _, raw := range bad {
		if _, err := input.ValidateRaw(json.RawMessage(raw)); err == nil {
			t.Fatalf("accepted %s", raw)
		}
	}
}

func TestValidateRawUnicodeStringLimits(t *testing.T) {
	schema := ToolSchema{root: &schemaNode{
		kind: "object",
		properties: map[string]*schemaNode{
			"text": {kind: "string", minLength: 2, maxLength: 4},
		},
		required: map[string]bool{"text": true},
	}}

	for _, test := range []struct {
		name    string
		value   string
		wantErr bool
	}{
		{name: "below minimum", value: strings.Repeat("界", 1), wantErr: true},
		{name: "at minimum", value: strings.Repeat("界", 2)},
		{name: "below maximum", value: strings.Repeat("界", 3)},
		{name: "at maximum", value: strings.Repeat("界", 4)},
		{name: "above maximum", value: strings.Repeat("界", 5), wantErr: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			raw, err := json.Marshal(map[string]string{"text": test.value})
			if err != nil {
				t.Fatal(err)
			}
			_, err = schema.ValidateRaw(raw)
			if (err != nil) != test.wantErr {
				t.Fatalf("runes=%d err=%v wantErr=%v", utf8.RuneCountInString(test.value), err, test.wantErr)
			}
		})
	}
}

type unicodeStringFixture struct {
	Text string `json:"text"`
}

func TestSanitizeStringUnicodeOutputLimit(t *testing.T) {
	contract := application.CommandContract{
		Name:       "unicode_fixture",
		InputType:  reflect.TypeOf(unicodeStringFixture{}),
		ResultType: reflect.TypeOf(unicodeStringFixture{}),
	}
	_, output, err := GenerateSchemas(contract)
	if err != nil {
		t.Fatal(err)
	}

	for _, test := range []struct {
		name      string
		runeCount int
	}{
		{name: "below limit", runeCount: maxStringLength - 1},
		{name: "at limit", runeCount: maxStringLength},
		{name: "above limit", runeCount: maxStringLength + 1},
	} {
		t.Run(test.name, func(t *testing.T) {
			sanitized := sanitizeString(strings.Repeat("😀", test.runeCount), maxStringLength)
			if !utf8.ValidString(sanitized) {
				t.Fatal("sanitized string is not valid UTF-8")
			}
			wantRunes := test.runeCount
			if wantRunes > maxStringLength {
				wantRunes = maxStringLength
			}
			if got := utf8.RuneCountInString(sanitized); got != wantRunes {
				t.Fatalf("sanitized runes=%d want=%d", got, wantRunes)
			}

			raw, err := json.Marshal(resultEnvelope{
				OK:            true,
				Kind:          "value",
				CorrelationID: "unicode-test",
				Value:         unicodeStringFixture{Text: sanitized},
			})
			if err != nil {
				t.Fatal(err)
			}
			if _, err := output.ValidateRaw(raw); err != nil {
				t.Fatalf("sanitized output failed validation: %v", err)
			}
		})
	}

	invalid := sanitizeString(string([]byte{'a', 0xff, 'b'}), maxStringLength)
	if !utf8.ValidString(invalid) {
		t.Fatal("sanitizer retained invalid UTF-8")
	}
}

func TestDecodeCommandUsesSnakeCaseAndPreservesInjectedContext(t *testing.T) {
	contract, _ := application.LookupCommandContract("create_channel")
	input, _, err := GenerateSchemas(contract)
	if err != nil {
		t.Fatal(err)
	}
	command, err := application.NewContractCommand(contract.Name, "123")
	if err != nil {
		t.Fatal(err)
	}
	if err := input.DecodeCommand(json.RawMessage(`{"name":"general","type":0,"rate_limit_per_user":5}`), command); err != nil {
		t.Fatal(err)
	}
	created := command.(*domain.CreateChannel)
	if created.GuildID != "123" || created.Name != "general" || created.RateLimitPerUser == nil || *created.RateLimitPerUser != 5 {
		t.Fatalf("decoded %#v", created)
	}
}

func FuzzStrictSchemaDeniesUnknownNestedKeys(f *testing.F) {
	f.Add("unknown", "value")
	contract, _ := application.LookupCommandContract("create_channel")
	input, _, err := GenerateSchemas(contract)
	if err != nil {
		f.Fatal(err)
	}
	f.Fuzz(func(t *testing.T, key, value string) {
		if key == "name" || key == "type" || len(key) > 128 || len(value) > 1024 || !json.Valid([]byte(`"`+strings.ReplaceAll(key, `"`, `\"`)+`"`)) {
			return
		}
		payload, _ := json.Marshal(map[string]any{"name": "general", "type": 0, key: value})
		if _, err := input.ValidateRaw(payload); err == nil {
			t.Fatalf("accepted unknown key %q", key)
		}
	})
}
