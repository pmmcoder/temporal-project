package approval_test

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"personal-workflow-platform/platform/sdk/approval"

	"go.temporal.io/sdk/activity"
	"go.temporal.io/sdk/interceptor"
	"go.temporal.io/sdk/testsuite"
	"go.temporal.io/sdk/worker"
	"go.temporal.io/sdk/workflow"
)

func approvalWorkflow(ctx workflow.Context, request approval.Req) (approval.Result, error) {
	return approval.Request(ctx, request)
}

func newEnvironment(t *testing.T, options ...approval.WorkerOptions) *testsuite.TestWorkflowEnvironment {
	t.Helper()
	var suite testsuite.WorkflowTestSuite
	environment := suite.NewTestWorkflowEnvironment()
	environment.SetWorkerOptions(worker.Options{
		Interceptors: []interceptor.WorkerInterceptor{approval.NewWorkerInterceptor(options...)},
	})
	environment.RegisterWorkflow(approvalWorkflow)
	return environment
}

var cardSent atomic.Int32
var cardUpdated atomic.Int32

func sendCardActivity(context.Context, approval.CardRequest) (string, error) {
	cardSent.Add(1)
	return "om_test", nil
}

func updateCardActivity(context.Context, approval.UpdateCardRequest) error {
	cardUpdated.Add(1)
	return nil
}

func validRequest(key string) approval.Req {
	return approval.Req{
		Key:       key,
		Title:     "发布确认",
		Summary:   "准备发布",
		Timeout:   time.Hour,
		OnTimeout: approval.Reject,
	}
}

func TestRequestReturnsApprovedDecision(t *testing.T) {
	environment := newEnvironment(t)
	decidedAt := time.Date(2026, 11, 20, 12, 0, 0, 0, time.UTC)
	environment.RegisterDelayedCallback(func() {
		environment.SignalWorkflow(approval.SignalName, approval.DecisionSignal{
			ApprovalID: "publish",
			Decision:   approval.Approved,
			Comment:    "ok",
			Operator:   "tester",
			Channel:    "web",
			DecidedAt:  decidedAt,
		})
	}, time.Second)

	environment.ExecuteWorkflow(approvalWorkflow, validRequest("publish"))
	if err := environment.GetWorkflowError(); err != nil {
		t.Fatalf("workflow error = %v", err)
	}

	var result approval.Result
	if err := environment.GetWorkflowResult(&result); err != nil {
		t.Fatalf("workflow result = %v", err)
	}
	if result.Decision != approval.Approved || result.Comment != "ok" {
		t.Fatalf("result = %#v", result)
	}
}

func TestRequestTimesOutAsRejected(t *testing.T) {
	environment := newEnvironment(t)
	request := validRequest("timeout")
	request.Timeout = 10 * time.Second

	environment.ExecuteWorkflow(approvalWorkflow, request)
	if err := environment.GetWorkflowError(); err != nil {
		t.Fatalf("workflow error = %v", err)
	}

	var result approval.Result
	if err := environment.GetWorkflowResult(&result); err != nil {
		t.Fatalf("workflow result = %v", err)
	}
	if result.Decision != approval.TimedOut {
		t.Fatalf("decision = %q, want timed_out", result.Decision)
	}
}

func TestRequestTimeoutCanFailWorkflow(t *testing.T) {
	environment := newEnvironment(t)
	request := validRequest("timeout-fail")
	request.Timeout = 10 * time.Second
	request.OnTimeout = approval.Fail

	environment.ExecuteWorkflow(approvalWorkflow, request)
	if err := environment.GetWorkflowError(); err == nil {
		t.Fatal("workflow error = nil, want timeout error")
	}
}

func TestRequestIgnoresUnmatchedApprovalSignal(t *testing.T) {
	environment := newEnvironment(t)
	environment.RegisterDelayedCallback(func() {
		environment.SignalWorkflow(approval.SignalName, approval.DecisionSignal{
			ApprovalID: "other",
			Decision:   approval.Rejected,
			Operator:   "tester",
			Channel:    "web",
		})
	}, time.Second)
	environment.RegisterDelayedCallback(func() {
		environment.SignalWorkflow(approval.SignalName, approval.DecisionSignal{
			ApprovalID: "right",
			Decision:   approval.Approved,
			Operator:   "tester",
			Channel:    "web",
		})
	}, 2*time.Second)

	environment.ExecuteWorkflow(approvalWorkflow, validRequest("right"))
	if err := environment.GetWorkflowError(); err != nil {
		t.Fatalf("workflow error = %v", err)
	}

	var result approval.Result
	if err := environment.GetWorkflowResult(&result); err != nil {
		t.Fatalf("workflow result = %v", err)
	}
	if result.Decision != approval.Approved {
		t.Fatalf("decision = %q, want approved", result.Decision)
	}
}

func duplicateKeyWorkflow(ctx workflow.Context) error {
	request := validRequest("duplicate")
	if _, err := approval.Request(ctx, request); err != nil {
		return err
	}
	_, err := approval.Request(ctx, request)
	return err
}

func TestRequestRejectsDuplicateKeyWithinWorkflow(t *testing.T) {
	var suite testsuite.WorkflowTestSuite
	environment := suite.NewTestWorkflowEnvironment()
	environment.SetWorkerOptions(worker.Options{
		Interceptors: []interceptor.WorkerInterceptor{approval.NewWorkerInterceptor()},
	})
	environment.RegisterWorkflow(duplicateKeyWorkflow)
	environment.RegisterDelayedCallback(func() {
		environment.SignalWorkflow(approval.SignalName, approval.DecisionSignal{
			ApprovalID: "duplicate",
			Decision:   approval.Approved,
			Operator:   "tester",
			Channel:    "web",
		})
	}, time.Second)

	environment.ExecuteWorkflow(duplicateKeyWorkflow)
	err := environment.GetWorkflowError()
	if !approval.IsDuplicateKey(err) {
		t.Fatalf("workflow error = %v, want duplicate key application error", err)
	}
}

func TestRequestNewWorkflowSendsAndUpdatesFeishuCard(t *testing.T) {
	cardSent.Store(0)
	cardUpdated.Store(0)
	environment := newEnvironment(t, approval.WorkerOptions{FeishuEnabled: true})
	environment.RegisterActivityWithOptions(
		sendCardActivity,
		activity.RegisterOptions{Name: approval.CardActivityName},
	)
	environment.RegisterActivityWithOptions(
		updateCardActivity,
		activity.RegisterOptions{Name: approval.UpdateCardActivityName},
	)
	environment.RegisterDelayedCallback(func() {
		environment.SignalWorkflow(approval.SignalName, approval.DecisionSignal{
			ApprovalID: "card",
			Decision:   approval.Approved,
			Operator:   "tester",
			Channel:    "feishu",
		})
	}, time.Second)

	environment.ExecuteWorkflow(approvalWorkflow, validRequest("card"))
	if err := environment.GetWorkflowError(); err != nil {
		t.Fatalf("workflow error = %v", err)
	}
	if cardSent.Load() != 1 || cardUpdated.Load() != 1 {
		t.Fatalf("card activities: sent=%d updated=%d", cardSent.Load(), cardUpdated.Load())
	}
}
