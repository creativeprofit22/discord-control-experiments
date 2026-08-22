package mcp

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"reflect"
	"regexp"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/ken-kai/discord-control-experiments/internal/application"
)

const (
	maxSchemaDepth  = 12
	maxObjectFields = 128
	maxArrayItems   = 100
	maxStringLength = 4096
)

var snowflakePattern = regexp.MustCompile(`^[1-9][0-9]{0,19}$`)

type schemaNode struct {
	kind                 string
	properties           map[string]*schemaNode
	required             map[string]bool
	items                *schemaNode
	oneOf                []*schemaNode
	minimum, maximum     float64
	minLength, maxLength int
	maxItems             int
	pattern              *regexp.Regexp
	goName               string
}

type ToolSchema struct {
	Document map[string]any
	root     *schemaNode
}

func GenerateSchemas(contract application.CommandContract) (ToolSchema, ToolSchema, error) {
	inputRoot, err := schemaForType(contract.InputType, true, 0, map[reflect.Type]int{})
	if err != nil {
		return ToolSchema{}, ToolSchema{}, fmt.Errorf("input schema for %s: %w", contract.Name, err)
	}
	valueRoot, err := schemaForType(contract.ResultType, false, 0, map[reflect.Type]int{})
	if err != nil {
		return ToolSchema{}, ToolSchema{}, fmt.Errorf("output schema for %s: %w", contract.Name, err)
	}
	outputRoot := envelopeNode(valueRoot)
	return ToolSchema{Document: nodeDocument(inputRoot), root: inputRoot}, ToolSchema{Document: nodeDocument(outputRoot), root: outputRoot}, nil
}

func schemaForType(input reflect.Type, commandInput bool, depth int, visiting map[reflect.Type]int) (*schemaNode, error) {
	if depth > maxSchemaDepth {
		return nil, errors.New("schema nesting exceeds limit")
	}
	for input.Kind() == reflect.Pointer {
		input = input.Elem()
	}
	if input == reflect.TypeOf(time.Time{}) {
		return &schemaNode{kind: "string", minLength: 20, maxLength: 40}, nil
	}
	if input.Kind() == reflect.Interface {
		if input.NumMethod() == 0 {
			return nil, errors.New("arbitrary any field is forbidden")
		}
		return nil, errors.New("interface field is forbidden")
	}
	switch input.Kind() {
	case reflect.Bool:
		return &schemaNode{kind: "boolean"}, nil
	case reflect.String:
		n := &schemaNode{kind: "string", minLength: 0, maxLength: stringLimit(input)}
		if isSnowflakeType(input) {
			n.minLength, n.maxLength, n.pattern = 1, 20, snowflakePattern
		}
		return n, nil
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64, reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return &schemaNode{kind: "integer", minimum: -1_000_000_000, maximum: 1_000_000_000}, nil
	case reflect.Float32, reflect.Float64:
		return &schemaNode{kind: "number", minimum: -1_000_000_000, maximum: 1_000_000_000}, nil
	case reflect.Slice, reflect.Array:
		item, err := schemaForType(input.Elem(), commandInput, depth+1, visiting)
		if err != nil {
			return nil, err
		}
		return &schemaNode{kind: "array", items: item, maxItems: maxArrayItems}, nil
	case reflect.Struct:
		if visiting[input] >= 3 {
			return nil, errors.New("recursive schema exceeds bounded expansion")
		}
		visiting[input]++
		defer func() { visiting[input]-- }()
		n := &schemaNode{kind: "object", properties: map[string]*schemaNode{}, required: map[string]bool{}}
		for index := 0; index < input.NumField(); index++ {
			field := input.Field(index)
			if field.PkgPath != "" {
				continue
			}
			if commandInput && forbiddenInputField(field) {
				continue
			}
			if field.Anonymous && field.Name == "CommandContext" {
				continue
			}
			if field.Type.Kind() == reflect.Slice && field.Type.Elem() == input && visiting[input] >= 3 {
				continue
			}
			jsonName, omit := fieldJSONName(field)
			if omit {
				continue
			}
			var child *schemaNode
			var err error
			if input.Name() == "ApplicationCommandChoice" && field.Name == "Value" {
				child = &schemaNode{oneOf: []*schemaNode{{kind: "string", maxLength: maxStringLength}, {kind: "number", minimum: -1_000_000_000, maximum: 1_000_000_000}, {kind: "boolean"}}}
			} else {
				child, err = schemaForType(field.Type, commandInput, depth+1, visiting)
			}
			if err != nil {
				return nil, fmt.Errorf("field %s: %w", field.Name, err)
			}
			child.goName = fieldJSONDecodeName(field)
			n.properties[jsonName] = child
			if field.Type.Kind() != reflect.Pointer && field.Type.Kind() != reflect.Slice && field.Type.Kind() != reflect.Map && !strings.Contains(field.Tag.Get("json"), "omitempty") {
				n.required[jsonName] = true
			}
			if len(n.properties) > maxObjectFields {
				return nil, errors.New("object property count exceeds limit")
			}
		}
		return n, nil
	default:
		return nil, fmt.Errorf("unsupported field kind %s", input.Kind())
	}
}

