package greeting_test

import (
	"testing"

	"personal-workflow-platform/app/greeting"
	"personal-workflow-platform/platform/runtime"
	"personal-workflow-platform/platform/sdk"

	"go.temporal.io/sdk/testsuite"
)

func TestGreetingWorkflow(t *testing.T) {
	registry := sdk.NewRegistry()
	greeting.Register(registry)

	var suite testsuite.WorkflowTestSuite
	environment := suite.NewTestWorkflowEnvironment()
	if err := runtime.Register(environment, registry); err != nil {
		t.Fatalf("register workflow definitions: %v", err)
	}

	environment.ExecuteWorkflow(
		greeting.GreetingWorkflow,
		greeting.GreetingInput{Names: []string{"Alice", "Bob"}},
	)

	if !environment.IsWorkflowCompleted() {
		t.Fatal("workflow did not complete")
	}
	if err := environment.GetWorkflowError(); err != nil {
		t.Fatalf("workflow error = %v", err)
	}

	var result []string
	if err := environment.GetWorkflowResult(&result); err != nil {
		t.Fatalf("workflow result = %v", err)
	}
	if len(result) != 2 || result[0] != "Hello Alice" || result[1] != "Hello Bob" {
		t.Fatalf("result = %#v, want [Hello Alice, Hello Bob]", result)
	}
}
