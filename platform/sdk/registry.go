package sdk

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"time"

	"go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/workflow"
)

const (
	reservedPrefix      = "platform."
	ErrTypeInvalidInput = "InvalidInput"
)

type WorkflowDef[In, Out any] struct {
	ID          string
	DisplayName string
	Description string
	Fn          func(workflow.Context, In) (Out, error)
	Example     any
	Signals     []SignalDef
	Internal    bool
}

type SignalDef struct {
	Name        string
	InputSchema json.RawMessage
	inputType   reflect.Type
}

func Signal[In any](name string) SignalDef {
	schema, err := marshalSchemaForType(reflect.TypeFor[In]())
	if err != nil {
		panic(fmt.Sprintf("generate schema for signal %q: %v", name, err))
	}
	return SignalDef{Name: name, InputSchema: schema, inputType: reflect.TypeFor[In]()}
}

type ActivityDef struct {
	Name    string
	Fn      any
	Options *workflow.ActivityOptions
}

type WorkflowInfo struct {
	ID           string
	DisplayName  string
	Description  string
	InputSchema  json.RawMessage
	ExampleInput json.RawMessage
	Signals      []SignalDef
	Internal     bool
}

type workflowEntry struct {
	info       WorkflowInfo
	fn         any
	input      reflect.Type
	example    json.RawMessage
	exampleErr error
	schemaErr  error
}

type activityEntry struct {
	def ActivityDef
}

type Registry struct {
	workflows  []workflowEntry
	activities []activityEntry
}

func NewRegistry() *Registry {
	return &Registry{}
}

func Workflow[In, Out any](registry *Registry, definition WorkflowDef[In, Out]) {
	if registry == nil {
		panic("sdk: nil registry")
	}

	inputType := reflect.TypeFor[In]()
	schema, schemaErr := marshalSchemaForType(inputType)
	example, exampleErr := json.Marshal(definition.Example)

	registry.workflows = append(registry.workflows, workflowEntry{
		info: WorkflowInfo{
			ID:           definition.ID,
			DisplayName:  definition.DisplayName,
			Description:  definition.Description,
			InputSchema:  schema,
			ExampleInput: example,
			Signals:      definition.Signals,
			Internal:     definition.Internal,
		},
		fn:         definition.Fn,
		input:      inputType,
		example:    example,
		exampleErr: exampleErr,
		schemaErr:  schemaErr,
	})
}

func Activity(registry *Registry, definition ActivityDef) {
	if registry == nil {
		panic("sdk: nil registry")
	}
	registry.activities = append(registry.activities, activityEntry{def: definition})
}

func (r *Registry) Validate() error {
	var errs []error
	workflowNames := make(map[string]struct{}, len(r.workflows))
	activityNames := make(map[string]struct{}, len(r.activities))

	for _, entry := range r.workflows {
		info := entry.info
		if info.ID == "" {
			errs = append(errs, errors.New("workflow ID is required"))
			continue
		}
		if strings.HasPrefix(info.ID, reservedPrefix) {
			errs = append(errs, fmt.Errorf("workflow ID %q uses reserved prefix", info.ID))
		}
		if _, exists := workflowNames[info.ID]; exists {
			errs = append(errs, fmt.Errorf("workflow ID %q is already registered", info.ID))
		} else {
			workflowNames[info.ID] = struct{}{}
		}
		if info.DisplayName == "" {
			errs = append(errs, fmt.Errorf("workflow %q display name is required", info.ID))
		}
		if entry.fn == nil {
			errs = append(errs, fmt.Errorf("workflow %q function is required", info.ID))
		}
		if entry.input.Kind() != reflect.Struct {
			errs = append(errs, fmt.Errorf("workflow %q input must be a struct", info.ID))
		}
		if entry.schemaErr != nil {
			errs = append(errs, fmt.Errorf("workflow %q input schema cannot be generated: %w", info.ID, entry.schemaErr))
		} else if len(info.InputSchema) == 0 {
			errs = append(errs, fmt.Errorf("workflow %q input schema is required", info.ID))
		} else if entry.exampleErr != nil {
			errs = append(errs, fmt.Errorf("workflow %q example cannot be encoded: %w", info.ID, entry.exampleErr))
		} else if err := validateJSONAgainstSchema(entry.example, info.InputSchema); err != nil {
			errs = append(errs, fmt.Errorf("workflow %q example does not match input schema: %w", info.ID, err))
		}
		if err := validateSignals(info.Signals); err != nil {
			errs = append(errs, fmt.Errorf("workflow %q: %w", info.ID, err))
		}
	}

	for _, entry := range r.activities {
		definition := entry.def
		if definition.Name == "" {
			errs = append(errs, errors.New("activity name is required"))
			continue
		}
		if strings.HasPrefix(definition.Name, reservedPrefix) {
			errs = append(errs, fmt.Errorf("activity %q uses reserved prefix", definition.Name))
		}
		if _, exists := activityNames[definition.Name]; exists {
			errs = append(errs, fmt.Errorf("activity %q is already registered", definition.Name))
		} else {
			activityNames[definition.Name] = struct{}{}
		}
		if err := validateActivitySignature(definition.Fn); err != nil {
			errs = append(errs, fmt.Errorf("activity %q: %w", definition.Name, err))
		}
	}

	return errors.Join(errs...)
}

