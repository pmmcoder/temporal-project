package feishu

import (
	"context"
	"fmt"
	"strings"
	"time"

	"personal-workflow-platform/platform/approvals"
	"personal-workflow-platform/platform/sdk/approval"

	larkdispatcher "github.com/larksuite/oapi-sdk-go/v3/event/dispatcher"
	larkcallback "github.com/larksuite/oapi-sdk-go/v3/event/dispatcher/callback"
	larkws "github.com/larksuite/oapi-sdk-go/v3/ws"
)

type Decider interface {
	Decide(context.Context, approvals.DecisionRequest) error
}

func (r *Runtime) Start(ctx context.Context, decider Decider) error {
	if !r.config.Enabled() {
		return nil
	}
	dispatcher := larkdispatcher.NewEventDispatcher("", "").
		OnP2CardActionTrigger(func(
			callbackCtx context.Context,
			event *larkcallback.CardActionTriggerEvent,
		) (*larkcallback.CardActionTriggerResponse, error) {
			return r.HandleCardAction(callbackCtx, event, decider)
		})
	wsClient := larkws.NewClient(
		r.config.AppID,
		r.config.AppSecret,
		larkws.WithEventHandler(dispatcher),
	)
	if r.logger != nil {
		r.logger.Info("starting Feishu long connection")
	}
	return wsClient.Start(ctx)
}

func (r *Runtime) HandleCardAction(
	ctx context.Context,
	event *larkcallback.CardActionTriggerEvent,
	decider Decider,
) (*larkcallback.CardActionTriggerResponse, error) {
	if event == nil || event.Event == nil || event.Event.Operator == nil || event.Event.Action == nil {
		return errorToast("无效的审批回调"), nil
	}
	if !r.isApprover(event.Event.Operator.OpenID) {
		return errorToast("无审批权限"), nil
	}

	value := event.Event.Action.Value
	decision, ok := parseDecision(stringValue(value["decision"]))
	if !ok {
		return errorToast("无效的审批决定"), nil
	}
	request := approvals.DecisionRequest{
		WorkflowID: stringValue(value["workflow_id"]),
		RunID:      stringValue(value["run_id"]),
		ApprovalID: stringValue(value["approval_id"]),
		Decision:   decision,
		Comment:    stringValue(event.Event.Action.FormValue["comment"]),
		Operator:   event.Event.Operator.OpenID,
		Channel:    "feishu",
	}
	if request.WorkflowID == "" || request.RunID == "" || request.ApprovalID == "" {
		return errorToast("审批参数不完整"), nil
	}

	decisionCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	if err := decider.Decide(decisionCtx, request); err != nil {
		if r.logger != nil {
			r.logger.Warn("Feishu approval decision failed", "error", err)
		}
		return errorToast("审批提交失败"), nil
	}
	return &larkcallback.CardActionTriggerResponse{
		Toast: &larkcallback.Toast{
			Type:    "success",
			Content: "审批已提交",
		},
	}, nil
}

func (r *Runtime) isApprover(openID string) bool {
	for _, allowed := range r.config.ApproverOpenIDs {
		if allowed == openID {
			return true
		}
	}
	return false
}

func parseDecision(value string) (approval.Decision, bool) {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "approved", "approve":
		return approval.Approved, true
	case "rejected", "reject":
		return approval.Rejected, true
	default:
		return "", false
	}
}

func stringValue(value any) string {
	if value == nil {
		return ""
	}
	if text, ok := value.(string); ok {
		return text
	}
	return fmt.Sprint(value)
}

func errorToast(message string) *larkcallback.CardActionTriggerResponse {
	return &larkcallback.CardActionTriggerResponse{
		Toast: &larkcallback.Toast{
			Type:    "error",
			Content: message,
		},
	}
}
