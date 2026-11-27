package notify

import (
	"personal-workflow-platform/platform/sdk"

	"go.temporal.io/sdk/workflow"
)

const ActivityName = "platform.feishu.SendMessage"

type Msg struct {
	Markdown string `json:"markdown"`
	ChatID   string `json:"chatId,omitempty"`
}

func Send(ctx workflow.Context, message Msg) error {
	ctx = workflow.WithActivityOptions(ctx, sdk.DefaultActivityOptions())
	return workflow.ExecuteActivity(ctx, ActivityName, message).Get(ctx, nil)
}
