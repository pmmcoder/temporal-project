package catalog

import (
	"context"
	"errors"
	"sort"

	"personal-workflow-platform/platform/sdk"
)

var ErrNotFound = errors.New("workflow definition not found")

type Definition = sdk.WorkflowInfo

type Catalog struct {
	definitions []Definition
	byID        map[string]Definition
}

func New(registry *sdk.Registry) (*Catalog, error) {
	if err := registry.Validate(); err != nil {
		return nil, err
	}

	definitions := make([]Definition, 0)
	byID := make(map[string]Definition)
	for _, definition := range registry.Workflows() {
		if definition.Internal {
			continue
		}
		definitions = append(definitions, definition)
		byID[definition.ID] = definition
	}
	sort.Slice(definitions, func(i, j int) bool {
		return definitions[i].ID < definitions[j].ID
	})

	return &Catalog{
		definitions: definitions,
		byID:        byID,
	}, nil
}

func (c *Catalog) List(_ context.Context) ([]Definition, error) {
	result := make([]Definition, len(c.definitions))
	copy(result, c.definitions)
	return result, nil
}

func (c *Catalog) Get(_ context.Context, id string) (Definition, error) {
	definition, ok := c.byID[id]
	if !ok {
		return Definition{}, ErrNotFound
	}
	return definition, nil
}
