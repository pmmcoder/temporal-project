package approval

import (
	"errors"
	"fmt"
	"sync"
	"time"

	"personal-workflow-platform/platform/sdk"

	"go.temporal.io/sdk/interceptor"
	"go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/workflow"
)

const (
	SignalName             = "platform.approval"
	MemoKey                = "platformApprovals"
	SearchAttribute        = "PlatformPendingApproval"
	CardActivityName       = "platform.feishu.SendApprovalCard"
	UpdateCardActivityName = "platform.feishu.UpdateApprovalCard"
)

var (
	ErrRuntimeNotConfigured = errors.New("approval runtime is not configured")
	ErrDuplicateKey         = errors.New("approval key is already used in this workflow")
	ErrInvalidRequest       = errors.New("invalid approval request")
	ErrTimedOut             = errors.New("approval timed out")
)

const errTypeDuplicateKey = "ApprovalKeyAlreadyUsed"

type CardRequest struct {
	WorkflowID string    `json:"workflowId"`
	RunID      string    `json:"runId"`
	Approval   Req       `json:"approval"`
	CreatedAt  time.Time `json:"createdAt"`
}

type DecisionCardRequest struct {
	WorkflowID string `json:"workflowId"`
	RunID      string `json:"runId"`
	ApprovalID string `json:"approvalId"`
	Decision   Result `json:"decision"`
	Title      string `json:"title"`
}

type UpdateCardRequest struct {
	MessageID string              `json:"messageId"`
	Card      DecisionCardRequest `json:"card"`
}

type Decision string

const (
	Approved Decision = "approved"
	Rejected Decision = "rejected"
	TimedOut Decision = "timed_out"
	Canceled Decision = "canceled"
)

type TimeoutAction string

const (
	Reject TimeoutAction = "reject"
	Fail   TimeoutAction = "fail"
)

type Field struct {
	Label string `json:"label"`
	Value string `json:"value"`
}

type Req struct {
	Key          string        `json:"key"`
	Title        string        `json:"title"`
	Summary      string        `json:"summary"`
	Fields       []Field       `json:"fields,omitempty"`
	AllowComment bool          `json:"allowComment"`
	Timeout      time.Duration `json:"timeout"`
	OnTimeout    TimeoutAction `json:"onTimeout"`
}

type Result struct {
	ApprovalID string    `json:"approvalId"`
	Decision   Decision  `json:"decision"`
	Comment    string    `json:"comment,omitempty"`
	Operator   string    `json:"operator,omitempty"`
	Channel    string    `json:"channel,omitempty"`
	DecidedAt  time.Time `json:"decidedAt"`
}

type DecisionSignal struct {
	ApprovalID string    `json:"approvalId"`
	Decision   Decision  `json:"decision"`
	Comment    string    `json:"comment,omitempty"`
	Operator   string    `json:"operator,omitempty"`
	Channel    string    `json:"channel,omitempty"`
	DecidedAt  time.Time `json:"decidedAt"`
}

type MemoApproval struct {
	ID           string    `json:"id"`
	Title        string    `json:"title"`
	Summary      string    `json:"summary"`
	Fields       []Field   `json:"fields,omitempty"`
	AllowComment bool      `json:"allowComment"`
	CreatedAt    time.Time `json:"createdAt"`
	Deadline     time.Time `json:"deadline"`
}

type contextKey struct{}

type WorkerOptions struct {
	FeishuEnabled bool
}

type runtimeState struct {
	mu            sync.Mutex
	signals       workflow.ReceiveChannel
	waiters       map[string]workflow.Channel
	pending       map[string]MemoApproval
	pendingOrder  []string
	usedKeys      map[string]struct{}
	feishuEnabled bool
}

type workerInterceptor struct {
	interceptor.WorkerInterceptorBase
	options WorkerOptions
}

func NewWorkerInterceptor(options ...WorkerOptions) interceptor.WorkerInterceptor {
	interceptorOptions := WorkerOptions{}
	if len(options) > 0 {
		interceptorOptions = options[0]
	}
	return &workerInterceptor{options: interceptorOptions}
}

