package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"personal-workflow-platform/app"
	"personal-workflow-platform/platform/approvals"
	"personal-workflow-platform/platform/bootstrap"
	"personal-workflow-platform/platform/catalog"
	"personal-workflow-platform/platform/config"
	"personal-workflow-platform/platform/feishu"
	"personal-workflow-platform/platform/obs"
	"personal-workflow-platform/platform/runs"
	"personal-workflow-platform/platform/runtime"
	"personal-workflow-platform/platform/schedules"
	"personal-workflow-platform/platform/sdk"
	"personal-workflow-platform/platform/webapi"
	"personal-workflow-platform/platform/webui"

	"go.temporal.io/sdk/client"
)

func main() {
	if err := run(); err != nil {
		slog.Error("platform exited", "error", err)
		os.Exit(1)
	}
}

func run() error {
	if len(os.Args) != 2 {
		return errors.New("usage: platform <serve|worker|bootstrap|healthcheck|healthcheck-worker>")
	}

	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}

	logger := obs.NewLogger(cfg.LogLevel)
	slog.SetDefault(logger)

	registry := sdk.NewRegistry()
	app.Register(registry)
	if err := registry.Validate(); err != nil {
		return fmt.Errorf("validate workflow registry: %w", err)
	}

	ctx, stop := signal.NotifyContext(
		context.Background(),
		os.Interrupt,
		syscall.SIGTERM,
	)
	defer stop()

	switch os.Args[1] {
	case "serve":
		return serve(ctx, cfg, registry, logger)
	case "worker":
		if err := runtime.RunWorker(ctx, cfg, registry, logger); err != nil {
			return fmt.Errorf("run worker: %w", err)
		}
		return nil
	case "bootstrap":
		if err := bootstrap.Run(ctx, cfg, logger); err != nil {
			return fmt.Errorf("bootstrap Temporal: %w", err)
		}
		return nil
	case "healthcheck":
		return runHealthcheck(cfg.HTTPAddr)
	case "healthcheck-worker":
		return runHealthcheck(cfg.WorkerHealthAddr)
	default:
		return fmt.Errorf("unknown command %q; usage: platform <serve|worker|bootstrap|healthcheck|healthcheck-worker>", os.Args[1])
	}
}

func runHealthcheck(address string) error {
	host := address
	if strings.HasPrefix(host, ":") {
		host = "127.0.0.1" + host
	}
	client := http.Client{Timeout: 3 * time.Second}
	response, err := client.Get("http://" + host + "/healthz")
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return fmt.Errorf("healthcheck returned HTTP %d", response.StatusCode)
	}
	return nil
}

func serve(
	ctx context.Context,
	cfg config.Config,
	registry *sdk.Registry,
	logger *slog.Logger,
) error {
	catalogView, err := catalog.New(registry)
	if err != nil {
		return fmt.Errorf("create catalog: %w", err)
	}
	temporalClient, err := client.NewLazyClient(client.Options{
		HostPort:  cfg.TemporalAddress,
		Namespace: cfg.TemporalNamespace,
	})
	if err != nil {
		return fmt.Errorf("create Temporal client: %w", err)
	}
	defer temporalClient.Close()

	temporalBackend := runs.NewTemporalStarter(temporalClient)
	runService, err := runs.New(runs.Options{
		Catalog:       catalogView,
		Registry:      registry,
		Starter:       temporalBackend,
		Inspector:     temporalBackend,
		TaskQueue:     cfg.TaskQueue,
		Namespace:     cfg.TemporalNamespace,
		TemporalUIURL: cfg.TemporalUIURL,
	})
	if err != nil {
		return fmt.Errorf("create runs service: %w", err)
	}
	approvalService := approvals.New(temporalBackend)
	if cfg.Feishu.Enabled() {
		feishuRuntime := feishu.NewRuntime(cfg.Feishu, logger)
		go func() {
			if err := feishuRuntime.Start(ctx, approvalService); err != nil && ctx.Err() == nil {
				logger.Error("Feishu long connection stopped", "error", err)
			}
		}()
	}
	scheduleService, err := schedules.New(schedules.Options{
		Catalog:        catalogView,
		Registry:       registry,
		ScheduleClient: temporalClient.ScheduleClient(),
		TaskQueue:      cfg.TaskQueue,
	})
	if err != nil {
		return fmt.Errorf("create schedules service: %w", err)
	}
	handler, err := webapi.New(webapi.Options{
		Catalog:      catalogView,
		Runs:         runService,
		RunReader:    runService,
		Approvals:    approvalService,
		Schedules:    scheduleService,
		AllowedHosts: cfg.AllowedHosts,
		Assets:       webui.FS(),
	})
	if err != nil {
		return fmt.Errorf("create web API: %w", err)
	}

	server := &http.Server{
		Addr:              cfg.HTTPAddr,
		Handler:           handler,
		ReadHeaderTimeout: 5 * time.Second,
	}

	serverError := make(chan error, 1)
	go func() {
		logger.Info("starting platform server", "address", cfg.HTTPAddr)
		serverError <- server.ListenAndServe()
	}()

	select {
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := server.Shutdown(shutdownCtx); err != nil {
			return fmt.Errorf("shutdown HTTP server: %w", err)
		}
		return nil
	case err := <-serverError:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	}
}
