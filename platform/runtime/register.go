package runtime

import (
	"log/slog"

	"personal-workflow-platform/platform/config"
	"personal-workflow-platform/platform/feishu"
	"personal-workflow-platform/platform/sdk"
	"personal-workflow-platform/platform/sdk/approval"
	"personal-workflow-platform/platform/sdk/notify"

	"go.temporal.io/sdk/activity"
	"go.temporal.io/sdk/workflow"
)

type Worker interface {
	RegisterWorkflowWithOptions(any, workflow.RegisterOptions)
	RegisterActivityWithOptions(any, activity.RegisterOptions)
}

func Register(worker Worker, registry *sdk.Registry, runtimes ...*feishu.Runtime) error {
	if err := registry.Validate(); err != nil {
		return err
	}

	for _, info := range registry.Workflows() {
		fn, _, ok := registry.Workflow(info.ID)
		if !ok {
			continue
		}
		worker.RegisterWorkflowWithOptions(fn, workflow.RegisterOptions{Name: info.ID})
	}
	for _, definition := range registry.Activities() {
		worker.RegisterActivityWithOptions(
			definition.Fn,
			activity.RegisterOptions{Name: definition.Name},
		)
	}
	feishuRuntime := feishu.NewRuntimeWithMessages(
		config.FeishuConfig{},
		slog.Default(),
		nil,
	)
	if len(runtimes) > 0 && runtimes[0] != nil {
		feishuRuntime = runtimes[0]
	}
	worker.RegisterActivityWithOptions(
		feishuRuntime.SendMessageActivity,
		activity.RegisterOptions{Name: notify.ActivityName},
	)
	worker.RegisterActivityWithOptions(
		feishuRuntime.SendApprovalCardActivity,
		activity.RegisterOptions{Name: approval.CardActivityName},
	)
	worker.RegisterActivityWithOptions(
		feishuRuntime.UpdateApprovalCardActivity,
		activity.RegisterOptions{Name: approval.UpdateCardActivityName},
	)
	return nil
}
