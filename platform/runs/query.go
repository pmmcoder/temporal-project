package runs

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"time"

	"go.temporal.io/api/enums/v1"
	historypb "go.temporal.io/api/history/v1"
	"go.temporal.io/api/serviceerror"
	workflowpb "go.temporal.io/api/workflow/v1"
	"go.temporal.io/api/workflowservice/v1"
	"go.temporal.io/sdk/converter"

	"personal-workflow-platform/platform/sdk"
)

type Status string

const (
	StatusRunning    Status = "running"
	StatusSucceeded  Status = "succeeded"
	StatusFailed     Status = "failed"
	StatusCanceled   Status = "canceled"
	StatusTerminated Status = "terminated"
	StatusTimedOut   Status = "timed_out"
	StatusContinued  Status = "continued"
	StatusUnknown    Status = "unknown"
)

type Inspector interface {
	ListWorkflow(context.Context, *workflowservice.ListWorkflowExecutionsRequest) (*workflowservice.ListWorkflowExecutionsResponse, error)
	DescribeWorkflowExecution(context.Context, string, string) (*workflowservice.DescribeWorkflowExecutionResponse, error)
	CancelWorkflow(context.Context, string, string) error
	CloseEvent(context.Context, RunRef) (*historypb.HistoryEvent, error)
	SignalWorkflow(context.Context, string, string, string, any) error
	HistoryEvents(context.Context, RunRef, int) ([]*historypb.HistoryEvent, bool, error)
}

type ListRequest struct {
	Workflow  string
	Status    Status
	PageSize  int
	PageToken string
	From      time.Time
	To        time.Time
}

type RunRef struct {
	WorkflowID string
	RunID      string
}

type SignalRequest struct {
	WorkflowID string
	RunID      string
	Name       string
	Input      any
}

type RunSummary struct {
	WorkflowID   string     `json:"workflowId"`
	RunID        string     `json:"runId"`
	WorkflowType string     `json:"workflowType"`
	Status       Status     `json:"status"`
	StartTime    *time.Time `json:"startTime,omitempty"`
	CloseTime    *time.Time `json:"closeTime,omitempty"`
}

type RunPage struct {
	Runs          []RunSummary `json:"runs"`
	NextPageToken string       `json:"nextPageToken,omitempty"`
}

type PendingActivity struct {
	ActivityID              string     `json:"activityId"`
	ActivityType            string     `json:"activityType"`
	Attempt                 int32      `json:"attempt"`
	MaximumAttempts         int32      `json:"maximumAttempts"`
	LastError               string     `json:"lastError,omitempty"`
	CurrentRetryIntervalMS  int64      `json:"currentRetryIntervalMs,omitempty"`
	NextAttemptScheduleTime *time.Time `json:"nextAttemptScheduleTime,omitempty"`
}

type TimelineEvent struct {
	Time    time.Time `json:"time"`
	Kind    string    `json:"kind"`
	Name    string    `json:"name"`
	Status  string    `json:"status"`
	Attempt int32     `json:"attempt,omitempty"`
	Error   string    `json:"error,omitempty"`
	EventID int64     `json:"eventId"`
}

type RunDetail struct {
	RunSummary
	TaskQueue         string            `json:"taskQueue"`
	HistoryLength     int64             `json:"historyLength"`
	PendingActivities []PendingActivity `json:"pendingActivities"`
	TemporalUIURL     string            `json:"temporalUiUrl,omitempty"`
	Output            any               `json:"output,omitempty"`
	Failure           string            `json:"failure,omitempty"`
	Signals           []sdk.SignalDef   `json:"signals,omitempty"`
	Timeline          []TimelineEvent   `json:"timeline,omitempty"`
	TimelineTruncated bool              `json:"timelineTruncated"`
}

