package runs_test

import (
	"context"
	"errors"
	"testing"

	"personal-workflow-platform/platform/catalog"
	"personal-workflow-platform/platform/runs"
	"personal-workflow-platform/platform/sdk"

	"go.temporal.io/sdk/workflow"
)

type greetingInput struct {
	Name string `json:"name"`
}

func greetingWorkflow(ctx workflow.Context, in greetingInput) (string, error) {
	return in.Name, nil
}

type fakeStarter struct {
	requests []runs.StartWorkflowRequest
	result   runs.StartWorkflowResult
	err      error
}

func (s *fakeStarter) StartWorkflow(
	_ context.Context,
	request runs.StartWorkflowRequest,
) (runs.StartWorkflowResult, error) {
	s.requests = append(s.requests, request)
	if s.result.Existing && s.result.ExistingInputHash == "" {
		s.result.ExistingInputHash = request.InputHash
	}
	return s.result, s.err
}

func newService(t *testing.T, starter *fakeStarter, inspectors ...runs.Inspector) *runs.Service {
	t.Helper()

	registry := sdk.NewRegistry()
	sdk.Workflow(registry, sdk.WorkflowDef[greetingInput, string]{
		ID:          "greeting",
		DisplayName: "Greeting",
		Fn:          greetingWorkflow,
		Example:     greetingInput{Name: "Alice"},
	})
	catalogView, err := catalog.New(registry)
	if err != nil {
		t.Fatalf("catalog.New() error = %v", err)
	}

	service, err := runs.New(runs.Options{
		Catalog:       catalogView,
		Registry:      registry,
		Starter:       starter,
		Inspector:     firstInspector(inspectors),
		TaskQueue:     "personal-workflows",
		Namespace:     "default",
		TemporalUIURL: "http://127.0.0.1:8233",
	})
	if err != nil {
		t.Fatalf("runs.New() error = %v", err)
	}
	return service
}

func firstInspector(inspectors []runs.Inspector) runs.Inspector {
	if len(inspectors) == 0 {
		return nil
	}
	return inspectors[0]
}

func TestStartUsesDeterministicWorkflowIDAndInputHash(t *testing.T) {
	starter := &fakeStarter{
		result: runs.StartWorkflowResult{
			WorkflowID: "greeting:idempotent-1",
			RunID:      "run-1",
		},
	}
	service := newService(t, starter)

	run, err := service.Start(context.Background(), runs.StartRequest{
		DefinitionID:   "greeting",
		Input:          greetingInput{Name: "Alice"},
		IdempotencyKey: "idempotent-1",
	})
	if err != nil {
		t.Fatalf("Start() error = %v", err)
	}

	if run.WorkflowID != "greeting:idempotent-1" || run.RunID != "run-1" {
		t.Fatalf("run = %#v, want greeting:idempotent-1/run-1", run)
	}
	if len(starter.requests) != 1 {
		t.Fatalf("starter requests = %d, want 1", len(starter.requests))
	}
	request := starter.requests[0]
	if request.WorkflowID != "greeting:idempotent-1" {
		t.Errorf("WorkflowID = %q, want greeting:idempotent-1", request.WorkflowID)
	}
	if request.InputHash == "" {
		t.Error("InputHash is empty")
	}
	if request.WorkflowType != "greeting" {
		t.Errorf("WorkflowType = %q, want greeting", request.WorkflowType)
	}
	if _, ok := request.Input.(greetingInput); !ok {
		t.Fatalf("Input type = %T, want greetingInput", request.Input)
	}
}

func TestStartReturnsExistingRunForSameInput(t *testing.T) {
	starter := &fakeStarter{
		result: runs.StartWorkflowResult{
			WorkflowID: "greeting:key",
			RunID:      "run-existing",
			Existing:   true,
		},
	}
	service := newService(t, starter)

	input := greetingInput{Name: "Alice"}
	run, err := service.Start(context.Background(), runs.StartRequest{
		DefinitionID:   "greeting",
		Input:          input,
		IdempotencyKey: "key",
	})
	if err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	if !run.Existing || run.RunID != "run-existing" {
		t.Fatalf("run = %#v, want existing run-existing", run)
	}
}

func TestStartRejectsInvalidInputWithoutCallingTemporal(t *testing.T) {
	starter := &fakeStarter{}
	service := newService(t, starter)

	_, err := service.Start(context.Background(), runs.StartRequest{
		DefinitionID:   "greeting",
		Input:          map[string]any{},
		IdempotencyKey: "key",
	})
	if !errors.Is(err, runs.ErrInvalidInput) {
		t.Fatalf("Start() error = %v, want ErrInvalidInput", err)
	}
	if len(starter.requests) != 0 {
		t.Fatalf("starter requests = %d, want 0", len(starter.requests))
	}
}
