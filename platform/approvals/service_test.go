package approvals_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"personal-workflow-platform/platform/approvals"
	"personal-workflow-platform/platform/sdk/approval"

	commonpb "go.temporal.io/api/common/v1"
	"go.temporal.io/api/enums/v1"
	workflowpb "go.temporal.io/api/workflow/v1"
	"go.temporal.io/api/workflowservice/v1"
	"go.temporal.io/sdk/converter"
	"google.golang.org/protobuf/types/known/timestamppb"
)

type fakeClient struct {
	listResponse  *workflowservice.ListWorkflowExecutionsResponse
	describe      *workflowservice.DescribeWorkflowExecutionResponse
	signalName    string
	signalPayload approval.DecisionSignal
	signalErr     error
}

func (c *fakeClient) ListWorkflow(
	_ context.Context,
	_ *workflowservice.ListWorkflowExecutionsRequest,
) (*workflowservice.ListWorkflowExecutionsResponse, error) {
	return c.listResponse, nil
}

func (c *fakeClient) DescribeWorkflowExecution(
	_ context.Context,
	_, _ string,
) (*workflowservice.DescribeWorkflowExecutionResponse, error) {
	return c.describe, nil
}

func (c *fakeClient) SignalWorkflow(
	_ context.Context,
	_, _ string,
	name string,
	value any,
) error {
	c.signalName = name
	c.signalPayload = value.(approval.DecisionSignal)
	return c.signalErr
}

func memo(t *testing.T, items []approval.MemoApproval) *commonpb.Memo {
	t.Helper()
	payload, err := converter.GetDefaultDataConverter().ToPayload(items)
	if err != nil {
		t.Fatalf("encode memo: %v", err)
	}
	return &commonpb.Memo{Fields: map[string]*commonpb.Payload{
		approval.MemoKey: payload,
	}}
}

func TestListReturnsPendingApprovals(t *testing.T) {
	client := &fakeClient{
		listResponse: &workflowservice.ListWorkflowExecutionsResponse{
			Executions: []*workflowpb.WorkflowExecutionInfo{
				{
					Execution: &commonpb.WorkflowExecution{
						WorkflowId: "approvaldemo:key",
						RunId:      "run-1",
					},
					Type:      &commonpb.WorkflowType{Name: "approvaldemo"},
					Status:    enums.WORKFLOW_EXECUTION_STATUS_RUNNING,
					StartTime: timestamppb.Now(),
					Memo: memo(t, []approval.MemoApproval{{
						ID:        "publish",
						Title:     "发布",
						CreatedAt: time.Now(),
						Deadline:  time.Now().Add(time.Hour),
					}}),
				},
			},
		},
	}
	service := approvals.New(client)

	items, err := service.List(context.Background())
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}
	if len(items) != 1 || items[0].ID != "publish" || items[0].WorkflowID != "approvaldemo:key" {
		t.Fatalf("items = %#v", items)
	}
}

func TestDecideRejectsUnknownApprovalWithoutSignal(t *testing.T) {
	client := &fakeClient{
		describe: &workflowservice.DescribeWorkflowExecutionResponse{
			WorkflowExecutionInfo: &workflowpb.WorkflowExecutionInfo{
				Execution: &commonpb.WorkflowExecution{
					WorkflowId: "approvaldemo:key",
					RunId:      "run-1",
				},
				Status: enums.WORKFLOW_EXECUTION_STATUS_RUNNING,
				Memo:   memo(t, nil),
			},
		},
	}
	service := approvals.New(client)

	err := service.Decide(context.Background(), approvals.DecisionRequest{
		WorkflowID: "approvaldemo:key",
		RunID:      "run-1",
		ApprovalID: "missing",
		Decision:   approval.Approved,
	})
	if !errors.Is(err, approvals.ErrNotFound) {
		t.Fatalf("Decide() error = %v, want ErrNotFound", err)
	}
	if client.signalName != "" {
		t.Fatal("SignalWorkflow was called for unknown approval")
	}
}

func TestDecideSignalsKnownApproval(t *testing.T) {
	client := &fakeClient{
		describe: &workflowservice.DescribeWorkflowExecutionResponse{
			WorkflowExecutionInfo: &workflowpb.WorkflowExecutionInfo{
				Execution: &commonpb.WorkflowExecution{
					WorkflowId: "approvaldemo:key",
					RunId:      "run-1",
				},
				Status: enums.WORKFLOW_EXECUTION_STATUS_RUNNING,
				Memo: memo(t, []approval.MemoApproval{{
					ID: "publish",
				}}),
			},
		},
	}
	service := approvals.New(client)

	err := service.Decide(context.Background(), approvals.DecisionRequest{
		WorkflowID: "approvaldemo:key",
		RunID:      "run-1",
		ApprovalID: "publish",
		Decision:   approval.Rejected,
		Comment:    "not ready",
	})
	if err != nil {
		t.Fatalf("Decide() error = %v", err)
	}
	if client.signalName != approval.SignalName {
		t.Errorf("signal name = %q, want %q", client.signalName, approval.SignalName)
	}
	if client.signalPayload.ApprovalID != "publish" ||
		client.signalPayload.Decision != approval.Rejected ||
		client.signalPayload.Operator != "local" ||
		client.signalPayload.Channel != "web" {
		t.Errorf("signal payload = %#v", client.signalPayload)
	}
}

func TestDecidePreservesExplicitOperatorAndChannel(t *testing.T) {
	client := &fakeClient{
		describe: &workflowservice.DescribeWorkflowExecutionResponse{
			WorkflowExecutionInfo: &workflowpb.WorkflowExecutionInfo{
				Execution: &commonpb.WorkflowExecution{
					WorkflowId: "approvaldemo:key",
					RunId:      "run-1",
				},
				Status: enums.WORKFLOW_EXECUTION_STATUS_RUNNING,
				Memo: memo(t, []approval.MemoApproval{{
					ID: "publish",
				}}),
			},
		},
	}
	service := approvals.New(client)

	err := service.Decide(context.Background(), approvals.DecisionRequest{
		WorkflowID: "approvaldemo:key",
		RunID:      "run-1",
		ApprovalID: "publish",
		Decision:   approval.Approved,
		Operator:   "ou_1",
		Channel:    "feishu",
	})
	if err != nil {
		t.Fatalf("Decide() error = %v", err)
	}
	if client.signalPayload.Operator != "ou_1" || client.signalPayload.Channel != "feishu" {
		t.Errorf("signal payload = %#v", client.signalPayload)
	}
}
