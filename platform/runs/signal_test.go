package runs_test

import (
	"context"
	"errors"
	"testing"

	"personal-workflow-platform/platform/catalog"
	"personal-workflow-platform/platform/runs"
	"personal-workflow-platform/platform/sdk"

	commonpb "go.temporal.io/api/common/v1"
	"go.temporal.io/api/enums/v1"
	workflowpb "go.temporal.io/api/workflow/v1"
	"go.temporal.io/api/workflowservice/v1"
	"go.temporal.io/sdk/workflow"
)

type pauseInput struct {
	Reason string `json:"reason"`
}

func signalWorkflow(ctx workflow.Context) error {
	return nil
}

func TestSignalValidatesDeclarationAndPayload(t *testing.T) {
	registry := sdk.NewRegistry()
	sdk.Workflow(registry, sdk.WorkflowDef[struct{}, string]{
		ID:          "signaltest",
		DisplayName: "Signal Test",
		Fn:          func(ctx workflow.Context, _ struct{}) (string, error) { return "", nil },
		Example:     struct{}{},
		Signals: []sdk.SignalDef{
			sdk.Signal[pauseInput]("pause"),
		},
	})
	catalogView, err := catalog.New(registry)
	if err != nil {
		t.Fatalf("catalog.New() error = %v", err)
	}
	inspector := &fakeInspector{
		describe: &workflowservice.DescribeWorkflowExecutionResponse{
			WorkflowExecutionInfo: &workflowpb.WorkflowExecutionInfo{
				Execution: &commonpb.WorkflowExecution{
					WorkflowId: "signaltest:key",
					RunId:      "run-1",
				},
				Type:   &commonpb.WorkflowType{Name: "signaltest"},
				Status: enums.WORKFLOW_EXECUTION_STATUS_RUNNING,
			},
		},
	}
	service, err := runs.New(runs.Options{
		Catalog:   catalogView,
		Registry:  registry,
		Starter:   &fakeStarter{},
		Inspector: inspector,
		TaskQueue: "personal-workflows",
	})
	if err != nil {
		t.Fatalf("runs.New() error = %v", err)
	}

	err = service.Signal(context.Background(), runs.SignalRequest{
		WorkflowID: "signaltest:key",
		RunID:      "run-1",
		Name:       "pause",
		Input:      pauseInput{Reason: "maintenance"},
	})
	if err != nil {
		t.Fatalf("Signal() error = %v", err)
	}
	if inspector.signalName != "pause" {
		t.Errorf("signal name = %q", inspector.signalName)
	}
	if _, ok := inspector.signalPayload.(pauseInput); !ok {
		t.Errorf("signal payload type = %T", inspector.signalPayload)
	}

	err = service.Signal(context.Background(), runs.SignalRequest{
		WorkflowID: "signaltest:key",
		RunID:      "run-1",
		Name:       "missing",
		Input:      pauseInput{Reason: "x"},
	})
	if !errors.Is(err, runs.ErrSignalNotDefined) {
		t.Fatalf("Signal() error = %v, want ErrSignalNotDefined", err)
	}
}
