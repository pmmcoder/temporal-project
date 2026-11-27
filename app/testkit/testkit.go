package testkit

import (
	"testing"
	"time"

	"personal-workflow-platform/app"
	"personal-workflow-platform/platform/runtime"
	"personal-workflow-platform/platform/sdk"
	"personal-workflow-platform/platform/sdk/approval"

	"go.temporal.io/sdk/interceptor"
	"go.temporal.io/sdk/testsuite"
	"go.temporal.io/sdk/worker"
)

func New(t *testing.T) *testsuite.TestWorkflowEnvironment {
	t.Helper()

	registry := sdk.NewRegistry()
	app.Register(registry)

	var suite testsuite.WorkflowTestSuite
	environment := suite.NewTestWorkflowEnvironment()
	environment.SetWorkerOptions(worker.Options{
		Interceptors: []interceptor.WorkerInterceptor{approval.NewWorkerInterceptor()},
	})
	if err := runtime.Register(environment, registry); err != nil {
		t.Fatalf("register application workflows: %v", err)
	}
	return environment
}

func Approve(environment *testsuite.TestWorkflowEnvironment, approvalID string) {
	decide(environment, approvalID, approval.Approved)
}

func Reject(environment *testsuite.TestWorkflowEnvironment, approvalID string) {
	decide(environment, approvalID, approval.Rejected)
}

func decide(
	environment *testsuite.TestWorkflowEnvironment,
	approvalID string,
	decision approval.Decision,
) {
	environment.RegisterDelayedCallback(func() {
		environment.SignalWorkflow(approval.SignalName, approval.DecisionSignal{
			ApprovalID: approvalID,
			Decision:   decision,
			Comment:    "testkit",
			Operator:   "testkit",
			Channel:    "test",
			DecidedAt:  time.Now().UTC(),
		})
	}, time.Second)
}
