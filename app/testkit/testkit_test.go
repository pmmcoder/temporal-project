package testkit_test

import (
	"testing"

	"personal-workflow-platform/app/approvaldemo"
	"personal-workflow-platform/app/testkit"
	"personal-workflow-platform/platform/sdk/approval"
)

func TestApproveDrivesApprovalWorkflow(t *testing.T) {
	environment := testkit.New(t)
	testkit.Approve(environment, "approval")

	environment.ExecuteWorkflow(
		approvaldemo.ApprovalWorkflow,
		approvaldemo.Input{Title: "发布", Summary: "测试审批"},
	)
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
