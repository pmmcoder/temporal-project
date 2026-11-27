package testkit

import (
	"os"
	"testing"

	"personal-workflow-platform/app"
	"personal-workflow-platform/platform/sdk"

	"go.temporal.io/sdk/client"
	"go.temporal.io/sdk/worker"
	"go.temporal.io/sdk/workflow"
)

func Replay(t *testing.T, historyFile string) {
	t.Helper()

	registry := sdk.NewRegistry()
	app.Register(registry)
	if err := registry.Validate(); err != nil {
		t.Fatalf("validate application workflows: %v", err)
	}

	replayer := worker.NewWorkflowReplayer()
	for _, info := range registry.Workflows() {
		fn, _, ok := registry.Workflow(info.ID)
		if !ok {
			continue
		}
		replayer.RegisterWorkflowWithOptions(fn, workflow.RegisterOptions{Name: info.ID})
	}
	file, err := os.Open(historyFile)
	if err != nil {
		t.Fatalf("open %s: %v", historyFile, err)
	}
	defer file.Close()
	history, err := client.HistoryFromJSON(file, client.HistoryJSONOptions{})
	if err != nil {
		t.Fatalf("decode %s: %v", historyFile, err)
	}
	if len(history.Events) == 0 {
		t.Fatalf("history %s has no events", historyFile)
	}
	started := history.Events[0].GetWorkflowExecutionStartedEventAttributes()
	if err := replayer.ReplayWorkflowHistoryWithOptions(
		nil,
		history,
		worker.ReplayWorkflowHistoryOptions{
			OriginalExecution: workflow.Execution{
				ID:    started.GetWorkflowId(),
				RunID: started.GetOriginalExecutionRunId(),
			},
		},
	); err != nil {
		t.Fatalf("replay %s: %v", historyFile, err)
	}
}
