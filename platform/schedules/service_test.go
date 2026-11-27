package schedules_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"personal-workflow-platform/platform/catalog"
	"personal-workflow-platform/platform/schedules"
	"personal-workflow-platform/platform/sdk"

	"go.temporal.io/api/enums/v1"
	"go.temporal.io/sdk/client"
	"go.temporal.io/sdk/converter"
	"go.temporal.io/sdk/workflow"
)

type greetingInput struct {
	Name string `json:"name"`
}

func greetingWorkflow(ctx workflow.Context, input greetingInput) (string, error) {
	return input.Name, nil
}

type fakeHandle struct {
	id            string
	paused        bool
	deleted       bool
	description   *client.ScheduleDescription
	updateOptions *client.ScheduleUpdateOptions
}

func (h *fakeHandle) GetID() string                                                  { return h.id }
func (h *fakeHandle) Delete(context.Context) error                                   { h.deleted = true; return nil }
func (h *fakeHandle) Backfill(context.Context, client.ScheduleBackfillOptions) error { return nil }
func (h *fakeHandle) Update(_ context.Context, options client.ScheduleUpdateOptions) error {
	h.updateOptions = &options
	return nil
}
func (h *fakeHandle) Describe(context.Context) (*client.ScheduleDescription, error) {
	if h.description != nil {
		return h.description, nil
	}
	return &client.ScheduleDescription{}, nil
}
func (h *fakeHandle) Trigger(context.Context, client.ScheduleTriggerOptions) error { return nil }
func (h *fakeHandle) Pause(context.Context, client.SchedulePauseOptions) error {
	h.paused = true
	return nil
}
func (h *fakeHandle) Unpause(context.Context, client.ScheduleUnpauseOptions) error {
	h.paused = false
	return nil
}

type fakeScheduleClient struct {
	createOptions client.ScheduleOptions
	handle        client.ScheduleHandle
	listEntries   []*client.ScheduleListEntry
}

func (c *fakeScheduleClient) Create(_ context.Context, options client.ScheduleOptions) (client.ScheduleHandle, error) {
	c.createOptions = options
	return c.handle, nil
}

func (c *fakeScheduleClient) List(context.Context, client.ScheduleListOptions) (client.ScheduleListIterator, error) {
	return &fakeIterator{entries: c.listEntries}, nil
}

func (c *fakeScheduleClient) GetHandle(_ context.Context, id string) client.ScheduleHandle {
	if c.handle != nil {
		return c.handle
	}
	return &fakeHandle{id: id}
}

type fakeIterator struct {
	entries []*client.ScheduleListEntry
	index   int
}

func (i *fakeIterator) HasNext() bool { return i.index < len(i.entries) }

func (i *fakeIterator) Next() (*client.ScheduleListEntry, error) {
	entry := i.entries[i.index]
	i.index++
	return entry, nil
}

func newService(t *testing.T, scheduleClient client.ScheduleClient) *schedules.Service {
	t.Helper()
	registry := sdk.NewRegistry()
	sdk.Workflow(registry, sdk.WorkflowDef[greetingInput, string]{
		ID:          "greeting",
		DisplayName: "Greeting",
		Fn:          greetingWorkflow,
		Example:     greetingInput{Name: "Alice"},
	})
	catalogView, err := catalog.New(registry)
	if err != nil {
		t.Fatalf("catalog.New() error = %v", err)
	}
	service, err := schedules.New(schedules.Options{
		Catalog:        catalogView,
		Registry:       registry,
		ScheduleClient: scheduleClient,
		TaskQueue:      "personal-workflows",
	})
	if err != nil {
		t.Fatalf("schedules.New() error = %v", err)
	}
	return service
}

func TestCreateMapsScheduleOptions(t *testing.T) {
	scheduleClient := &fakeScheduleClient{handle: &fakeHandle{id: "greeting-abc12345"}}
	service := newService(t, scheduleClient)

	schedule, err := service.Create(context.Background(), schedules.CreateRequest{
		Name:         "每日问候",
		DefinitionID: "greeting",
		Input:        greetingInput{Name: "Alice"},
		Cron:         "0 9 * * *",
		Timezone:     "Asia/Shanghai",
	})
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	if schedule.ID == "" {
		t.Fatal("schedule ID is empty")
	}
	if len(scheduleClient.createOptions.Spec.CronExpressions) != 1 ||
		scheduleClient.createOptions.Spec.CronExpressions[0] != "0 9 * * *" {
		t.Errorf("cron = %#v", scheduleClient.createOptions.Spec.CronExpressions)
	}
	if scheduleClient.createOptions.Spec.TimeZoneName != "Asia/Shanghai" {
		t.Errorf("timezone = %q", scheduleClient.createOptions.Spec.TimeZoneName)
	}
	action, ok := scheduleClient.createOptions.Action.(*client.ScheduleWorkflowAction)
	if !ok {
		t.Fatalf("action type = %T", scheduleClient.createOptions.Action)
	}
	if action.Workflow != "greeting" || action.TaskQueue != "personal-workflows" {
		t.Errorf("action = %#v", action)
	}
	if _, ok := action.Args[0].(greetingInput); !ok {
		t.Errorf("argument type = %T, want greetingInput", action.Args[0])
	}
	if action.Memo["displayName"] != "每日问候" {
		t.Errorf("memo = %#v", action.Memo)
	}
	if scheduleClient.createOptions.Overlap != enums.SCHEDULE_OVERLAP_POLICY_SKIP {
		t.Errorf("overlap = %v", scheduleClient.createOptions.Overlap)
	}
	if scheduleClient.createOptions.CatchupWindow != time.Hour {
		t.Errorf("catchup window = %s", scheduleClient.createOptions.CatchupWindow)
	}
}

