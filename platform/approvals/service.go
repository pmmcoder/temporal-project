package approvals

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"time"

	"personal-workflow-platform/platform/sdk/approval"

	commonpb "go.temporal.io/api/common/v1"
	"go.temporal.io/api/enums/v1"
	"go.temporal.io/api/serviceerror"
	"go.temporal.io/api/workflowservice/v1"
	"go.temporal.io/sdk/converter"
)

var (
	ErrNotFound            = errors.New("approval not found")
	ErrInvalidInput        = errors.New("invalid approval decision")
	ErrInvalidState        = errors.New("workflow is not running")
	ErrTemporalUnavailable = errors.New("Temporal is unavailable")
)

type Client interface {
	ListWorkflow(context.Context, *workflowservice.ListWorkflowExecutionsRequest) (*workflowservice.ListWorkflowExecutionsResponse, error)
	DescribeWorkflowExecution(context.Context, string, string) (*workflowservice.DescribeWorkflowExecutionResponse, error)
	SignalWorkflow(context.Context, string, string, string, any) error
}

type Service struct {
	client Client
}

type PendingApproval struct {
	approval.MemoApproval
	WorkflowID   string     `json:"workflowId"`
	RunID        string     `json:"runId"`
	WorkflowType string     `json:"workflowType"`
	StartTime    *time.Time `json:"startTime,omitempty"`
}

type DecisionRequest struct {
	WorkflowID string
	RunID      string
	ApprovalID string
	Decision   approval.Decision
	Comment    string
	Operator   string
	Channel    string
}

func New(client Client) *Service {
	return &Service{client: client}
}

func (s *Service) List(ctx context.Context) ([]PendingApproval, error) {
	response, err := s.client.ListWorkflow(ctx, &workflowservice.ListWorkflowExecutionsRequest{
		PageSize: 100,
		Query:    "PlatformPendingApproval = true AND ExecutionStatus = 'Running'",
	})
	if err != nil {
		return nil, mapTemporalError(err)
	}

	result := make([]PendingApproval, 0)
	for _, execution := range response.GetExecutions() {
		items, err := decodeMemo(execution.GetMemo())
		if err != nil {
			return nil, err
		}
		for _, item := range items {
			pending := PendingApproval{
				MemoApproval: item,
				WorkflowID:   execution.GetExecution().GetWorkflowId(),
				RunID:        execution.GetExecution().GetRunId(),
				WorkflowType: execution.GetType().GetName(),
			}
			if start := execution.GetStartTime(); start != nil {
				value := start.AsTime()
				pending.StartTime = &value
			}
			result = append(result, pending)
		}
	}
	sort.Slice(result, func(i, j int) bool {
		if result[i].Deadline.Equal(result[j].Deadline) {
			return result[i].ID < result[j].ID
		}
		return result[i].Deadline.Before(result[j].Deadline)
	})
	return result, nil
}

func (s *Service) Decide(ctx context.Context, request DecisionRequest) error {
	if request.Decision != approval.Approved && request.Decision != approval.Rejected {
		return fmt.Errorf("%w: decision must be approved or rejected", ErrInvalidInput)
	}
	if request.WorkflowID == "" || request.RunID == "" || request.ApprovalID == "" {
		return fmt.Errorf("%w: workflow, run and approval IDs are required", ErrInvalidInput)
	}

	response, err := s.client.DescribeWorkflowExecution(ctx, request.WorkflowID, request.RunID)
	if err != nil {
		return mapTemporalError(err)
	}
	info := response.GetWorkflowExecutionInfo()
	if info == nil {
		return ErrNotFound
	}
	if info.GetStatus() != enums.WORKFLOW_EXECUTION_STATUS_RUNNING {
		return ErrInvalidState
	}

	items, err := decodeMemo(info.GetMemo())
	if err != nil {
		return err
	}
	found := false
	for _, item := range items {
		if item.ID == request.ApprovalID {
			found = true
			break
		}
	}
	if !found {
		return ErrNotFound
	}

	operator := request.Operator
	if operator == "" {
		operator = "local"
	}
	channel := request.Channel
	if channel == "" {
		channel = "web"
	}
	err = s.client.SignalWorkflow(
		ctx,
		request.WorkflowID,
		request.RunID,
		approval.SignalName,
		approval.DecisionSignal{
			ApprovalID: request.ApprovalID,
			Decision:   request.Decision,
			Comment:    request.Comment,
			Operator:   operator,
			Channel:    channel,
			DecidedAt:  time.Now().UTC(),
		},
	)
	if err != nil {
		return mapTemporalError(err)
	}
	return nil
}

func decodeMemo(memo *commonpb.Memo) ([]approval.MemoApproval, error) {
	if memo == nil {
		return nil, nil
	}
	payload := memo.GetFields()[approval.MemoKey]
	if payload == nil {
		return nil, nil
	}
	var items []approval.MemoApproval
	if err := converter.GetDefaultDataConverter().FromPayload(payload, &items); err != nil {
		return nil, fmt.Errorf("decode approval memo: %w", err)
	}
	return items, nil
}

func mapTemporalError(err error) error {
	var notFound *serviceerror.NotFound
	if errors.As(err, &notFound) {
		return ErrNotFound
	}
	var unavailable *serviceerror.Unavailable
	if errors.As(err, &unavailable) {
		return ErrTemporalUnavailable
	}
	return err
}
