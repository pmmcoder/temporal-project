package sdk_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"personal-workflow-platform/platform/sdk"

	"go.temporal.io/sdk/workflow"
)

type greetingInput struct {
	Names []string `json:"names" description:"要问候的名字"`
}

func greetingWorkflow(ctx workflow.Context, in greetingInput) ([]string, error) {
	return in.Names, nil
}

func greetingActivity(ctx context.Context, in greetingInput) ([]string, error) {
	return in.Names, nil
}

func TestRegistryPublishesGeneratedWorkflowSchema(t *testing.T) {
	registry := sdk.NewRegistry()
	sdk.Workflow(registry, sdk.WorkflowDef[greetingInput, []string]{
		ID:          "greeting",
		DisplayName: "问候",
		Description: "生成问候语",
		Fn:          greetingWorkflow,
		Example:     greetingInput{Names: []string{"Alice"}},
	})

	if err := registry.Validate(); err != nil {
		t.Fatalf("Validate() error = %v", err)
	}

	definitions := registry.Workflows()
	if len(definitions) != 1 {
		t.Fatalf("Workflows() length = %d, want 1", len(definitions))
	}

	definition := definitions[0]
	if definition.ID != "greeting" {
		t.Errorf("ID = %q, want greeting", definition.ID)
	}

	var schema map[string]any
	if err := json.Unmarshal(definition.InputSchema, &schema); err != nil {
		t.Fatalf("InputSchema is not JSON: %v", err)
	}
	if schema["type"] != "object" {
		t.Errorf("schema type = %v, want object", schema["type"])
	}
	properties, ok := schema["properties"].(map[string]any)
	if !ok {
		t.Fatalf("schema properties = %#v, want object", schema["properties"])
	}
	if _, ok := properties["names"]; !ok {
		t.Fatalf("schema properties = %#v, want names", properties)
	}
}

func TestRegistryRejectsDuplicateAndReservedNames(t *testing.T) {
	registry := sdk.NewRegistry()
	definition := sdk.WorkflowDef[greetingInput, []string]{
		ID:          "greeting",
		DisplayName: "问候",
		Fn:          greetingWorkflow,
		Example:     greetingInput{Names: []string{"Alice"}},
	}
	sdk.Workflow(registry, definition)
	sdk.Workflow(registry, definition)
	sdk.Workflow(registry, sdk.WorkflowDef[greetingInput, []string]{
		ID:          "platform.reserved",
		DisplayName: "非法名称",
		Fn:          greetingWorkflow,
		Example:     greetingInput{Names: []string{"Alice"}},
	})
	sdk.Activity(registry, sdk.ActivityDef{
		Name: "greeting.Greet",
		Fn:   greetingActivity,
	})
	sdk.Activity(registry, sdk.ActivityDef{
		Name: "greeting.Greet",
		Fn:   greetingActivity,
	})

	err := registry.Validate()
	if err == nil {
		t.Fatal("Validate() error = nil, want validation errors")
	}

	for _, want := range []string{
		`workflow ID "greeting" is already registered`,
		`workflow ID "platform.reserved" uses reserved prefix`,
		`activity "greeting.Greet" is already registered`,
	} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("Validate() error = %q, want substring %q", err, want)
		}
	}
}

func TestRegistryRejectsInvalidExampleAndActivitySignature(t *testing.T) {
	type requiredInput struct {
		Count int `json:"count"`
	}

	registry := sdk.NewRegistry()
	sdk.Workflow(registry, sdk.WorkflowDef[requiredInput, string]{
		ID:          "invalid.example",
		DisplayName: "错误示例",
		Fn: func(ctx workflow.Context, in requiredInput) (string, error) {
			return "", nil
		},
		Example: map[string]any{},
	})
	sdk.Activity(registry, sdk.ActivityDef{
		Name: "invalid.activity",
		Fn:   func(value string) string { return value },
	})

	err := registry.Validate()
	if err == nil {
		t.Fatal("Validate() error = nil, want validation errors")
	}

	for _, want := range []string{
		"example does not match input schema",
		"activity signature must start with context.Context",
	} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("Validate() error = %q, want substring %q", err, want)
		}
	}
}

func TestValidateInputRejectsMissingRequiredField(t *testing.T) {
	type requiredInput struct {
		Name string `json:"name"`
	}

	schema := json.RawMessage(`{
		"type": "object",
		"properties": {"name": {"type": "string"}},
		"required": ["name"]
	}`)

	err := sdk.ValidateInput(schema, map[string]any{})
	if err == nil {
		t.Fatal("ValidateInput() error = nil, want required field error")
	}
}