func TestCreateRejectsInvalidCron(t *testing.T) {
	scheduleClient := &fakeScheduleClient{handle: &fakeHandle{id: "unused"}}
	service := newService(t, scheduleClient)

	_, err := service.Create(context.Background(), schedules.CreateRequest{
		Name:         "bad",
		DefinitionID: "greeting",
		Input:        greetingInput{Name: "Alice"},
		Cron:         "bad cron",
		Timezone:     "Asia/Shanghai",
	})
	if !errors.Is(err, schedules.ErrInvalidInput) {
		t.Fatalf("Create() error = %v, want ErrInvalidInput", err)
	}
}

func TestListAndPause(t *testing.T) {
	last := time.Date(2026, 11, 20, 1, 0, 0, 0, time.UTC)
	next := last.Add(24 * time.Hour)
	handle := &fakeHandle{id: "greeting-abc12345"}
	scheduleClient := &fakeScheduleClient{
		handle: handle,
		listEntries: []*client.ScheduleListEntry{{
			ID: "greeting-abc12345",
			Spec: &client.ScheduleSpec{
				CronExpressions: []string{"0 9 * * *"},
				TimeZoneName:    "Asia/Shanghai",
			},
			WorkflowType:    workflow.Type{Name: "greeting"},
			RecentActions:   []client.ScheduleActionResult{{ActualTime: last}},
			NextActionTimes: []time.Time{next},
		}},
	}
	service := newService(t, scheduleClient)

	items, err := service.List(context.Background())
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}
	if len(items) != 1 || items[0].DefinitionID != "greeting" || items[0].Cron != "0 9 * * *" {
		t.Fatalf("items = %#v", items)
	}
	if items[0].LastActionTime == nil || items[0].NextActionTime == nil {
		t.Fatalf("action times = %#v", items[0])
	}

	if err := service.Pause(context.Background(), "greeting-abc12345"); err != nil {
		t.Fatalf("Pause() error = %v", err)
	}
	if !handle.paused {
		t.Fatal("schedule was not paused")
	}
}

func TestUpdateChangesCronAndWorkflowInput(t *testing.T) {
	handle := &fakeHandle{
		id: "greeting-abc12345",
		description: &client.ScheduleDescription{
			Schedule: client.Schedule{
				Action: &client.ScheduleWorkflowAction{
					Workflow:  "greeting",
					TaskQueue: "personal-workflows",
				},
				Spec:  &client.ScheduleSpec{CronExpressions: []string{"0 9 * * *"}},
				State: &client.ScheduleState{Note: "旧名称"},
			},
		},
	}
	service := newService(t, &fakeScheduleClient{handle: handle})

	err := service.Update(context.Background(), schedules.UpdateRequest{
		ID:       "greeting-abc12345",
		Name:     "新名称",
		Input:    greetingInput{Name: "Bob"},
		Cron:     "30 8 * * 1-5",
		Timezone: "Asia/Shanghai",
		Paused:   true,
	})
	if err != nil {
		t.Fatalf("Update() error = %v", err)
	}
	if handle.updateOptions == nil {
		t.Fatal("Schedule.Update was not called")
	}
	update, err := handle.updateOptions.DoUpdate(client.ScheduleUpdateInput{
		Description: *handle.description,
	})
	if err != nil {
		t.Fatalf("DoUpdate() error = %v", err)
	}
	if update.Schedule.Spec.CronExpressions[0] != "30 8 * * 1-5" {
		t.Errorf("cron = %#v", update.Schedule.Spec.CronExpressions)
	}
	if !update.Schedule.State.Paused || update.Schedule.State.Note != "新名称" {
		t.Errorf("state = %#v", update.Schedule.State)
	}
	action := update.Schedule.Action.(*client.ScheduleWorkflowAction)
	if _, ok := action.Args[0].(greetingInput); !ok {
		t.Errorf("action args = %#v", action.Args)
	}
}

func TestListMarksScheduleNeedsReviewWhenInputNoLongerMatches(t *testing.T) {
	invalidPayload, err := converter.GetDefaultDataConverter().ToPayload(map[string]any{})
	if err != nil {
		t.Fatalf("encode payload: %v", err)
	}
	handle := &fakeHandle{
		id: "greeting-abc12345",
		description: &client.ScheduleDescription{
			Schedule: client.Schedule{
				Action: &client.ScheduleWorkflowAction{
					Workflow: "greeting",
					Args:     []any{invalidPayload},
				},
				Spec: &client.ScheduleSpec{
					Calendars: []client.ScheduleCalendarSpec{{
						Second:     []client.ScheduleRange{{Start: 0}},
						Minute:     []client.ScheduleRange{{Start: 0}},
						Hour:       []client.ScheduleRange{{Start: 9}},
						DayOfMonth: []client.ScheduleRange{{Start: 1, End: 31}},
						Month:      []client.ScheduleRange{{Start: 1, End: 12}},
						DayOfWeek:  []client.ScheduleRange{{Start: 0, End: 6}},
					}},
					TimeZoneName: "Asia/Shanghai",
				},
			},
		},
	}
	scheduleClient := &fakeScheduleClient{
		handle: handle,
		listEntries: []*client.ScheduleListEntry{{
			ID:           "greeting-abc12345",
			WorkflowType: workflow.Type{Name: "greeting"},
		}},
	}
	service := newService(t, scheduleClient)

	items, err := service.List(context.Background())
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}
	if len(items) != 1 || !items[0].NeedsReview {
		t.Fatalf("items = %#v", items)
	}
	if items[0].Cron != "0 9 * * *" {
		t.Errorf("cron = %q", items[0].Cron)
	}
}