func (i *workerInterceptor) InterceptWorkflow(
	ctx workflow.Context,
	next interceptor.WorkflowInboundInterceptor,
) interceptor.WorkflowInboundInterceptor {
	return &workflowInbound{
		WorkflowInboundInterceptorBase: interceptor.WorkflowInboundInterceptorBase{Next: next},
		options:                        i.options,
	}
}

type workflowInbound struct {
	interceptor.WorkflowInboundInterceptorBase
	options WorkerOptions
}

func (i *workflowInbound) ExecuteWorkflow(
	ctx workflow.Context,
	input *interceptor.ExecuteWorkflowInput,
) (any, error) {
	state := newRuntimeState(ctx, i.options)
	ctx = workflow.WithValue(ctx, contextKey{}, state)
	return i.Next.ExecuteWorkflow(ctx, input)
}

func Request(ctx workflow.Context, request Req) (Result, error) {
	state, ok := ctx.Value(contextKey{}).(*runtimeState)
	if !ok || state == nil {
		return Result{}, ErrRuntimeNotConfigured
	}
	if err := validateRequest(request); err != nil {
		return Result{}, err
	}

	waiter := workflow.NewChannel(ctx)
	if err := state.register(ctx, request, waiter); err != nil {
		return Result{}, err
	}
	defer state.unregister(ctx, request.Key)

	sendFeishuCard := state.feishuEnabled

	var messageID string
	if sendFeishuCard {
		info := workflow.GetInfo(ctx)
		activityCtx := workflow.WithActivityOptions(ctx, sdk.DefaultActivityOptions())
		if err := workflow.ExecuteActivity(
			activityCtx,
			CardActivityName,
			CardRequest{
				WorkflowID: info.WorkflowExecution.ID,
				RunID:      info.WorkflowExecution.RunID,
				Approval:   request,
				CreatedAt:  workflow.Now(ctx),
			},
		).Get(activityCtx, &messageID); err != nil {
			workflow.GetLogger(ctx).Warn("unable to send approval card", "error", err)
			messageID = ""
		}
	}

	timeout := workflow.NewTimer(ctx, request.Timeout)
	var result Result
	var receiveErr error
	selector := workflow.NewSelector(ctx)
	selector.AddReceive(waiter, func(channel workflow.ReceiveChannel, _ bool) {
		var signal DecisionSignal
		channel.Receive(ctx, &signal)
		result = Result(signal)
	})
	selector.AddFuture(timeout, func(workflow.Future) {
		result = Result{
			ApprovalID: request.Key,
			Decision:   TimedOut,
			Channel:    "timeout",
			DecidedAt:  workflow.Now(ctx),
		}
	})
	selector.AddReceive(ctx.Done(), func(workflow.ReceiveChannel, bool) {
		receiveErr = workflow.ErrCanceled
		result = Result{
			ApprovalID: request.Key,
			Decision:   Canceled,
			Channel:    "workflow",
			DecidedAt:  workflow.Now(ctx),
		}
	})
	selector.Select(ctx)

	if receiveErr != nil {
		if sendFeishuCard && messageID != "" {
			state.updateCard(ctx, messageID, request, result)
		}
		return result, receiveErr
	}
	if result.Decision == TimedOut && request.OnTimeout == Fail {
		if sendFeishuCard && messageID != "" {
			state.updateCard(ctx, messageID, request, result)
		}
		return result, temporal.NewApplicationErrorWithCause(
			"approval timed out",
			"ApprovalTimeout",
			ErrTimedOut,
		)
	}
	if sendFeishuCard && messageID != "" {
		state.updateCard(ctx, messageID, request, result)
	}
	return result, nil
}

func (s *runtimeState) updateCard(
	ctx workflow.Context,
	messageID string,
	request Req,
	result Result,
) {
	info := workflow.GetInfo(ctx)
	updateCtx, cancel := workflow.NewDisconnectedContext(ctx)
	defer cancel()
	updateCtx = workflow.WithActivityOptions(updateCtx, sdk.DefaultActivityOptions())
	if err := workflow.ExecuteActivity(
		updateCtx,
		UpdateCardActivityName,
		UpdateCardRequest{
			MessageID: messageID,
			Card: DecisionCardRequest{
				WorkflowID: info.WorkflowExecution.ID,
				RunID:      info.WorkflowExecution.RunID,
				ApprovalID: result.ApprovalID,
				Decision:   result,
				Title:      request.Title,
			},
		},
	).Get(updateCtx, nil); err != nil {
		workflow.GetLogger(ctx).Warn("unable to update approval card", "error", err)
	}
}

