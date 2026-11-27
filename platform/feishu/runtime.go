package feishu

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"

	"personal-workflow-platform/platform/config"
	"personal-workflow-platform/platform/sdk/approval"
	"personal-workflow-platform/platform/sdk/notify"

	lark "github.com/larksuite/oapi-sdk-go/v3"
	larkim "github.com/larksuite/oapi-sdk-go/v3/service/im/v1"
	"go.temporal.io/sdk/temporal"
)

var ErrNotConfigured = errors.New("Feishu is not configured")

type MessageClient interface {
	Create(context.Context, string, string, string) (string, error)
	Patch(context.Context, string, string) error
}

type Runtime struct {
	config   config.FeishuConfig
	logger   *slog.Logger
	messages MessageClient
}

type UpdateApprovalCardRequest = approval.UpdateCardRequest

func NewRuntime(feishuConfig config.FeishuConfig, logger *slog.Logger) *Runtime {
	if !feishuConfig.Enabled() {
		return NewRuntimeWithMessages(feishuConfig, logger, nil)
	}
	apiClient := lark.NewClient(feishuConfig.AppID, feishuConfig.AppSecret)
	return NewRuntimeWithMessages(feishuConfig, logger, &sdkMessages{client: apiClient})
}

func NewRuntimeWithMessages(
	feishuConfig config.FeishuConfig,
	logger *slog.Logger,
	messages MessageClient,
) *Runtime {
	return &Runtime{
		config:   feishuConfig,
		logger:   logger,
		messages: messages,
	}
}

func (r *Runtime) SendMessage(_ context.Context, message notify.Msg) error {
	if !r.config.Enabled() || r.messages == nil {
		return ErrNotConfigured
	}
	card := map[string]any{
		"schema": "2.0",
		"config": map[string]any{"update_multi": true},
		"body": map[string]any{
			"elements": []any{
				map[string]any{"tag": "markdown", "content": message.Markdown},
			},
		},
	}
	content, err := json.Marshal(card)
	if err != nil {
		return err
	}
	chatID := message.ChatID
	if chatID == "" {
		chatID = r.config.DefaultChatID
	}
	_, err = r.messages.Create(context.Background(), chatID, string(content), "")
	return err
}

func (r *Runtime) SendApprovalCard(
	ctx context.Context,
	request ApprovalCardRequest,
) (string, error) {
	if !r.config.Enabled() || r.messages == nil {
		return "", ErrNotConfigured
	}
	content, err := RenderApprovalCard(request)
	if err != nil {
		return "", err
	}
	return r.messages.Create(
		ctx,
		r.config.DefaultChatID,
		string(content),
		stableUUID("approval-card", request.WorkflowID, request.RunID, request.Approval.Key),
	)
}

func (r *Runtime) UpdateApprovalCard(
	ctx context.Context,
	messageID string,
	request DecisionCardRequest,
) error {
	if !r.config.Enabled() || r.messages == nil {
		return ErrNotConfigured
	}
	content, err := RenderDecisionCard(request)
	if err != nil {
		return err
	}
	return r.messages.Patch(ctx, messageID, string(content))
}

func (r *Runtime) SendMessageActivity(ctx context.Context, message notify.Msg) error {
	return nonRetryableNotConfigured(r.SendMessage(ctx, message))
}

func (r *Runtime) SendApprovalCardActivity(
	ctx context.Context,
	request ApprovalCardRequest,
) (string, error) {
	messageID, err := r.SendApprovalCard(ctx, request)
	return messageID, nonRetryableNotConfigured(err)
}

func (r *Runtime) UpdateApprovalCardActivity(
	ctx context.Context,
	request UpdateApprovalCardRequest,
) error {
	return nonRetryableNotConfigured(r.UpdateApprovalCard(ctx, request.MessageID, request.Card))
}

func nonRetryableNotConfigured(err error) error {
	if errors.Is(err, ErrNotConfigured) {
		return temporal.NewNonRetryableApplicationError(
			"Feishu is not configured",
			"FeishuNotConfigured",
			ErrNotConfigured,
		)
	}
	return err
}

func stableUUID(parts ...string) string {
	hash := sha256.Sum256([]byte(fmt.Sprint(parts)))
	return hex.EncodeToString(hash[:20])
}

type sdkMessages struct {
	client *lark.Client
}

func (m *sdkMessages) Create(
	ctx context.Context,
	chatID string,
	content string,
	uuid string,
) (string, error) {
	bodyBuilder := larkim.NewCreateMessageReqBodyBuilder().
		ReceiveId(chatID).
		MsgType(larkim.MsgTypeInteractive).
		Content(content)
	if uuid != "" {
		bodyBuilder.Uuid(uuid)
	}
	response, err := m.client.Im.Message.Create(
		ctx,
		larkim.NewCreateMessageReqBuilder().
			ReceiveIdType("chat_id").
			Body(bodyBuilder.Build()).
			Build(),
	)
	if err != nil {
		return "", err
	}
	if !response.Success() {
		return "", fmt.Errorf("Feishu rejected message: code=%d message=%s", response.Code, response.Msg)
	}
	if response.Data == nil || response.Data.MessageId == nil {
		return "", errors.New("Feishu response is missing message_id")
	}
	return *response.Data.MessageId, nil
}

func (m *sdkMessages) Patch(ctx context.Context, messageID string, content string) error {
	response, err := m.client.Im.Message.Patch(
		ctx,
		larkim.NewPatchMessageReqBuilder().
			MessageId(messageID).
			Body(
				larkim.NewPatchMessageReqBodyBuilder().
					Content(content).
					Build(),
			).
			Build(),
	)
	if err != nil {
		return err
	}
	if !response.Success() {
		return fmt.Errorf("Feishu rejected card update: code=%d message=%s", response.Code, response.Msg)
	}
	return nil
}
