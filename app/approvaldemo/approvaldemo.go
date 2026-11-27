package approvaldemo

import (
	"time"

	"personal-workflow-platform/platform/sdk"
	"personal-workflow-platform/platform/sdk/approval"

	"go.temporal.io/sdk/workflow"
)

const WorkflowID = "approvaldemo"

type Input struct {
	Title   string `json:"title" description:"审批标题"`
	Summary string `json:"summary" description:"审批摘要"`
}

func Register(registry *sdk.Registry) {
	sdk.Workflow(registry, sdk.WorkflowDef[Input, approval.Result]{
		ID:          WorkflowID,
		DisplayName: "人工确认示例",
		Description: "发起一次人工确认并返回审批结果",
		Fn:          ApprovalWorkflow,
		Example: Input{
			Title:   "发布确认",
			Summary: "示例流程等待人工通过或拒绝",
		},
	})
}

func ApprovalWorkflow(ctx workflow.Context, input Input) (approval.Result, error) {
	return approval.Request(ctx, approval.Req{
		Key:          "approval",
		Title:        input.Title,
		Summary:      input.Summary,
		AllowComment: true,
		Timeout:      24 * time.Hour,
		OnTimeout:    approval.Reject,
	})
}
