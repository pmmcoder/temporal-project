package schedules

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"personal-workflow-platform/platform/catalog"
	"personal-workflow-platform/platform/sdk"

	"github.com/robfig/cron"
	commonpb "go.temporal.io/api/common/v1"
	"go.temporal.io/api/enums/v1"
	"go.temporal.io/api/serviceerror"
	"go.temporal.io/sdk/client"
	"go.temporal.io/sdk/converter"
)

var (
	ErrInvalidInput        = errors.New("invalid schedule input")
	ErrNotFound            = errors.New("schedule or workflow not found")
	ErrTemporalUnavailable = errors.New("Temporal is unavailable")
)

type Catalog interface {
	Get(context.Context, string) (catalog.Definition, error)
}

type Registry interface {
	Workflow(string) (any, sdk.WorkflowInfo, bool)
	DecodeInput(string, json.RawMessage) (any, error)
}

type Options struct {
	Catalog        Catalog
	Registry       Registry
	ScheduleClient client.ScheduleClient
	TaskQueue      string
}

type Service struct {
	catalog        Catalog
	registry       Registry
	scheduleClient client.ScheduleClient
	taskQueue      string
}

type CreateRequest struct {
	Name         string
	DefinitionID string
	Input        any
	Cron         string
	Timezone     string
	Paused       bool
}

type Schedule struct {
	ID             string     `json:"id"`
	Name           string     `json:"name"`
	DefinitionID   string     `json:"definitionId"`
	Cron           string     `json:"cron"`
	Timezone       string     `json:"timezone"`
	Paused         bool       `json:"paused"`
	LastActionTime *time.Time `json:"lastActionTime,omitempty"`
	NextActionTime *time.Time `json:"nextActionTime,omitempty"`
	NeedsReview    bool       `json:"needsReview"`
}

type UpdateRequest struct {
	ID       string
	Name     string
	Input    any
	Cron     string
	Timezone string
	Paused   bool
}

func (s *Service) List(ctx context.Context) ([]Schedule, error) {
	iterator, err := s.scheduleClient.List(ctx, client.ScheduleListOptions{PageSize: 100})
	if err != nil {
		return nil, mapTemporalError(err)
	}

	result := make([]Schedule, 0)
	for iterator.HasNext() {
		entry, err := iterator.Next()
		if err != nil {
			return nil, mapTemporalError(err)
		}
		schedule := Schedule{
			ID:           entry.ID,
			DefinitionID: entry.WorkflowType.Name,
			Paused:       entry.Paused,
			Name:         entry.Note,
		}
		if entry.Spec != nil {
			if len(entry.Spec.CronExpressions) > 0 {
				schedule.Cron = entry.Spec.CronExpressions[0]
			}
			schedule.Timezone = entry.Spec.TimeZoneName
		}
		schedule.Name, _ = memoString(entry.Memo, "displayName")
		if len(entry.RecentActions) > 0 {
			value := entry.RecentActions[len(entry.RecentActions)-1].ActualTime
			schedule.LastActionTime = &value
		}
		if len(entry.NextActionTimes) > 0 {
			value := entry.NextActionTimes[0]
			schedule.NextActionTime = &value
		}
		if description, describeErr := s.scheduleClient.GetHandle(ctx, entry.ID).Describe(ctx); describeErr == nil {
			applyDescription(&schedule, description)
			schedule.NeedsReview = s.needsReview(ctx, description)
		}
		result = append(result, schedule)
	}
	return result, nil
}