func forbiddenInputField(field reflect.StructField) bool {
	name := strings.ToLower(field.Name)
	if field.Name == "CommandContext" || field.Name == "GuildID" || name == "uploads" || name == "upload" || strings.Contains(name, "reader") {
		return true
	}
	for _, forbidden := range []string{"url", "method", "path", "script", "authorization", "token"} {
		if name == forbidden || strings.HasSuffix(name, forbidden) {
			return true
		}
	}
	return false
}

func fieldJSONName(field reflect.StructField) (string, bool) {
	tag := field.Tag.Get("json")
	if tag != "" {
		name := strings.Split(tag, ",")[0]
		if name == "-" {
			return "", true
		}
		if name != "" {
			return name, false
		}
	}
	return snakeCase(field.Name), false
}

func fieldJSONDecodeName(field reflect.StructField) string {
	tag := field.Tag.Get("json")
	if tag != "" {
		name := strings.Split(tag, ",")[0]
		if name != "" && name != "-" {
			return name
		}
	}
	return field.Name
}

func snakeCase(value string) string {
	runes := []rune(value)
	var out strings.Builder
	for index, current := range runes {
		if unicode.IsUpper(current) && index > 0 {
			previousLower := unicode.IsLower(runes[index-1]) || unicode.IsDigit(runes[index-1])
			nextLower := index+1 < len(runes) && unicode.IsLower(runes[index+1])
			if previousLower || nextLower {
				out.WriteByte('_')
			}
		}
		out.WriteRune(unicode.ToLower(current))
	}
	return out.String()
}

func isSnowflakeType(value reflect.Type) bool {
	name := value.Name()
	return strings.HasSuffix(name, "ID") || name == "Snowflake"
}

func stringLimit(value reflect.Type) int {
	switch value.Name() {
	case "InviteCode":
		return 64
	case "PermissionBits":
		return 32
	default:
		return maxStringLength
	}
}

func nodeDocument(n *schemaNode) map[string]any {
	if len(n.oneOf) > 0 {
		values := make([]any, len(n.oneOf))
		for i, child := range n.oneOf {
			values[i] = nodeDocument(child)
		}
		return map[string]any{"oneOf": values}
	}
	doc := map[string]any{"type": n.kind}
	switch n.kind {
	case "object":
		props := map[string]any{}
		required := []string{}
		for name, child := range n.properties {
			props[name] = nodeDocument(child)
			if n.required[name] {
				required = append(required, name)
			}
		}
		sortStrings(required)
		doc["properties"], doc["additionalProperties"], doc["maxProperties"] = props, false, maxObjectFields
		if len(required) > 0 {
			doc["required"] = required
		}
	case "array":
		doc["items"], doc["maxItems"] = nodeDocument(n.items), n.maxItems
	case "string":
		doc["minLength"], doc["maxLength"] = n.minLength, n.maxLength
		if n.pattern != nil {
			doc["pattern"] = n.pattern.String()
		}
	case "integer", "number":
		doc["minimum"], doc["maximum"] = n.minimum, n.maximum
	}
	return doc
}

