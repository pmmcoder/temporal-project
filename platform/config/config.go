package config

import (
	"errors"
	"fmt"
	"os"
	"strings"
)

const (
	DefaultTemporalAddress  = "127.0.0.1:7233"
	DefaultNamespace        = "default"
	DefaultTaskQueue        = "personal-workflows"
	DefaultTemporalUIURL    = "http://127.0.0.1:8233"
	DefaultHTTPAddr         = "127.0.0.1:8080"
	DefaultWorkerHealthAddr = "127.0.0.1:8081"
	DefaultLogLevel         = "info"
)

type Config struct {
	TemporalAddress   string
	TemporalNamespace string
	TaskQueue         string
	TemporalUIURL     string
	HTTPAddr          string
	AllowedHosts      []string
	WorkerHealthAddr  string
	LogLevel          string
	Feishu            FeishuConfig
}

type FeishuConfig struct {
	AppID           string
	AppSecret       string
	ApproverOpenIDs []string
	DefaultChatID   string
}

func (f FeishuConfig) Enabled() bool {
	return f.AppID != "" || f.AppSecret != ""
}

func Load() (Config, error) {
	cfg := Config{
		TemporalAddress:   envOrDefault("TEMPORAL_ADDRESS", DefaultTemporalAddress),
		TemporalNamespace: envOrDefault("TEMPORAL_NAMESPACE", DefaultNamespace),
		TaskQueue:         envOrDefault("TEMPORAL_TASK_QUEUE", DefaultTaskQueue),
		TemporalUIURL:     envOrDefault("TEMPORAL_UI_URL", DefaultTemporalUIURL),
		HTTPAddr:          envOrDefault("HTTP_ADDR", DefaultHTTPAddr),
		WorkerHealthAddr:  envOrDefault("WORKER_HEALTH_ADDR", DefaultWorkerHealthAddr),
		LogLevel:          envOrDefault("LOG_LEVEL", DefaultLogLevel),
		AllowedHosts: splitList(envOrDefault(
			"HTTP_ALLOWED_HOSTS",
			"127.0.0.1:8080,localhost:8080",
		)),
		Feishu: FeishuConfig{
			AppID:           strings.TrimSpace(os.Getenv("FEISHU_APP_ID")),
			AppSecret:       strings.TrimSpace(os.Getenv("FEISHU_APP_SECRET")),
			ApproverOpenIDs: splitList(os.Getenv("FEISHU_APPROVER_OPEN_IDS")),
			DefaultChatID:   strings.TrimSpace(os.Getenv("FEISHU_DEFAULT_CHAT_ID")),
		},
	}

	if err := cfg.Validate(); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

func (c Config) Validate() error {
	var errs []error

	if (c.Feishu.AppID == "") != (c.Feishu.AppSecret == "") {
		errs = append(errs, errors.New("FEISHU_APP_ID and FEISHU_APP_SECRET must both be set or both be empty"))
	}
	if c.Feishu.Enabled() && len(c.Feishu.ApproverOpenIDs) == 0 {
		errs = append(errs, errors.New("FEISHU_APPROVER_OPEN_IDS is required when Feishu is enabled"))
	}
	if c.Feishu.Enabled() && c.Feishu.DefaultChatID == "" {
		errs = append(errs, errors.New("FEISHU_DEFAULT_CHAT_ID is required when Feishu is enabled"))
	}

	return errors.Join(errs...)
}

func envOrDefault(name, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(name)); value != "" {
		return value
	}
	return fallback
}

func splitList(value string) []string {
	if strings.TrimSpace(value) == "" {
		return nil
	}

	parts := strings.Split(value, ",")
	result := make([]string, 0, len(parts))
	for _, part := range parts {
		if item := strings.TrimSpace(part); item != "" {
			result = append(result, item)
		}
	}
	return result
}

func (c Config) String() string {
	return fmt.Sprintf(
		"temporal=%s namespace=%s queue=%s http=%s feishu=%t",
		c.TemporalAddress,
		c.TemporalNamespace,
		c.TaskQueue,
		c.HTTPAddr,
		c.Feishu.Enabled(),
	)
}
