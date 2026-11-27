package runs

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"

	"personal-workflow-platform/platform/catalog"
	"personal-workflow-platform/platform/sdk"
)

var (
	ErrNotFound            = errors.New("workflow definition not found")
	ErrInvalidInput        = errors.New("invalid workflow input")
	ErrInputConflict       = errors.New("idempotency key was used with different input")
	ErrRunNotFound         = errors.New("workflow run not found")
	ErrInvalidState        = errors.New("workflow run is not in a valid state")
	ErrTemporalUnavailable = errors.New("Temporal is unavailable")
	ErrSignalNotDefined    = errors.New("signal is not declared by workflow")
)

var idempotencyKeyPattern = regexp.MustCompile(`^[A-Za-z0-9_-]{1,64}$`)

type Catalog interface {
	Get(context.Context, string) (catalog.Definition, error)
}

type Registry interface {
	Workflow(string) (any, sdk.WorkflowInfo, bool)
	DecodeInput(string, json.RawMessage) (any, error)
	DecodeSignal(string, string, json.RawMessage) (any, error)
}

type Starter interface {
	StartWorkflow(context.Context, StartWorkflowRequest) (StartWorkflowResult, error)
}

type Options struct {
	Catalog       Catalog
	Registry      Registry
	Starter       Starter
	Inspector     Inspector
	TaskQueue     string
	Namespace     string
	TemporalUIURL string
}

type Service struct {
	catalog       Catalog
	registry      Registry
	starter       Starter
	inspector     Inspector
	taskQueue     string
	namespace     string
	temporalUIURL string
}

type StartRequest struct {
	DefinitionID   string
	Input          any
	IdempotencyKey string
}

type StartWorkflowRequest struct {
	DefinitionID string
	WorkflowID   string
	TaskQueue    string
	WorkflowType string
	Input        any
	InputHash    string
}

type StartWorkflowResult struct {
	WorkflowID        string
	RunID             string
	Existing          bool
	ExistingInputHash string
}

type Run struct {
	WorkflowID string `json:"workflowId"`
	RunID      string `json:"runId"`
	Existing   bool   `json:"existing"`
}

func New(options Options) (*Service, error) {
	if options.Catalog == nil {
		return nil, errors.New("runs: catalog is required")
	}
	if options.Registry == nil {
		return nil, errors.New("runs: registry is required")
	}
	if options.Starter == nil {
		return nil, errors.New("runs: starter is required")
	}
	if options.TaskQueue == "" {
		return nil, errors.New("runs: task queue is required")
	}

	namespace := options.Namespace
	if namespace == "" {
		namespace = "default"
	}

	return &Service{
		catalog:       options.Catalog,
		registry:      options.Registry,
		starter:       options.Starter,
		inspector:     options.Inspector,
		taskQueue:     options.TaskQueue,
		namespace:     namespace,
		temporalUIURL: strings.TrimRight(options.TemporalUIURL, "/"),
	}, nil
}

func (s *Service) Start(ctx context.Context, request StartRequest) (Run, error) {
	definition, err := s.catalog.Get(ctx, request.DefinitionID)
	if err != nil {
		if errors.Is(err, catalog.ErrNotFound) {
			return Run{}, ErrNotFound
		}
		return Run{}, err
	}

	if err := sdk.ValidateInput(definition.InputSchema, request.Input); err != nil {
		return Run{}, fmt.Errorf("%w: %v", ErrInvalidInput, err)
	}

	idempotencyKey := request.IdempotencyKey
	if idempotencyKey == "" {
		idempotencyKey, err = newIdempotencyKey()
		if err != nil {
			return Run{}, fmt.Errorf("generate idempotency key: %w", err)
		}
	} else if !idempotencyKeyPattern.MatchString(idempotencyKey) {
		return Run{}, fmt.Errorf("%w: idempotency key must match [A-Za-z0-9_-]{1,64}", ErrInvalidInput)
	}

	canonicalInput, inputHash, err := canonicalizeInput(request.Input)
	if err != nil {
		return Run{}, fmt.Errorf("%w: %v", ErrInvalidInput, err)
	}

	_, workflowInfo, ok := s.registry.Workflow(definition.ID)
	if !ok {
		return Run{}, ErrNotFound
	}
	workflowInput, err := s.registry.DecodeInput(definition.ID, canonicalInput)
	if err != nil {
		return Run{}, fmt.Errorf("%w: %v", ErrInvalidInput, err)
	}
	workflowID := definition.ID + ":" + idempotencyKey

	result, err := s.starter.StartWorkflow(ctx, StartWorkflowRequest{
		DefinitionID: definition.ID,
		WorkflowID:   workflowID,
		TaskQueue:    s.taskQueue,
		WorkflowType: workflowInfo.ID,
		Input:        workflowInput,
		InputHash:    inputHash,
	})
	if err != nil {
		return Run{}, err
	}
	if result.Existing && result.ExistingInputHash != inputHash {
		return Run{}, ErrInputConflict
	}
	if result.WorkflowID == "" {
		result.WorkflowID = workflowID
	}

	return Run{
		WorkflowID: result.WorkflowID,
		RunID:      result.RunID,
		Existing:   result.Existing,
	}, nil
}

func canonicalizeInput(input any) (json.RawMessage, string, error) {
	encoded, err := json.Marshal(input)
	if err != nil {
		return nil, "", fmt.Errorf("encode input: %w", err)
	}

	var normalized any
	decoder := json.NewDecoder(bytes.NewReader(encoded))
	decoder.UseNumber()
	if err := decoder.Decode(&normalized); err != nil {
		return nil, "", fmt.Errorf("decode input: %w", err)
	}

	canonical, err := json.Marshal(normalized)
	if err != nil {
		return nil, "", fmt.Errorf("canonicalize input: %w", err)
	}
	sum := sha256.Sum256(canonical)
	return canonical, hex.EncodeToString(sum[:]), nil
}

func newIdempotencyKey() (string, error) {
	var bytes [16]byte
	if _, err := rand.Read(bytes[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(bytes[:]), nil
}