func (s *Service) List(ctx context.Context, request ListRequest) (RunPage, error) {
	if s.inspector == nil {
		return RunPage{}, ErrTemporalUnavailable
	}

	pageSize := request.PageSize
	if pageSize <= 0 {
		pageSize = 50
	}
	if pageSize > 100 {
		pageSize = 100
	}

	var pageToken []byte
	if request.PageToken != "" {
		decoded, err := base64.RawURLEncoding.DecodeString(request.PageToken)
		if err != nil {
			return RunPage{}, fmt.Errorf("%w: invalid page token", ErrInvalidInput)
		}
		pageToken = decoded
	}

	response, err := s.inspector.ListWorkflow(ctx, &workflowservice.ListWorkflowExecutionsRequest{
		PageSize:      int32(pageSize),
		Query:         buildVisibilityQuery(request),
		NextPageToken: pageToken,
	})
	if err != nil {
		return RunPage{}, mapTemporalError(err)
	}

	page := RunPage{
		Runs:          make([]RunSummary, 0, len(response.GetExecutions())),
		NextPageToken: base64.RawURLEncoding.EncodeToString(response.GetNextPageToken()),
	}
	for _, execution := range response.GetExecutions() {
		page.Runs = append(page.Runs, summaryFromInfo(execution))
	}
	return page, nil
}

func (s *Service) Get(ctx context.Context, ref RunRef) (RunDetail, error) {
	if s.inspector == nil {
		return RunDetail{}, ErrTemporalUnavailable
	}

	response, err := s.inspector.DescribeWorkflowExecution(ctx, ref.WorkflowID, ref.RunID)
	if err != nil {
		return RunDetail{}, mapTemporalError(err)
	}
	info := response.GetWorkflowExecutionInfo()
	if info == nil {
		return RunDetail{}, ErrRunNotFound
	}

	detail := RunDetail{
		RunSummary:        summaryFromInfo(info),
		TaskQueue:         info.GetTaskQueue(),
		HistoryLength:     info.GetHistoryLength(),
		PendingActivities: make([]PendingActivity, 0, len(response.GetPendingActivities())),
	}
	if s.temporalUIURL != "" {
		detail.TemporalUIURL = temporalUIURL(
			s.temporalUIURL,
			s.namespace,
			info.GetExecution().GetWorkflowId(),
			info.GetExecution().GetRunId(),
		)
	}
	if detail.Status != StatusRunning {
		if closeEvent, historyErr := s.inspector.CloseEvent(ctx, ref); historyErr == nil && closeEvent != nil {
			applyCloseEvent(&detail, closeEvent)
		}
	}
	if definition, catalogErr := s.catalog.Get(ctx, detail.WorkflowType); catalogErr == nil {
		detail.Signals = definition.Signals
	}
	for _, pending := range response.GetPendingActivities() {
		currentRetryMillis := int64(0)
		if current := pending.GetCurrentRetryInterval(); current != nil {
			currentRetryMillis = current.AsDuration().Milliseconds()
		}
		var nextAttempt *time.Time
		if next := pending.GetNextAttemptScheduleTime(); next != nil {
			value := next.AsTime()
			nextAttempt = &value
		}
		detail.PendingActivities = append(detail.PendingActivities, PendingActivity{
			ActivityID:              pending.GetActivityId(),
			ActivityType:            pending.GetActivityType().GetName(),
			Attempt:                 pending.GetAttempt(),
			MaximumAttempts:         pending.GetMaximumAttempts(),
			LastError:               pending.GetLastFailure().GetMessage(),
			CurrentRetryIntervalMS:  currentRetryMillis,
			NextAttemptScheduleTime: nextAttempt,
		})
	}
	if events, truncated, historyErr := s.inspector.HistoryEvents(ctx, ref, 2000); historyErr == nil {
		detail.Timeline = summarizeTimeline(events)
		detail.TimelineTruncated = truncated
	}
	return detail, nil
}