type safeIntent struct {
	Key   string `json:"key"`
	Value string `json:"value"`
}
type safeOperationPlan struct {
	Operation        string       `json:"operation"`
	Capability       string       `json:"capability"`
	Mode             string       `json:"mode"`
	DestructiveClass string       `json:"destructive_class"`
	ResourceKinds    []string     `json:"resource_kinds"`
	Intent           []safeIntent `json:"intent"`
	OperationHash    string       `json:"operation_hash"`
	Preapproved      bool         `json:"preapproved"`
}

func envelopeNode(value *schemaNode) *schemaNode {
	base := func() map[string]*schemaNode {
		return map[string]*schemaNode{"ok": {kind: "boolean"}, "kind": {kind: "string", minLength: 1, maxLength: 64}, "correlation_id": {kind: "string", minLength: 1, maxLength: 128}}
	}
	variants := []*schemaNode{}
	for key, child := range map[string]*schemaNode{"value": value, "plan": mustSchema(reflect.TypeOf(safeOperationPlan{})), "error": errorNode()} {
		props := base()
		props[key] = child
		variants = append(variants, &schemaNode{kind: "object", properties: props, required: map[string]bool{"ok": true, "kind": true, "correlation_id": true, key: true}})
	}
	return &schemaNode{oneOf: variants}
}

func mustSchema(value reflect.Type) *schemaNode {
	n, err := schemaForType(value, false, 0, map[reflect.Type]int{})
	if err != nil {
		panic(err)
	}
	return n
}
func errorNode() *schemaNode {
	return &schemaNode{kind: "object", properties: map[string]*schemaNode{"code": {kind: "string", minLength: 1, maxLength: 64}, "detail": {kind: "string", minLength: 1, maxLength: 512}}, required: map[string]bool{"code": true, "detail": true}}
}

