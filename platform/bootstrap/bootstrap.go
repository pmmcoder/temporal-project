package bootstrap

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"personal-workflow-platform/platform/config"

	"go.temporal.io/api/enums/v1"
	namespacepb "go.temporal.io/api/namespace/v1"
	operatorservicepb "go.temporal.io/api/operatorservice/v1"
	"go.temporal.io/api/serviceerror"
	"go.temporal.io/api/workflowservice/v1"
	"go.temporal.io/sdk/client"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/durationpb"
)

const (
	retentionPeriod      = 90 * 24 * time.Hour
	pendingApprovalField = "PlatformPendingApproval"
)

func Run(ctx context.Context, cfg config.Config, logger *slog.Logger) error {
	options := client.Options{HostPort: cfg.TemporalAddress}
	namespaceClient, err := connectNamespaceClient(ctx, options, logger)
	if err != nil {
		return err
	}
	defer namespaceClient.Close()

	if err := ensureNamespace(ctx, namespaceClient, cfg.TemporalNamespace); err != nil {
		return err
	}

	options.Namespace = cfg.TemporalNamespace
	workflowClient, err := connectClient(ctx, options, logger)
	if err != nil {
		return err
	}
	defer workflowClient.Close()

	if err := ensureSearchAttribute(ctx, workflowClient, cfg.TemporalNamespace); err != nil {
		return err
	}

	logger.Info(
		"Temporal bootstrap completed",
		"namespace", cfg.TemporalNamespace,
		"retention", retentionPeriod.String(),
	)
	return nil
}

type namespaceManager interface {
	Describe(context.Context, string) (*workflowservice.DescribeNamespaceResponse, error)
	Register(context.Context, *workflowservice.RegisterNamespaceRequest) error
	Update(context.Context, *workflowservice.UpdateNamespaceRequest) error
	Close()
}

func ensureNamespace(ctx context.Context, manager namespaceManager, namespace string) error {
	description, err := manager.Describe(ctx, namespace)
	if err == nil {
		ttl := description.GetConfig().GetWorkflowExecutionRetentionTtl().AsDuration()
		if ttl == retentionPeriod {
			return nil
		}
		if err := manager.Update(ctx, &workflowservice.UpdateNamespaceRequest{
			Namespace: namespace,
			Config: &namespacepb.NamespaceConfig{
				WorkflowExecutionRetentionTtl: durationpb.New(retentionPeriod),
			},
		}); err != nil {
			return fmt.Errorf("update namespace %q retention: %w", namespace, err)
		}
		return nil
	}

	var notFound *serviceerror.NamespaceNotFound
	if !errors.As(err, &notFound) {
		return fmt.Errorf("describe namespace %q: %w", namespace, err)
	}
	if err := manager.Register(ctx, &workflowservice.RegisterNamespaceRequest{
		Namespace:                        namespace,
		Description:                      "Personal workflow platform",
		WorkflowExecutionRetentionPeriod: durationpb.New(retentionPeriod),
	}); err != nil {
		return fmt.Errorf("register namespace %q: %w", namespace, err)
	}
	return nil
}

func ensureSearchAttribute(ctx context.Context, temporalClient client.Client, namespace string) error {
	searchAttributes, err := temporalClient.GetSearchAttributes(ctx)
	if err != nil {
		return fmt.Errorf("get search attributes: %w", err)
	}
	if _, exists := searchAttributes.GetKeys()[pendingApprovalField]; exists {
		return nil
	}

	_, err = temporalClient.OperatorService().AddSearchAttributes(
		ctx,
		&operatorservicepb.AddSearchAttributesRequest{
			Namespace: namespace,
			SearchAttributes: map[string]enums.IndexedValueType{
				pendingApprovalField: enums.INDEXED_VALUE_TYPE_BOOL,
			},
		},
	)
	if err != nil && status.Code(err) != codes.AlreadyExists {
		return fmt.Errorf("register search attribute %q: %w", pendingApprovalField, err)
	}
	return nil
}

func connectNamespaceClient(
	ctx context.Context,
	options client.Options,
	logger *slog.Logger,
) (client.NamespaceClient, error) {
	for {
		namespaceClient, err := client.NewNamespaceClient(options)
		if err == nil {
			return namespaceClient, nil
		}
		logger.Info("waiting for Temporal server", "address", options.HostPort)
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(time.Second):
		}
	}
}

func connectClient(
	ctx context.Context,
	options client.Options,
	logger *slog.Logger,
) (client.Client, error) {
	for {
		temporalClient, err := client.Dial(options)
		if err == nil {
			return temporalClient, nil
		}
		logger.Info("waiting for Temporal namespace", "namespace", options.Namespace)
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(time.Second):
		}
	}
}