func (s *Service) Update(ctx context.Context, request UpdateRequest) error {
	if request.ID == "" || request.Name == "" || request.Cron == "" || request.Timezone == "" {
		return fmt.Errorf("%w: id, name, cron and timezone are required", ErrInvalidInput)
	}
	if _, err := cron.ParseStandard(request.Cron); err != nil {
		return fmt.Errorf("%w: invalid cron expression: %v", ErrInvalidInput, err)
	}
	if _, err := time.LoadLocation(request.Timezone); err != nil {
		return fmt.Errorf("%w: invalid timezone: %v", ErrInvalidInput, err)
	}

	handle := s.scheduleClient.GetHandle(ctx, request.ID)
	description, err := handle.Describe(ctx)
	if err != nil {
		return mapTemporalError(err)
	}
	action, ok := description.Schedule.Action.(*client.ScheduleWorkflowAction)
	if !ok {
		return ErrNotFound
	}
	definitionID, ok := action.Workflow.(string)
	if !ok {
		return ErrNotFound
	}
	definition, err := s.catalog.Get(ctx, definitionID)
	if err != nil {
		return ErrNotFound
	}
	if err := sdk.ValidateInput(definition.InputSchema, request.Input); err != nil {
		return fmt.Errorf("%w: %v", ErrInvalidInput, err)
	}
	encodedInput, err := json.Marshal(request.Input)
	if err != nil {
		return fmt.Errorf("%w: encode input: %v", ErrInvalidInput, err)
	}
	workflowInput, err := s.registry.DecodeInput(definition.ID, encodedInput)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrInvalidInput, err)
	}

	err = handle.Update(ctx, client.ScheduleUpdateOptions{
		DoUpdate: func(input client.ScheduleUpdateInput) (*client.ScheduleUpdate, error) {
			updated := input.Description.Schedule
			updated.Spec = &client.ScheduleSpec{
				CronExpressions: []string{request.Cron},
				TimeZoneName:    request.Timezone,
			}
			updated.Action = &client.ScheduleWorkflowAction{
				Workflow:  definition.ID,
				Args:      []any{workflowInput},
				TaskQueue: s.taskQueue,
				Memo: map[string]any{
					"displayName": request.Name,
				},
			}
			if updated.State == nil {
				updated.State = &client.ScheduleState{}
			}
			updated.State.Note = request.Name
			updated.State.Paused = request.Paused
			return &client.ScheduleUpdate{Schedule: &updated}, nil
		},
	})
	return mapTemporalError(err)
}

func (s *Service) Pause(ctx context.Context, scheduleID string) error {
	return mapTemporalError(s.scheduleClient.GetHandle(ctx, scheduleID).Pause(
		ctx,
		client.SchedulePauseOptions{Note: "paused from web console"},
	))
}

func (s *Service) Resume(ctx context.Context, scheduleID string) error {
	return mapTemporalError(s.scheduleClient.GetHandle(ctx, scheduleID).Unpause(
		ctx,
		client.ScheduleUnpauseOptions{Note: "resumed from web console"},
	))
}

func (s *Service) Delete(ctx context.Context, scheduleID string) error {
	return mapTemporalError(s.scheduleClient.GetHandle(ctx, scheduleID).Delete(ctx))
}

func New(options Options) (*Service, error) {
	if options.Catalog == nil {
		return nil, errors.New("schedules: catalog is required")
	}
	if options.Registry == nil {
		return nil, errors.New("schedules: registry is required")
	}
	if options.ScheduleClient == nil {
		return nil, errors.New("schedules: schedule client is required")
	}
	if options.TaskQueue == "" {
		return nil, errors.New("schedules: task queue is required")
	}
	return &Service{
		catalog:        options.Catalog,
		registry:       options.Registry,
		scheduleClient: options.ScheduleClient,
		taskQueue:      options.TaskQueue,
	}, nil
}

