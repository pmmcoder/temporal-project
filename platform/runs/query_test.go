package runs_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"personal-workflow-platform/platform/runs"

	commonpb "go.temporal.io/api/common/v1"
	"go.temporal.io/api/enums/v1"
	failurepb "go.temporal.io/api/failure/v1"
	historypb "go.temporal.io/api/history/v1"
	workflowpb "go.temporal.io/api/workflow/v1"
	"go.temporal.io/api/workflowservice/v1"
	"go.temporal.io/sdk/converter"
	"google.golang.org/protobuf/types/known/durationpb"
	"google.golang.org/protobuf/types/known/timestamppb"
)

type fakeInspector struct {
	listRequest      *workflowservice.ListWorkflowExecutionsRequest
	listResult       *workflowservice.ListWorkflowExecutionsResponse
	describe         *workflowservice.DescribeWorkflowExecutionResponse
	cancelledID      string
	cancelledRun     string
	closeEvent       *historypb.HistoryEvent
	closeEventCalled bool
	signalName       string
	signalPayload    any
	historyEvents    []*historypb.HistoryEvent
	err              error
}

func (i *fakeInspector) ListWorkflow(
	_ context.Context,
	request *workflowservice.ListWorkflowExecutionsRequest,
) (*workflowservice.ListWorkflowExecutionsResponse, error) {
	i.listRequest = request
	return i.listResult, i.err
}

func (i *fakeInspector) DescribeWorkflowExecution(
	_ context.Context,
	_, _ string,
) (*workflowservice.DescribeWorkflowExecutionResponse, error) {
	return i.describe, i.err
}

func (i *fakeInspector) CancelWorkflow(_ context.Context, workflowID, runID string) error {
	i.cancelledID = workflowID
	i.cancelledRun = runID
	return i.err
}

func (i *fakeInspector) CloseEvent(_ context.Context, _ runs.RunRef) (*historypb.HistoryEvent, error) {
	i.closeEventCalled = true
	return i.closeEvent, i.err
}

func (i *fakeInspector) SignalWorkflow(
	_ context.Context,
	_ string,
	_ string,
	name string,
	payload any,
) error {
	i.signalName = name
	i.signalPayload = payload
	return i.err
}

func (i *fakeInspector) HistoryEvents(
	_ context.Context,
	_ runs.RunRef,
	_ int,
) ([]*historypb.HistoryEvent, bool, error) {
	return i.historyEvents, false, i.err
}

func TestListMapsVisibilityResultsAndFilters(t *testing.T) {
	start := time.Date(2026, 11, 20, 9, 0, 0, 0, time.UTC)
	inspector := &fakeInspector{
		listResult: &workflowservice.ListWorkflowExecutionsResponse{
			Executions: []*workflowpb.WorkflowExecutionInfo{
				{
					Execution: &commonpb.WorkflowExecution{
						WorkflowId: "greeting:key",
						RunId:      "run-1",
					},
					Type:      &commonpb.WorkflowType{Name: "greeting"},
					Status:    enums.WORKFLOW_EXECUTION_STATUS_RUNNING,
					StartTime: timestamppb.New(start),
				},
			},
			NextPageToken: []byte("next"),
		},
	}
	service := newService(t, &fakeStarter{}, inspector)

	page, err := service.List(context.Background(), runs.ListRequest{
		Workflow: "greeting",
		Status:   runs.StatusRunning,
		PageSize: 25,
		From:     start.Add(-time.Hour),
		To:       start.Add(time.Hour),
	})
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}
	if len(page.Runs) != 1 {
		t.Fatalf("runs length = %d, want 1", len(page.Runs))
	}
	if page.Runs[0].Status != runs.StatusRunning {
		t.Errorf("status = %q, want running", page.Runs[0].Status)
	}
	if page.Runs[0].StartTime == nil || !page.Runs[0].StartTime.Equal(start) {
		t.Errorf("start time = %v, want %v", page.Runs[0].StartTime, start)
	}
	if page.NextPageToken == "" {
		t.Error("NextPageToken is empty")
	}
	if inspector.listRequest == nil {
		t.Fatal("ListWorkflow request is nil")
	}
	if inspector.listRequest.PageSize != 25 {
		t.Errorf("PageSize = %d, want 25", inspector.listRequest.PageSize)
	}
	for _, fragment := range []string{
		`WorkflowType = 'greeting'`,
		`ExecutionStatus = 'Running'`,
		`StartTime >= '2026-11-20T08:00:00Z'`,
		`StartTime <= '2026-11-20T10:00:00Z'`,
	} {
		if !strings.Contains(inspector.listRequest.Query, fragment) {
			t.Errorf("Query = %q, want fragment %q", inspector.listRequest.Query, fragment)
		}
	}
}

