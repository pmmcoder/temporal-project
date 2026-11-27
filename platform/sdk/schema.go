package sdk

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"time"
)

func marshalSchemaForType(inputType reflect.Type) (json.RawMessage, error) {
	schema, err := schemaForType(inputType)
	if err != nil {
		return nil, err
	}
	return json.Marshal(schema)
}

func schemaForType(inputType reflect.Type) (map[string]any, error) {
	for inputType.Kind() == reflect.Pointer {
		inputType = inputType.Elem()
	}

	if inputType == reflect.TypeFor[time.Time]() {
		return map[string]any{"type": "string", "format": "date-time"}, nil
	}

	switch inputType.Kind() {
	case reflect.Struct:
		properties := make(map[string]any)
		required := make([]string, 0)
		for i := 0; i < inputType.NumField(); i++ {
			field := inputType.Field(i)
			if field.PkgPath != "" {
				continue
			}
			name, omitEmpty := jsonFieldName(field)
			if name == "-" {
				continue
			}
			property, err := schemaForType(field.Type)
			if err != nil {
				return nil, fmt.Errorf("%s: %w", field.Name, err)
			}
			if description := field.Tag.Get("description"); description != "" {
				property["description"] = description
			}
			properties[name] = property
			if !omitEmpty {
				required = append(required, name)
			}
		}
		schema := map[string]any{
			"type":                 "object",
			"properties":           properties,
			"additionalProperties": false,
		}
		if len(required) > 0 {
			schema["required"] = required
		}
		return schema, nil
	case reflect.Slice, reflect.Array:
		items, err := schemaForType(inputType.Elem())
		if err != nil {
			return nil, err
		}
		return map[string]any{"type": "array", "items": items}, nil
	case reflect.Map:
		values, err := schemaForType(inputType.Elem())
		if err != nil {
			return nil, err
		}
		return map[string]any{"type": "object", "additionalProperties": values}, nil
	case reflect.String:
		return map[string]any{"type": "string"}, nil
	case reflect.Bool:
		return map[string]any{"type": "boolean"}, nil
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return map[string]any{"type": "integer"}, nil
	case reflect.Float32, reflect.Float64:
		return map[string]any{"type": "number"}, nil
	default:
		return nil, fmt.Errorf("unsupported type %s", inputType)
	}
}

func jsonFieldName(field reflect.StructField) (name string, omitEmpty bool) {
	tag := field.Tag.Get("json")
	if tag == "" {
		return field.Name, false
	}
	parts := strings.Split(tag, ",")
	name = parts[0]
	if name == "" {
		name = field.Name
	}
	for _, option := range parts[1:] {
		if option == "omitempty" {
			omitEmpty = true
		}
	}
	return name, omitEmpty
}

func validateJSONAgainstSchema(value json.RawMessage, schema json.RawMessage) error {
	if len(bytes.TrimSpace(value)) == 0 {
		return errors.New("example is empty")
	}

	var decoded any
	decoder := json.NewDecoder(bytes.NewReader(value))
	decoder.UseNumber()
	if err := decoder.Decode(&decoded); err != nil {
		return fmt.Errorf("decode example: %w", err)
	}

	var schemaValue map[string]any
	if err := json.Unmarshal(schema, &schemaValue); err != nil {
		return fmt.Errorf("decode schema: %w", err)
	}
	return validateSchemaValue(decoded, schemaValue, "")
}

func ValidateInput(schema json.RawMessage, input any) error {
	encoded, err := json.Marshal(input)
	if err != nil {
		return fmt.Errorf("encode input: %w", err)
	}
	return validateJSONAgainstSchema(encoded, schema)
}

func validateSchemaValue(value any, schema map[string]any, path string) error {
	if path == "" {
		path = "/"
	}

	expected, _ := schema["type"].(string)
	switch expected {
	case "object":
		object, ok := value.(map[string]any)
		if !ok {
			return fmt.Errorf("%s: must be object", path)
		}
		if required, ok := schema["required"].([]any); ok {
			for _, item := range required {
				name, _ := item.(string)
				if _, exists := object[name]; !exists {
					return fmt.Errorf("%s%s: required field missing", path, name)
				}
			}
		}
		properties, _ := schema["properties"].(map[string]any)
		for name, rawProperty := range properties {
			property, ok := rawProperty.(map[string]any)
			if !ok {
				continue
			}
			if child, exists := object[name]; exists {
				if err := validateSchemaValue(child, property, path+name+"/"); err != nil {
					return err
				}
			}
		}
	case "array":
		array, ok := value.([]any)
		if !ok {
			return fmt.Errorf("%s: must be array", path)
		}
		items, _ := schema["items"].(map[string]any)
		for i, child := range array {
			if err := validateSchemaValue(child, items, fmt.Sprintf("%s%d/", path, i)); err != nil {
				return err
			}
		}
	case "string":
		if _, ok := value.(string); !ok {
			return fmt.Errorf("%s: must be string", path)
		}
	case "integer":
		number, ok := value.(json.Number)
		if !ok {
			return fmt.Errorf("%s: must be integer", path)
		}
		if strings.Contains(number.String(), ".") {
			return fmt.Errorf("%s: must be integer", path)
		}
	case "number":
		if _, ok := value.(json.Number); !ok {
			return fmt.Errorf("%s: must be number", path)
		}
	case "boolean":
		if _, ok := value.(bool); !ok {
			return fmt.Errorf("%s: must be boolean", path)
		}
	}
	return nil
}