func (s *Service) Create(ctx context.Context, request CreateRequest) (Schedule, error) {
	if request.Name == "" || request.DefinitionID == "" || request.Cron == "" || request.Timezone == "" {
		return Schedule{}, fmt.Errorf("%w: name, workflow, cron and timezone are required", ErrInvalidInput)
	}
	if _, err := cron.ParseStandard(request.Cron); err != nil {
		return Schedule{}, fmt.Errorf("%w: invalid cron expression: %v", ErrInvalidInput, err)
	}
	if _, err := time.LoadLocation(request.Timezone); err != nil {
		return Schedule{}, fmt.Errorf("%w: invalid timezone: %v", ErrInvalidInput, err)
	}

	definition, err := s.catalog.Get(ctx, request.DefinitionID)
	if err != nil {
		if errors.Is(err, catalog.ErrNotFound) {
			return Schedule{}, ErrNotFound
		}
		return Schedule{}, err
	}
	if err := sdk.ValidateInput(definition.InputSchema, request.Input); err != nil {
		return Schedule{}, fmt.Errorf("%w: %v", ErrInvalidInput, err)
	}
	if _, _, ok := s.registry.Workflow(definition.ID); !ok {
		return Schedule{}, ErrNotFound
	}
	encodedInput, err := json.Marshal(request.Input)
	if err != nil {
		return Schedule{}, fmt.Errorf("%w: encode input: %v", ErrInvalidInput, err)
	}
	workflowInput, err := s.registry.DecodeInput(definition.ID, encodedInput)
	if err != nil {
		return Schedule{}, fmt.Errorf("%w: %v", ErrInvalidInput, err)
	}
	suffix, err := randomSuffix()
	if err != nil {
		return Schedule{}, err
	}
	scheduleID := definition.ID + "-" + suffix

	_, err = s.scheduleClient.Create(ctx, client.ScheduleOptions{
		ID: scheduleID,
		Spec: client.ScheduleSpec{
			CronExpressions: []string{request.Cron},
			TimeZoneName:    request.Timezone,
		},
		Action: &client.ScheduleWorkflowAction{
			Workflow:  definition.ID,
			Args:      []any{workflowInput},
			TaskQueue: s.taskQueue,
			Memo: map[string]any{
				"displayName": request.Name,
			},
		},
		Overlap:       enums.SCHEDULE_OVERLAP_POLICY_SKIP,
		CatchupWindow: time.Hour,
		Paused:        request.Paused,
		Note:          request.Name,
		Memo: map[string]any{
			"displayName":  request.Name,
			"cron":         request.Cron,
			"timezone":     request.Timezone,
			"definitionId": definition.ID,
		},
	})
	if err != nil {
		return Schedule{}, mapTemporalError(err)
	}

	return Schedule{
		ID:           scheduleID,
		Name:         request.Name,
		DefinitionID: definition.ID,
		Cron:         request.Cron,
		Timezone:     request.Timezone,
		Paused:       request.Paused,
	}, nil
}

func applyDescription(schedule *Schedule, description *client.ScheduleDescription) {
	if description == nil {
		return
	}
	if spec := description.Schedule.Spec; spec != nil {
		if len(spec.CronExpressions) > 0 {
			schedule.Cron = spec.CronExpressions[0]
		} else if cronExpression := calendarToCron(spec.Calendars); cronExpression != "" {
			schedule.Cron = cronExpression
		}
		if spec.TimeZoneName != "" {
			schedule.Timezone = spec.TimeZoneName
		}
	}
	if state := description.Schedule.State; state != nil {
		schedule.Paused = state.Paused
		if schedule.Name == "" {
			schedule.Name = state.Note
		}
	}
	if len(description.Info.RecentActions) > 0 {
		value := description.Info.RecentActions[len(description.Info.RecentActions)-1].ActualTime
		schedule.LastActionTime = &value
	}
	if len(description.Info.NextActionTimes) > 0 {
		value := description.Info.NextActionTimes[0]
		schedule.NextActionTime = &value
	}
	if name, _ := memoString(description.Memo, "displayName"); name != "" {
		if schedule.Name == "" {
			schedule.Name = name
		}
	}
	if cronExpression, _ := memoString(description.Memo, "cron"); cronExpression != "" {
		if schedule.Cron == "" {
			schedule.Cron = cronExpression
		}
	}
	if timezone, _ := memoString(description.Memo, "timezone"); timezone != "" {
		if schedule.Timezone == "" {
			schedule.Timezone = timezone
		}
	}
	if definitionID, _ := memoString(description.Memo, "definitionId"); definitionID != "" {
		schedule.DefinitionID = definitionID
	}
	if workflowAction, ok := description.Schedule.Action.(*client.ScheduleWorkflowAction); ok {
		if workflowType, ok := workflowAction.Workflow.(string); ok {
			schedule.DefinitionID = workflowType
		}
		if namePayload, ok := workflowAction.Memo["displayName"].(*commonpb.Payload); ok {
			var value string
			if err := converter.GetDefaultDataConverter().FromPayload(namePayload, &value); err == nil && value != "" {
				schedule.Name = value
			}
		}
	}
}