func TestCancelRejectsClosedRun(t *testing.T) {
	inspector := &fakeInspector{
		describe: &workflowservice.DescribeWorkflowExecutionResponse{
			WorkflowExecutionInfo: &workflowpb.WorkflowExecutionInfo{
				Execution: &commonpb.WorkflowExecution{WorkflowId: "greeting:key", RunId: "run-1"},
				Status:    enums.WORKFLOW_EXECUTION_STATUS_COMPLETED,
			},
		},
	}
	service := newService(t, &fakeStarter{}, inspector)

	err := service.Cancel(context.Background(), runs.RunRef{
		WorkflowID: "greeting:key",
		RunID:      "run-1",
	})
	if !errors.Is(err, runs.ErrInvalidState) {
		t.Fatalf("Cancel() error = %v, want ErrInvalidState", err)
	}
	if inspector.cancelledID != "" {
		t.Fatalf("CancelWorkflow called for closed run: %s/%s", inspector.cancelledID, inspector.cancelledRun)
	}
}

func TestGetIncludesTemporalUILink(t *testing.T) {
	inspector := &fakeInspector{
		describe: &workflowservice.DescribeWorkflowExecutionResponse{
			WorkflowExecutionInfo: &workflowpb.WorkflowExecutionInfo{
				Execution: &commonpb.WorkflowExecution{
					WorkflowId: "greeting:key",
					RunId:      "run-1",
				},
				Type:   &commonpb.WorkflowType{Name: "greeting"},
				Status: enums.WORKFLOW_EXECUTION_STATUS_RUNNING,
			},
		},
	}
	service := newService(t, &fakeStarter{}, inspector)

	detail, err := service.Get(context.Background(), runs.RunRef{
		WorkflowID: "greeting:key",
		RunID:      "run-1",
	})
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}

	const want = "http://127.0.0.1:8233/namespaces/default/workflows/greeting:key/run-1"
	if detail.TemporalUIURL != want {
		t.Errorf("TemporalUIURL = %q, want %q", detail.TemporalUIURL, want)
	}
}

func TestGetIncludesCompletedWorkflowOutput(t *testing.T) {
	payloads, err := converter.GetDefaultDataConverter().ToPayloads([]string{"Hello Ada"})
	if err != nil {
		t.Fatalf("encode result: %v", err)
	}
	inspector := &fakeInspector{
		describe: &workflowservice.DescribeWorkflowExecutionResponse{
			WorkflowExecutionInfo: &workflowpb.WorkflowExecutionInfo{
				Execution: &commonpb.WorkflowExecution{
					WorkflowId: "greeting:key",
					RunId:      "run-1",
				},
				Type:   &commonpb.WorkflowType{Name: "greeting"},
				Status: enums.WORKFLOW_EXECUTION_STATUS_COMPLETED,
			},
		},
		closeEvent: &historypb.HistoryEvent{
			EventType: enums.EVENT_TYPE_WORKFLOW_EXECUTION_COMPLETED,
			Attributes: &historypb.HistoryEvent_WorkflowExecutionCompletedEventAttributes{
				WorkflowExecutionCompletedEventAttributes: &historypb.WorkflowExecutionCompletedEventAttributes{
					Result: payloads,
				},
			},
		},
	}
	service := newService(t, &fakeStarter{}, inspector)

	detail, err := service.Get(context.Background(), runs.RunRef{
		WorkflowID: "greeting:key",
		RunID:      "run-1",
	})
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if detail.Output == nil {
		t.Fatal("Output is nil")
	}
}

