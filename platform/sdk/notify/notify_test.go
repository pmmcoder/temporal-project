package notify_test

import (
	"log/slog"
	"testing"

	"personal-workflow-platform/platform/config"
	"personal-workflow-platform/platform/feishu"
	"personal-workflow-platform/platform/sdk/notify"

	"github.com/stretchr/testify/mock"
	"go.temporal.io/sdk/activity"
	"go.temporal.io/sdk/testsuite"
	"go.temporal.io/sdk/workflow"
)

func notifyWorkflow(ctx workflow.Context) error {
	return notify.Send(ctx, notify.Msg{Markdown: "**完成**"})
}

func TestSendExecutesPlatformActivity(t *testing.T) {
	var suite testsuite.WorkflowTestSuite
	environment := suite.NewTestWorkflowEnvironment()
	environment.RegisterWorkflow(notifyWorkflow)
	feishuRuntime := feishu.NewRuntime(config.FeishuConfig{}, slog.Default())
	environment.RegisterActivityWithOptions(
		feishuRuntime.SendMessageActivity,
		activity.RegisterOptions{Name: notify.ActivityName},
	)
	environment.OnActivity(
		notify.ActivityName,
		mock.Anything,
		notify.Msg{Markdown: "**完成**"},
	).Return(nil)

	environment.ExecuteWorkflow(notifyWorkflow)
	if err := environment.GetWorkflowError(); err != nil {
		t.Fatalf("workflow error = %v", err)
	}
	environment.AssertExpectations(t)
}
