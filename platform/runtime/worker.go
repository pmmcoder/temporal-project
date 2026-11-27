package runtime

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"sync/atomic"
	"time"

	"personal-workflow-platform/platform/config"
	"personal-workflow-platform/platform/feishu"
	"personal-workflow-platform/platform/sdk"
	"personal-workflow-platform/platform/sdk/approval"

	"go.temporal.io/sdk/client"
	"go.temporal.io/sdk/interceptor"
	"go.temporal.io/sdk/worker"
)

func RunWorker(ctx context.Context, cfg config.Config, registry *sdk.Registry, logger *slog.Logger) error {
	clientInstance, err := client.Dial(client.Options{
		HostPort:  cfg.TemporalAddress,
		Namespace: cfg.TemporalNamespace,
	})
	if err != nil {
		return err
	}
	defer clientInstance.Close()

	temporalWorker := worker.New(clientInstance, cfg.TaskQueue, worker.Options{
		WorkerStopTimeout: 30 * time.Second,
		Interceptors: []interceptor.WorkerInterceptor{
			approval.NewWorkerInterceptor(approval.WorkerOptions{
				FeishuEnabled: cfg.Feishu.Enabled(),
			}),
		},
	})
	if err := Register(temporalWorker, registry, feishu.NewRuntime(cfg.Feishu, logger)); err != nil {
		return err
	}

	var running atomic.Bool
	running.Store(true)
	defer running.Store(false)

	healthServer := &http.Server{
		Addr:              cfg.WorkerHealthAddr,
		ReadHeaderTimeout: 3 * time.Second,
		Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if !running.Load() {
				http.Error(w, "worker is not running", http.StatusServiceUnavailable)
				return
			}
			checkCtx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
			defer cancel()
			if _, err := clientInstance.CheckHealth(checkCtx, nil); err != nil {
				http.Error(w, "Temporal is unavailable", http.StatusServiceUnavailable)
				return
			}
			w.Header().Set("Content-Type", "application/json; charset=utf-8")
			_, _ = w.Write([]byte(`{"status":"ok"}`))
		}),
	}
	healthErr := make(chan error, 1)
	go func() {
		logger.Info("starting worker health server", "address", cfg.WorkerHealthAddr)
		healthErr <- healthServer.ListenAndServe()
	}()
	defer func() {
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		if err := healthServer.Shutdown(shutdownCtx); err != nil {
			logger.Warn("shutdown worker health server", "error", err)
		}
	}()

	logger.Info(
		"starting workflow worker",
		"namespace", cfg.TemporalNamespace,
		"task_queue", cfg.TaskQueue,
	)

	stop := make(chan interface{}, 1)
	go func() {
		<-ctx.Done()
		stop <- struct{}{}
	}()

	workerErr := make(chan error, 1)
	go func() {
		workerErr <- temporalWorker.Run(stop)
	}()

	select {
	case err := <-workerErr:
		return err
	case err := <-healthErr:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		stop <- struct{}{}
		<-workerErr
		return err
	case <-ctx.Done():
		stop <- struct{}{}
		return <-workerErr
	}
}
