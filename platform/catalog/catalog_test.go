package catalog_test

import (
	"context"
	"errors"
	"testing"

	"personal-workflow-platform/platform/catalog"
	"personal-workflow-platform/platform/sdk"

	"go.temporal.io/sdk/workflow"
)

type testInput struct {
	Name string `json:"name"`
}

func testWorkflow(ctx workflow.Context, in testInput) (string, error) {
	return in.Name, nil
}

func TestCatalogListsPublicWorkflowsInStableOrder(t *testing.T) {
	registry := sdk.NewRegistry()
	sdk.Workflow(registry, sdk.WorkflowDef[testInput, string]{
		ID:          "zeta",
		DisplayName: "Zeta",
		Fn:          testWorkflow,
		Example:     testInput{Name: "z"},
	})
	sdk.Workflow(registry, sdk.WorkflowDef[testInput, string]{
		ID:          "alpha",
		DisplayName: "Alpha",
		Fn:          testWorkflow,
		Example:     testInput{Name: "a"},
	})
	sdk.Workflow(registry, sdk.WorkflowDef[testInput, string]{
		ID:          "alpha.child",
		DisplayName: "Internal",
		Fn:          testWorkflow,
		Example:     testInput{Name: "child"},
		Internal:    true,
	})

	catalogView, err := catalog.New(registry)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	definitions, err := catalogView.List(context.Background())
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}
	if len(definitions) != 2 {
		t.Fatalf("List() length = %d, want 2", len(definitions))
	}
	if definitions[0].ID != "alpha" || definitions[1].ID != "zeta" {
		t.Fatalf("List() IDs = [%s, %s], want [alpha, zeta]", definitions[0].ID, definitions[1].ID)
	}
}

func TestCatalogHidesInternalAndReturnsNotFound(t *testing.T) {
	registry := sdk.NewRegistry()
	sdk.Workflow(registry, sdk.WorkflowDef[testInput, string]{
		ID:          "internal.only",
		DisplayName: "Internal",
		Fn:          testWorkflow,
		Example:     testInput{Name: "x"},
		Internal:    true,
	})

	catalogView, err := catalog.New(registry)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	_, err = catalogView.Get(context.Background(), "internal.only")
	if !errors.Is(err, catalog.ErrNotFound) {
		t.Fatalf("Get() error = %v, want ErrNotFound", err)
	}
}
