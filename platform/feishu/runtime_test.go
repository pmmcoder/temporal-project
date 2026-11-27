package feishu_test

import (
	"context"
	"errors"
	"log/slog"
	"testing"
	"time"

	"personal-workflow-platform/platform/config"
	"personal-workflow-platform/platform/feishu"
	"personal-workflow-platform/platform/sdk/approval"
	"personal-workflow-platform/platform/sdk/notify"
)

type fakeMessages struct {
	chatID   string
	content  string
	uuid     string
	patched  string
	messageID string
}

func (m *fakeMessages) Create(
	_ context.Context,
	chatID string,
	content string,
	uuid string,
) (string, error) {
	m.chatID = chatID
	m.content = content
	m.uuid = uuid
	return m.messageID, nil
}

func (m *fakeMessages) Patch(_ context.Context, messageID string, content string) error {
	m.patched = messageID + ":" + content
	return nil
}

func enabledConfig() config.FeishuConfig {
	return config.FeishuConfig{
		AppID:           "cli_test",
		AppSecret:       "secret",
		ApproverOpenIDs: []string{"ou_1"},
		DefaultChatID:   "oc_test",
	}
}

func TestSendApprovalCardReturnsMessageID(t *testing.T) {
	messages := &fakeMessages{messageID: "om_1"}
	runtime := feishu.NewRuntimeWithMessages(
		enabledConfig(),
		slog.Default(),
		messages,
	)
	messageID, err := runtime.SendApprovalCard(context.Background(), feishu.ApprovalCardRequest{
		WorkflowID: "approvaldemo:key",
		RunID:      "run-1",
		Approval: approval.Req{
			Key:          "publish",
			Title:        "发布",
			Summary:      "确认",
			AllowComment: true,
			Timeout:      time.Hour,
			OnTimeout:    approval.Reject,
		},
	})
	if err != nil {
		t.Fatalf("SendApprovalCard() error = %v", err)
	}
	if messageID != "om_1" {
		t.Errorf("message ID = %q", messageID)
	}
	if messages.chatID != "oc_test" || messages.uuid == "" {
		t.Errorf("message request = %#v", messages)
	}
}

func TestSendMessageWithoutConfigurationFails(t *testing.T) {
	runtime := feishu.NewRuntime(config.FeishuConfig{}, slog.Default())
	err := runtime.SendMessage(context.Background(), notify.Msg{Markdown: "test"})
	if !errors.Is(err, feishu.ErrNotConfigured) {
		t.Fatalf("SendMessage() error = %v, want ErrNotConfigured", err)
	}
}