func summarizeTimeline(events []*historypb.HistoryEvent) []TimelineEvent {
	result := make([]TimelineEvent, 0, len(events))
	for _, event := range events {
		item := TimelineEvent{
			EventID: event.GetEventId(),
		}
		if event.GetEventTime() != nil {
			item.Time = event.GetEventTime().AsTime()
		}
		switch event.GetEventType() {
		case enums.EVENT_TYPE_ACTIVITY_TASK_SCHEDULED:
			attributes := event.GetActivityTaskScheduledEventAttributes()
			item.Kind = "activity"
			item.Name = attributes.GetActivityType().GetName()
			item.Status = "scheduled"
		case enums.EVENT_TYPE_ACTIVITY_TASK_STARTED:
			attributes := event.GetActivityTaskStartedEventAttributes()
			item.Kind = "activity"
			item.Status = "started"
			item.Attempt = attributes.GetAttempt()
		case enums.EVENT_TYPE_ACTIVITY_TASK_COMPLETED:
			item.Kind = "activity"
			item.Status = "completed"
		case enums.EVENT_TYPE_ACTIVITY_TASK_FAILED:
			attributes := event.GetActivityTaskFailedEventAttributes()
			item.Kind = "activity"
			item.Status = "failed"
			item.Error = attributes.GetFailure().GetMessage()
		case enums.EVENT_TYPE_CHILD_WORKFLOW_EXECUTION_STARTED:
			item.Kind = "child_workflow"
			item.Name = event.GetChildWorkflowExecutionStartedEventAttributes().GetWorkflowType().GetName()
			item.Status = "started"
		case enums.EVENT_TYPE_CHILD_WORKFLOW_EXECUTION_COMPLETED:
			item.Kind = "child_workflow"
			item.Status = "completed"
		case enums.EVENT_TYPE_CHILD_WORKFLOW_EXECUTION_FAILED:
			item.Kind = "child_workflow"
			item.Status = "failed"
			item.Error = event.GetChildWorkflowExecutionFailedEventAttributes().GetFailure().GetMessage()
		case enums.EVENT_TYPE_TIMER_STARTED:
			item.Kind = "timer"
			item.Status = "started"
			item.Name = event.GetTimerStartedEventAttributes().GetTimerId()
		case enums.EVENT_TYPE_TIMER_FIRED:
			item.Kind = "timer"
			item.Status = "fired"
			item.Name = event.GetTimerFiredEventAttributes().GetTimerId()
		case enums.EVENT_TYPE_WORKFLOW_EXECUTION_SIGNALED:
			item.Kind = "signal"
			item.Name = event.GetWorkflowExecutionSignaledEventAttributes().GetSignalName()
			item.Status = "received"
		default:
			continue
		}
		result = append(result, item)
	}
	return result
}

func applyCloseEvent(detail *RunDetail, event *historypb.HistoryEvent) {
	switch event.GetEventType() {
	case enums.EVENT_TYPE_WORKFLOW_EXECUTION_COMPLETED:
		payloads := event.GetWorkflowExecutionCompletedEventAttributes().GetResult()
		var output any
		if err := converter.GetDefaultDataConverter().FromPayloads(payloads, &output); err == nil {
			detail.Output = output
		}
	case enums.EVENT_TYPE_WORKFLOW_EXECUTION_FAILED:
		detail.Failure = event.GetWorkflowExecutionFailedEventAttributes().GetFailure().GetMessage()
	case enums.EVENT_TYPE_WORKFLOW_EXECUTION_CANCELED:
		detail.Failure = "workflow canceled"
	case enums.EVENT_TYPE_WORKFLOW_EXECUTION_TIMED_OUT:
		detail.Failure = "workflow timed out"
	}
}

func temporalUIURL(base, namespace, workflowID, runID string) string {
	value, err := url.JoinPath(base, "namespaces", namespace, "workflows", workflowID, runID)
	if err != nil {
		return ""
	}
	return value
}

func (s *Service) Cancel(ctx context.Context, ref RunRef) error {
	if s.inspector == nil {
		return ErrTemporalUnavailable
	}

	response, err := s.inspector.DescribeWorkflowExecution(ctx, ref.WorkflowID, ref.RunID)
	if err != nil {
		return mapTemporalError(err)
	}
	info := response.GetWorkflowExecutionInfo()
	if info == nil {
		return ErrRunNotFound
	}
	if info.GetStatus() != enums.WORKFLOW_EXECUTION_STATUS_RUNNING {
		return fmt.Errorf("%w: run status is %s", ErrInvalidState, statusFromEnum(info.GetStatus()))
	}

	if err := s.inspector.CancelWorkflow(ctx, ref.WorkflowID, ref.RunID); err != nil {
		return mapTemporalError(err)
	}
	return nil
}

