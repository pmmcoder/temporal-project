package runtime_test

import (
	"context"
	"testing"

	"personal-workflow-platform/platform/runtime"
	"personal-workflow-platform/platform/sdk"

	"go.temporal.io/sdk/activity"
	"go.temporal.io/sdk/workflow"
)

type greetInput struct {
	Name string `json:"name"`
}

func greetWorkflow(ctx workflow.Context, in greetInput) (string, error) {
	return in.Name, nil
}

func greetActivity(ctx context.Context, in greetInput) (string, error) {
	return in.Name, nil
}

type fakeWorker struct {
	workflows  map[string]any
	activities map[string]any
}

func (w *fakeWorker) RegisterWorkflowWithOptions(value any, options workflow.RegisterOptions) {
	w.workflows[options.Name] = value
}

func (w *fakeWorker) RegisterActivityWithOptions(value any, options activity.RegisterOptions) {
	w.activities[options.Name] = value
}

func TestRegisterUsesExplicitNamesAndIncludesInternalWorkflows(t *testing.T) {
	registry := sdk.NewRegistry()
	sdk.Workflow(registry, sdk.WorkflowDef[greetInput, string]{
		ID:          "greeting",
		DisplayName: "Greeting",
		Fn:          greetWorkflow,
		Example:     greetInput{Name: "parent"},
	})
	sdk.Workflow(registry, sdk.WorkflowDef[greetInput, string]{
		ID:          "greeting.one",
		DisplayName: "Greeting One",
		Fn:          greetWorkflow,
		Example:     greetInput{Name: "child"},
		Internal:    true,
	})
	sdk.Activity(registry, sdk.ActivityDef{Name: "greeting.Greet", Fn: greetActivity})

	worker := &fakeWorker{
		workflows:  make(map[string]any),
		activities: make(map[string]any),
	}
	if err := runtime.Register(worker, registry); err != nil {
		t.Fatalf("Register() error = %v", err)
	}

	for _, name := range []string{"greeting", "greeting.one"} {
		if _, ok := worker.workflows[name]; !ok {
			t.Errorf("workflow %q is not registered", name)
		}
	}
	if _, ok := worker.activities["greeting.Greet"]; !ok {
		t.Error(`activity "greeting.Greet" is not registered`)
	}
	if _, ok := worker.activities["platform.feishu.SendMessage"]; !ok {
		t.Error(`activity "platform.feishu.SendMessage" is not registered`)
	}
}