func TestGetRunningWorkflowDoesNotReadCloseEvent(t *testing.T) {
	inspector := &fakeInspector{
		describe: &workflowservice.DescribeWorkflowExecutionResponse{
			WorkflowExecutionInfo: &workflowpb.WorkflowExecutionInfo{
				Execution: &commonpb.WorkflowExecution{
					WorkflowId: "approvaldemo:key",
					RunId:      "run-1",
				},
				Type:   &commonpb.WorkflowType{Name: "approvaldemo"},
				Status: enums.WORKFLOW_EXECUTION_STATUS_RUNNING,
			},
		},
	}
	service := newService(t, &fakeStarter{}, inspector)

	if _, err := service.Get(context.Background(), runs.RunRef{
		WorkflowID: "approvaldemo:key",
		RunID:      "run-1",
	}); err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if inspector.closeEventCalled {
		t.Fatal("CloseEvent was called for a running workflow")
	}
}

func TestGetIncludesPendingRetryAndTimelineSummary(t *testing.T) {
	now := time.Date(2026, 11, 20, 10, 0, 0, 0, time.UTC)
	nextRetry := now.Add(2 * time.Minute)
	inspector := &fakeInspector{
		describe: &workflowservice.DescribeWorkflowExecutionResponse{
			WorkflowExecutionInfo: &workflowpb.WorkflowExecutionInfo{
				Execution: &commonpb.WorkflowExecution{
					WorkflowId: "greeting:key",
					RunId:      "run-1",
				},
				Type:   &commonpb.WorkflowType{Name: "greeting"},
				Status: enums.WORKFLOW_EXECUTION_STATUS_RUNNING,
			},
			PendingActivities: []*workflowpb.PendingActivityInfo{{
				ActivityId:              "5",
				ActivityType:            &commonpb.ActivityType{Name: "greeting.Greet"},
				Attempt:                 3,
				MaximumAttempts:         5,
				LastFailure:             &failurepb.Failure{Message: "network error"},
				CurrentRetryInterval:    durationpb.New(2 * time.Minute),
				NextAttemptScheduleTime: timestamppb.New(nextRetry),
			}},
		},
		historyEvents: []*historypb.HistoryEvent{
			{
				EventId:   1,
				EventTime: timestamppb.New(now),
				EventType: enums.EVENT_TYPE_ACTIVITY_TASK_SCHEDULED,
				Attributes: &historypb.HistoryEvent_ActivityTaskScheduledEventAttributes{
					ActivityTaskScheduledEventAttributes: &historypb.ActivityTaskScheduledEventAttributes{
						ActivityId:   "5",
						ActivityType: &commonpb.ActivityType{Name: "greeting.Greet"},
					},
				},
			},
			{
				EventId:   2,
				EventTime: timestamppb.New(now.Add(time.Second)),
				EventType: enums.EVENT_TYPE_WORKFLOW_EXECUTION_SIGNALED,
				Attributes: &historypb.HistoryEvent_WorkflowExecutionSignaledEventAttributes{
					WorkflowExecutionSignaledEventAttributes: &historypb.WorkflowExecutionSignaledEventAttributes{
						SignalName: "pause",
					},
				},
			},
		},
	}
	service := newService(t, &fakeStarter{}, inspector)

	detail, err := service.Get(context.Background(), runs.RunRef{
		WorkflowID: "greeting:key",
		RunID:      "run-1",
	})
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if len(detail.PendingActivities) != 1 ||
		detail.PendingActivities[0].LastError != "network error" ||
		detail.PendingActivities[0].MaximumAttempts != 5 {
		t.Fatalf("pending activities = %#v", detail.PendingActivities)
	}
	if len(detail.Timeline) != 2 {
		t.Fatalf("timeline = %#v", detail.Timeline)
	}
	if detail.Timeline[0].Kind != "activity" || detail.Timeline[1].Kind != "signal" {
		t.Errorf("timeline = %#v", detail.Timeline)
	}
}
