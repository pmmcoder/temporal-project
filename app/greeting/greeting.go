package greeting

import (
	"context"
	"fmt"

	"personal-workflow-platform/platform/sdk"

	"go.temporal.io/sdk/workflow"
)

const (
	WorkflowID      = "greeting"
	ChildWorkflowID = "greeting.one"
	ActivityName    = "greeting.Greet"
)

type GreetingInput struct {
	Names []string `json:"names" description:"需要问候的名字"`
}

type GreetOneInput struct {
	Name string `json:"name" description:"单个名字"`
}

func Register(registry *sdk.Registry) {
	sdk.Workflow(registry, sdk.WorkflowDef[GreetingInput, []string]{
		ID:          WorkflowID,
		DisplayName: "并行问候",
		Description: "为每个名字启动内部子流程并汇总问候语",
		Fn:          GreetingWorkflow,
		Example:     GreetingInput{Names: []string{"Alice", "Bob"}},
	})
	sdk.Workflow(registry, sdk.WorkflowDef[GreetOneInput, string]{
		ID:          ChildWorkflowID,
		DisplayName: "单个问候",
		Description: "仅供并行问候流程内部使用",
		Fn:          GreetOneWorkflow,
		Example:     GreetOneInput{Name: "Alice"},
		Internal:    true,
	})
	sdk.Activity(registry, sdk.ActivityDef{
		Name: ActivityName,
		Fn:   Greet,
	})
}

func GreetingWorkflow(ctx workflow.Context, input GreetingInput) ([]string, error) {
	parentID := workflow.GetInfo(ctx).WorkflowExecution.ID
	futures := make([]workflow.ChildWorkflowFuture, len(input.Names))

	for i, name := range input.Names {
		childCtx := workflow.WithChildOptions(ctx, workflow.ChildWorkflowOptions{
			WorkflowID: fmt.Sprintf("%s/%d", parentID, i),
		})
		futures[i] = workflow.ExecuteChildWorkflow(
			childCtx,
			ChildWorkflowID,
			GreetOneInput{Name: name},
		)
	}

	results := make([]string, len(futures))
	for i, future := range futures {
		if err := future.Get(ctx, &results[i]); err != nil {
			return nil, err
		}
	}
	return results, nil
}

func GreetOneWorkflow(ctx workflow.Context, input GreetOneInput) (string, error) {
	ctx = workflow.WithActivityOptions(ctx, sdk.DefaultActivityOptions())

	var result string
	if err := workflow.ExecuteActivity(ctx, ActivityName, input.Name).Get(ctx, &result); err != nil {
		return "", err
	}
	return result, nil
}

func Greet(_ context.Context, name string) (string, error) {
	return fmt.Sprintf("Hello %s", name), nil
}