func (s *Service) Signal(ctx context.Context, request SignalRequest) error {
	if s.inspector == nil {
		return ErrTemporalUnavailable
	}
	if request.Name == "" {
		return fmt.Errorf("%w: signal name is required", ErrInvalidInput)
	}

	response, err := s.inspector.DescribeWorkflowExecution(ctx, request.WorkflowID, request.RunID)
	if err != nil {
		return mapTemporalError(err)
	}
	info := response.GetWorkflowExecutionInfo()
	if info == nil {
		return ErrRunNotFound
	}
	if info.GetStatus() != enums.WORKFLOW_EXECUTION_STATUS_RUNNING {
		return fmt.Errorf("%w: run is not running", ErrInvalidState)
	}

	definition, err := s.catalog.Get(ctx, info.GetType().GetName())
	if err != nil {
		return ErrSignalNotDefined
	}
	var signal *sdk.SignalDef
	for index := range definition.Signals {
		if definition.Signals[index].Name == request.Name {
			signal = &definition.Signals[index]
			break
		}
	}
	if signal == nil {
		return ErrSignalNotDefined
	}
	if err := sdk.ValidateInput(signal.InputSchema, request.Input); err != nil {
		return fmt.Errorf("%w: %v", ErrInvalidInput, err)
	}
	encoded, err := json.Marshal(request.Input)
	if err != nil {
		return fmt.Errorf("%w: encode signal input: %v", ErrInvalidInput, err)
	}
	payload, err := s.registry.DecodeSignal(definition.ID, request.Name, encoded)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrInvalidInput, err)
	}
	if err := s.inspector.SignalWorkflow(
		ctx,
		request.WorkflowID,
		request.RunID,
		request.Name,
		payload,
	); err != nil {
		return mapTemporalError(err)
	}
	return nil
}

func buildVisibilityQuery(request ListRequest) string {
	filters := make([]string, 0, 4)
	if request.Workflow != "" {
		filters = append(filters, "WorkflowType = '"+escapeQueryValue(request.Workflow)+"'")
	}
	if enumValue, ok := statusToEnum(request.Status); ok {
		filters = append(filters, "ExecutionStatus = '"+enumValue+"'")
	}
	if !request.From.IsZero() {
		filters = append(filters, "StartTime >= '"+request.From.UTC().Format(time.RFC3339)+"'")
	}
	if !request.To.IsZero() {
		filters = append(filters, "StartTime <= '"+request.To.UTC().Format(time.RFC3339)+"'")
	}
	if len(filters) == 0 {
		return ""
	}
	return strings.Join(filters, " AND ")
}

func escapeQueryValue(value string) string {
	return strings.ReplaceAll(value, "'", "\\'")
}

func summaryFromInfo(info *workflowpb.WorkflowExecutionInfo) RunSummary {
	summary := RunSummary{
		WorkflowID:   info.GetExecution().GetWorkflowId(),
		RunID:        info.GetExecution().GetRunId(),
		WorkflowType: info.GetType().GetName(),
		Status:       statusFromEnum(info.GetStatus()),
	}
	if start := info.GetStartTime(); start != nil {
		value := start.AsTime()
		summary.StartTime = &value
	}
	if closeTime := info.GetCloseTime(); closeTime != nil {
		value := closeTime.AsTime()
		summary.CloseTime = &value
	}
	return summary
}

func statusFromEnum(status enums.WorkflowExecutionStatus) Status {
	switch status {
	case enums.WORKFLOW_EXECUTION_STATUS_RUNNING:
		return StatusRunning
	case enums.WORKFLOW_EXECUTION_STATUS_COMPLETED:
		return StatusSucceeded
	case enums.WORKFLOW_EXECUTION_STATUS_FAILED:
		return StatusFailed
	case enums.WORKFLOW_EXECUTION_STATUS_CANCELED:
		return StatusCanceled
	case enums.WORKFLOW_EXECUTION_STATUS_TERMINATED:
		return StatusTerminated
	case enums.WORKFLOW_EXECUTION_STATUS_TIMED_OUT:
		return StatusTimedOut
	case enums.WORKFLOW_EXECUTION_STATUS_CONTINUED_AS_NEW:
		return StatusContinued
	default:
		return StatusUnknown
	}
}

func statusToEnum(status Status) (string, bool) {
	switch status {
	case StatusRunning:
		return "Running", true
	case StatusSucceeded:
		return "Completed", true
	case StatusFailed:
		return "Failed", true
	case StatusCanceled:
		return "Canceled", true
	case StatusTerminated:
		return "Terminated", true
	case StatusTimedOut:
		return "TimedOut", true
	case StatusContinued:
		return "ContinuedAsNew", true
	default:
		return "", false
	}
}

func mapTemporalError(err error) error {
	var notFound *serviceerror.NotFound
	if errors.As(err, &notFound) {
		return ErrRunNotFound
	}
	var unavailable *serviceerror.Unavailable
	if errors.As(err, &unavailable) {
		return ErrTemporalUnavailable
	}
	return err
}