func (schema ToolSchema) ValidateRaw(raw json.RawMessage) (map[string]any, error) {
	if len(raw) == 0 || !utf8.Valid(raw) {
		return nil, errors.New("arguments are missing or invalid UTF-8")
	}
	if err := rejectDuplicateJSONKeys(raw); err != nil {
		return nil, err
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	var value any
	if err := decoder.Decode(&value); err != nil {
		return nil, errors.New("arguments are malformed JSON")
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		return nil, errors.New("arguments contain trailing JSON")
	}
	if err := validateNode(schema.root, value, 0); err != nil {
		return nil, err
	}
	object, ok := value.(map[string]any)
	if !ok {
		return nil, errors.New("arguments must be an object")
	}
	return object, nil
}

func validateNode(node *schemaNode, value any, depth int) error {
	if depth > maxSchemaDepth {
		return errors.New("arguments exceed nesting limit")
	}
	if len(node.oneOf) > 0 {
		matches := 0
		for _, candidate := range node.oneOf {
			if validateNode(candidate, value, depth+1) == nil {
				matches++
			}
		}
		if matches != 1 {
			return errors.New("argument does not match exactly one allowed shape")
		}
		return nil
	}
	switch node.kind {
	case "object":
		object, ok := value.(map[string]any)
		if !ok {
			return errors.New("argument must be an object")
		}
		if len(object) > maxObjectFields {
			return errors.New("object contains too many fields")
		}
		for required := range node.required {
			if _, ok := object[required]; !ok {
				return fmt.Errorf("required field %s is missing", required)
			}
		}
		for name, childValue := range object {
			child, ok := node.properties[name]
			if !ok {
				return fmt.Errorf("unknown field %s", name)
			}
			if err := validateNode(child, childValue, depth+1); err != nil {
				return fmt.Errorf("field %s: %w", name, err)
			}
		}
	case "array":
		array, ok := value.([]any)
		if !ok {
			return errors.New("argument must be an array")
		}
		if len(array) > node.maxItems {
			return errors.New("array exceeds item limit")
		}
		for _, item := range array {
			if err := validateNode(node.items, item, depth+1); err != nil {
				return err
			}
		}
	case "string":
		text, ok := value.(string)
		if !ok || !utf8.ValidString(text) {
			return errors.New("string is invalid or outside limits")
		}
		length := utf8.RuneCountInString(text)
		if length < node.minLength || length > node.maxLength {
			return errors.New("string is invalid or outside limits")
		}
		if node.pattern != nil && !node.pattern.MatchString(text) {
			return errors.New("string format is invalid")
		}
	case "boolean":
		if _, ok := value.(bool); !ok {
			return errors.New("argument must be boolean")
		}
	case "integer":
		number, ok := value.(json.Number)
		if !ok {
			return errors.New("argument must be integer")
		}
		integer, err := number.Int64()
		if err != nil || float64(integer) < node.minimum || float64(integer) > node.maximum {
			return errors.New("integer is outside limits")
		}
	case "number":
		number, ok := value.(json.Number)
		if !ok {
			return errors.New("argument must be numeric")
		}
		parsed, err := number.Float64()
		if err != nil || parsed < node.minimum || parsed > node.maximum {
			return errors.New("number is outside limits")
		}
	default:
		return errors.New("invalid schema node")
	}
	return nil
}

func (schema ToolSchema) DecodeCommand(raw json.RawMessage, destination any) error {
	object, err := schema.ValidateRaw(raw)
	if err != nil {
		return err
	}
	normalized := normalizeKeys(schema.root, object)
	payload, err := json.Marshal(normalized)
	if err != nil {
		return errors.New("arguments could not be normalized")
	}
	decoder := json.NewDecoder(bytes.NewReader(payload))
	decoder.DisallowUnknownFields()
	decoder.UseNumber()
	if err := decoder.Decode(destination); err != nil {
		return errors.New("arguments could not be decoded")
	}
	return nil
}

func normalizeKeys(node *schemaNode, value any) any {
	if len(node.oneOf) > 0 {
		for _, candidate := range node.oneOf {
			if validateNode(candidate, value, 0) == nil {
				return normalizeKeys(candidate, value)
			}
		}
		return value
	}
	switch node.kind {
	case "object":
		object := value.(map[string]any)
		out := map[string]any{}
		for name, childValue := range object {
			child := node.properties[name]
			out[child.goName] = normalizeKeys(child, childValue)
		}
		return out
	case "array":
		array := value.([]any)
		out := make([]any, len(array))
		for i, item := range array {
			out[i] = normalizeKeys(node.items, item)
		}
		return out
	default:
		return value
	}
}

func rejectDuplicateJSONKeys(raw []byte) error {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	stack := []map[string]bool{}
	expectingKey := []bool{}
	for {
		token, err := decoder.Token()
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return errors.New("arguments are malformed JSON")
		}
		switch typed := token.(type) {
		case json.Delim:
			switch typed {
			case '{':
				stack = append(stack, map[string]bool{})
				expectingKey = append(expectingKey, true)
			case '[':
				stack = append(stack, nil)
				expectingKey = append(expectingKey, false)
			case '}', ']':
				if len(stack) == 0 {
					return errors.New("arguments have invalid structure")
				}
				stack = stack[:len(stack)-1]
				expectingKey = expectingKey[:len(expectingKey)-1]
				if len(stack) > 0 && stack[len(stack)-1] != nil {
					expectingKey[len(expectingKey)-1] = true
				}
			}
		case string:
			if len(stack) > 0 && stack[len(stack)-1] != nil && expectingKey[len(expectingKey)-1] {
				if stack[len(stack)-1][typed] {
					return errors.New("arguments contain duplicate object key")
				}
				stack[len(stack)-1][typed] = true
				expectingKey[len(expectingKey)-1] = false
			} else if len(stack) > 0 && stack[len(stack)-1] != nil {
				expectingKey[len(expectingKey)-1] = true
			}
		default:
			if len(stack) > 0 && stack[len(stack)-1] != nil {
				expectingKey[len(expectingKey)-1] = true
			}
		}
	}
}

func sortStrings(values []string) {
	for i := 1; i < len(values); i++ {
		for j := i; j > 0 && values[j] < values[j-1]; j-- {
			values[j], values[j-1] = values[j-1], values[j]
		}
	}
}
