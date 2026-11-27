package runs

import (
	"context"
	"errors"
	"fmt"

	commonpb "go.temporal.io/api/common/v1"
	"go.temporal.io/api/enums/v1"
	historypb "go.temporal.io/api/history/v1"
	"go.temporal.io/api/serviceerror"
	"go.temporal.io/api/workflowservice/v1"
	"go.temporal.io/sdk/client"
	"go.temporal.io/sdk/converter"
)

const memoInputHash = "platformInputHash"

type temporalClient interface {
	ExecuteWorkflow(context.Context, client.StartWorkflowOptions, any, ...any) (client.WorkflowRun, error)
	ListWorkflow(context.Context, *workflowservice.ListWorkflowExecutionsRequest) (*workflowservice.ListWorkflowExecutionsResponse, error)
	DescribeWorkflowExecution(context.Context, string, string) (*workflowservice.DescribeWorkflowExecutionResponse, error)
	CancelWorkflow(context.Context, string, string) error
	SignalWorkflow(context.Context, string, string, string, any) error
	GetWorkflowHistory(context.Context, string, string, bool, enums.HistoryEventFilterType) client.HistoryEventIterator
}

func (s *TemporalStarter) ListWorkflow(
	ctx context.Context,
	request *workflowservice.ListWorkflowExecutionsRequest,
) (*workflowservice.ListWorkflowExecutionsResponse, error) {
	return s.client.ListWorkflow(ctx, request)
}

func (s *TemporalStarter) DescribeWorkflowExecution(
	ctx context.Context,
	workflowID string,
	runID string,
) (*workflowservice.DescribeWorkflowExecutionResponse, error) {
	return s.client.DescribeWorkflowExecution(ctx, workflowID, runID)
}

func (s *TemporalStarter) CancelWorkflow(ctx context.Context, workflowID string, runID string) error {
	return s.client.CancelWorkflow(ctx, workflowID, runID)
}

func (s *TemporalStarter) SignalWorkflow(
	ctx context.Context,
	workflowID string,
	runID string,
	signalName string,
	value any,
) error {
	return s.client.SignalWorkflow(ctx, workflowID, runID, signalName, value)
}

func (s *TemporalStarter) CloseEvent(
	ctx context.Context,
	ref RunRef,
) (*historypb.HistoryEvent, error) {
	iterator := s.client.GetWorkflowHistory(
		ctx,
		ref.WorkflowID,
		ref.RunID,
		false,
		enums.HISTORY_EVENT_FILTER_TYPE_CLOSE_EVENT,
	)
	if !iterator.HasNext() {
		return nil, nil
	}
	return iterator.Next()
}

func (s *TemporalStarter) HistoryEvents(
	ctx context.Context,
	ref RunRef,
	limit int,
) ([]*historypb.HistoryEvent, bool, error) {
	iterator := s.client.GetWorkflowHistory(
		ctx,
		ref.WorkflowID,
		ref.RunID,
		false,
		enums.HISTORY_EVENT_FILTER_TYPE_ALL_EVENT,
	)
	events := make([]*historypb.HistoryEvent, 0, min(limit, 256))
	truncated := false
	for iterator.HasNext() {
		if len(events) >= limit {
			truncated = true
			break
		}
		event, err := iterator.Next()
		if err != nil {
			return nil, false, err
		}
		events = append(events, event)
	}
	return events, truncated, nil
}

type TemporalStarter struct {
	client temporalClient
}

func NewTemporalStarter(clientInstance temporalClient) *TemporalStarter {
	return &TemporalStarter{client: clientInstance}
}

func (s *TemporalStarter) StartWorkflow(
	ctx context.Context,
	request StartWorkflowRequest,
) (StartWorkflowResult, error) {
	run, err := s.client.ExecuteWorkflow(ctx, client.StartWorkflowOptions{
		ID:                                       request.WorkflowID,
		TaskQueue:                                request.TaskQueue,
		WorkflowIDReusePolicy:                    enums.WORKFLOW_ID_REUSE_POLICY_REJECT_DUPLICATE,
		WorkflowExecutionErrorWhenAlreadyStarted: true,
		Memo: map[string]any{
			memoInputHash: request.InputHash,
		},
	}, request.WorkflowType, request.Input)
	if err == nil {
		return StartWorkflowResult{
			WorkflowID: run.GetID(),
			RunID:      run.GetRunID(),
		}, nil
	}

	var alreadyStarted *serviceerror.WorkflowExecutionAlreadyStarted
	if !errors.As(err, &alreadyStarted) {
		return StartWorkflowResult{}, err
	}

	existing, describeErr := s.client.DescribeWorkflowExecution(ctx, request.WorkflowID, "")
	if describeErr != nil {
		return StartWorkflowResult{}, describeErr
	}
	info := existing.GetWorkflowExecutionInfo()
	if info == nil || info.GetExecution() == nil {
		return StartWorkflowResult{}, fmt.Errorf("describe existing workflow %q: execution info is empty", request.WorkflowID)
	}

	existingHash, hashErr := memoString(info.GetMemo(), memoInputHash)
	if hashErr != nil {
		return StartWorkflowResult{}, hashErr
	}
	return StartWorkflowResult{
		WorkflowID:        info.GetExecution().GetWorkflowId(),
		RunID:             info.GetExecution().GetRunId(),
		Existing:          true,
		ExistingInputHash: existingHash,
	}, nil
}

func memoString(memo *commonpb.Memo, field string) (string, error) {
	if memo == nil {
		return "", nil
	}
	payload := memo.GetFields()[field]
	if payload == nil {
		return "", nil
	}

	var value string
	if err := converter.GetDefaultDataConverter().FromPayload(payload, &value); err != nil {
		return "", fmt.Errorf("decode memo %q: %w", field, err)
	}
	return value, nil
}