func (r *Registry) Workflows() []WorkflowInfo {
	result := make([]WorkflowInfo, 0, len(r.workflows))
	for _, entry := range r.workflows {
		result = append(result, entry.info)
	}
	return result
}

func (r *Registry) Workflow(id string) (any, WorkflowInfo, bool) {
	for _, entry := range r.workflows {
		if entry.info.ID == id {
			return entry.fn, entry.info, true
		}
	}
	return nil, WorkflowInfo{}, false
}

func (r *Registry) DecodeInput(id string, encoded json.RawMessage) (any, error) {
	for _, entry := range r.workflows {
		if entry.info.ID != id {
			continue
		}
		input := reflect.New(entry.input)
		if err := json.Unmarshal(encoded, input.Interface()); err != nil {
			return nil, fmt.Errorf("decode workflow %q input: %w", id, err)
		}
		return input.Elem().Interface(), nil
	}
	return nil, fmt.Errorf("decode workflow %q input: workflow is not registered", id)
}

func (r *Registry) DecodeSignal(
	workflowID string,
	signalName string,
	encoded json.RawMessage,
) (any, error) {
	for _, entry := range r.workflows {
		if entry.info.ID != workflowID {
			continue
		}
		for _, signal := range entry.info.Signals {
			if signal.Name != signalName {
				continue
			}
			input := reflect.New(signal.inputType)
			if err := json.Unmarshal(encoded, input.Interface()); err != nil {
				return nil, fmt.Errorf("decode signal %q input: %w", signalName, err)
			}
			return input.Elem().Interface(), nil
		}
		return nil, fmt.Errorf("decode signal %q input: signal is not registered", signalName)
	}
	return nil, fmt.Errorf("decode signal %q input: workflow is not registered", signalName)
}

func (r *Registry) Activities() []ActivityDef {
	result := make([]ActivityDef, 0, len(r.activities))
	for _, entry := range r.activities {
		result = append(result, entry.def)
	}
	return result
}

func DefaultActivityOptions() workflow.ActivityOptions {
	return workflow.ActivityOptions{
		StartToCloseTimeout: 30 * time.Second,
		RetryPolicy: &temporal.RetryPolicy{
			InitialInterval:        time.Second,
			BackoffCoefficient:     2,
			MaximumInterval:        60 * time.Second,
			MaximumAttempts:        5,
			NonRetryableErrorTypes: []string{ErrTypeInvalidInput},
		},
	}
}

func InvalidInput(err error) error {
	if err == nil {
		return nil
	}
	return temporal.NewNonRetryableApplicationError(
		err.Error(),
		ErrTypeInvalidInput,
		err,
	)
}

func validateSignals(signals []SignalDef) error {
	seen := make(map[string]struct{}, len(signals))
	for _, signal := range signals {
		if signal.Name == "" {
			return errors.New("signal name is required")
		}
		if strings.HasPrefix(signal.Name, reservedPrefix) {
			return fmt.Errorf("signal %q uses reserved prefix", signal.Name)
		}
		if _, exists := seen[signal.Name]; exists {
			return fmt.Errorf("signal %q is already registered", signal.Name)
		}
		seen[signal.Name] = struct{}{}
	}
	return nil
}

func validateActivitySignature(fn any) error {
	if fn == nil {
		return errors.New("function is required")
	}

	fnType := reflect.TypeOf(fn)
	if fnType.Kind() != reflect.Func {
		return errors.New("must be a function")
	}
	if fnType.NumIn() == 0 || fnType.In(0) != reflect.TypeFor[context.Context]() {
		return errors.New("activity signature must start with context.Context")
	}
	if fnType.NumOut() < 1 || !fnType.Out(fnType.NumOut()-1).Implements(reflect.TypeFor[error]()) {
		return errors.New("activity signature must return error")
	}
	return nil
}