func newRuntimeState(ctx workflow.Context, options WorkerOptions) *runtimeState {
	state := &runtimeState{
		signals:       workflow.GetSignalChannel(ctx, SignalName),
		waiters:       make(map[string]workflow.Channel),
		pending:       make(map[string]MemoApproval),
		usedKeys:      make(map[string]struct{}),
		feishuEnabled: options.FeishuEnabled,
	}
	workflow.Go(ctx, state.dispatch)
	return state
}

func (s *runtimeState) dispatch(ctx workflow.Context) {
	for {
		var signal DecisionSignal
		s.signals.Receive(ctx, &signal)

		s.mu.Lock()
		waiter, exists := s.waiters[signal.ApprovalID]
		if exists {
			delete(s.waiters, signal.ApprovalID)
		}
		s.mu.Unlock()
		if exists {
			waiter.Send(ctx, signal)
		}
	}
}

func (s *runtimeState) register(
	ctx workflow.Context,
	request Req,
	waiter workflow.Channel,
) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if _, exists := s.usedKeys[request.Key]; exists {
		message := fmt.Sprintf("%s: %q", ErrDuplicateKey, request.Key)
		return temporal.NewNonRetryableApplicationError(message, errTypeDuplicateKey, ErrDuplicateKey)
	}
	s.usedKeys[request.Key] = struct{}{}
	s.waiters[request.Key] = waiter
	s.pendingOrder = append(s.pendingOrder, request.Key)
	createdAt := workflow.Now(ctx)
	s.pending[request.Key] = MemoApproval{
		ID:           request.Key,
		Title:        request.Title,
		Summary:      request.Summary,
		Fields:       request.Fields,
		AllowComment: request.AllowComment,
		CreatedAt:    createdAt,
		Deadline:     createdAt.Add(request.Timeout),
	}
	return s.updateMetadata(ctx)
}

func IsDuplicateKey(err error) bool {
	var applicationError *temporal.ApplicationError
	return errors.As(err, &applicationError) && applicationError.Type() == errTypeDuplicateKey
}

func (s *runtimeState) unregister(ctx workflow.Context, approvalID string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	delete(s.waiters, approvalID)
	delete(s.pending, approvalID)
	for index, key := range s.pendingOrder {
		if key == approvalID {
			s.pendingOrder = append(s.pendingOrder[:index], s.pendingOrder[index+1:]...)
			break
		}
	}
	_ = s.updateMetadata(ctx)
}

func (s *runtimeState) updateMetadata(ctx workflow.Context) error {
	pending := make([]MemoApproval, 0, len(s.pendingOrder))
	for _, id := range s.pendingOrder {
		pending = append(pending, s.pending[id])
	}
	if err := workflow.UpsertMemo(ctx, map[string]any{MemoKey: pending}); err != nil {
		return fmt.Errorf("update approval memo: %w", err)
	}

	err := workflow.UpsertTypedSearchAttributes(
		ctx,
		temporal.NewSearchAttributeKeyBool(SearchAttribute).ValueSet(len(pending) > 0),
	)
	if err != nil {
		workflow.GetLogger(ctx).Warn("unable to update approval search attribute", "error", err)
	}
	return nil
}

func validateRequest(request Req) error {
	if request.Key == "" {
		return fmt.Errorf("%w: key is required", ErrInvalidRequest)
	}
	if request.Title == "" {
		return fmt.Errorf("%w: title is required", ErrInvalidRequest)
	}
	if request.Timeout <= 0 {
		return fmt.Errorf("%w: timeout must be greater than zero", ErrInvalidRequest)
	}
	if request.OnTimeout != Reject && request.OnTimeout != Fail {
		return fmt.Errorf("%w: timeout action must be reject or fail", ErrInvalidRequest)
	}
	return nil
}