func (s *Service) needsReview(ctx context.Context, description *client.ScheduleDescription) bool {
	if description == nil {
		return true
	}
	action, ok := description.Schedule.Action.(*client.ScheduleWorkflowAction)
	if !ok {
		return true
	}
	definitionID, ok := action.Workflow.(string)
	if !ok {
		return true
	}
	definition, err := s.catalog.Get(ctx, definitionID)
	if err != nil {
		return true
	}
	if len(action.Args) == 0 {
		return true
	}
	payload, ok := action.Args[0].(*commonpb.Payload)
	if !ok {
		return true
	}
	var input any
	if err := converter.GetDefaultDataConverter().FromPayload(payload, &input); err != nil {
		return true
	}
	return sdk.ValidateInput(definition.InputSchema, input) != nil
}

func calendarToCron(calendars []client.ScheduleCalendarSpec) string {
	if len(calendars) != 1 {
		return ""
	}
	calendar := calendars[0]
	second := formatScheduleRanges(calendar.Second, 0, 59)
	minute := formatScheduleRanges(calendar.Minute, 0, 59)
	hour := formatScheduleRanges(calendar.Hour, 0, 23)
	day := formatScheduleRanges(calendar.DayOfMonth, 1, 31)
	month := formatScheduleRanges(calendar.Month, 1, 12)
	weekday := formatScheduleRanges(calendar.DayOfWeek, 0, 6)
	if second != "0" {
		return ""
	}
	return strings.Join([]string{minute, hour, day, month, weekday}, " ")
}

func formatScheduleRanges(ranges []client.ScheduleRange, minValue, maxValue int) string {
	if len(ranges) == 0 {
		return "*"
	}
	parts := make([]string, 0, len(ranges))
	for _, value := range ranges {
		end := value.End
		if end == 0 || end < value.Start {
			end = value.Start
		}
		step := value.Step
		if step <= 0 {
			step = 1
		}
		if value.Start == minValue && end == maxValue && step == 1 {
			return "*"
		}
		part := strconv.Itoa(value.Start)
		if end != value.Start {
			part += "-" + strconv.Itoa(end)
		}
		if step != 1 {
			part += "/" + strconv.Itoa(step)
		}
		parts = append(parts, part)
	}
	return strings.Join(parts, ",")
}

func randomSuffix() (string, error) {
	var bytes [4]byte
	if _, err := rand.Read(bytes[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(bytes[:]), nil
}

func mapTemporalError(err error) error {
	var notFound *serviceerror.NotFound
	if errors.As(err, &notFound) {
		return ErrNotFound
	}
	var unavailable *serviceerror.Unavailable
	if errors.As(err, &unavailable) {
		return ErrTemporalUnavailable
	}
	return err
}

func memoString(memo *commonpb.Memo, field string) (string, error) {
	if memo == nil {
		return "", nil
	}
	payload := memo.GetFields()[field]
	if payload == nil {
		return "", nil
	}
	var value string
	if err := converter.GetDefaultDataConverter().FromPayload(payload, &value); err != nil {
		return "", err
	}
	return value, nil
}
