package feishu_test

import (
	"context"
	"testing"

	"personal-workflow-platform/platform/approvals"
	"personal-workflow-platform/platform/feishu"
	"personal-workflow-platform/platform/sdk/approval"

	larkcallback "github.com/larksuite/oapi-sdk-go/v3/event/dispatcher/callback"
)

type fakeDecider struct {
	request approvals.DecisionRequest
	err     error
}

func (d *fakeDecider) Decide(_ context.Context, request approvals.DecisionRequest) error {
	d.request = request
	return d.err
}

func TestHandleCardActionConvertsFormDecision(t *testing.T) {
	runtime := feishu.NewRuntimeWithMessages(enabledConfig(), nil, nil)
	decider := &fakeDecider{}
	event := &larkcallback.CardActionTriggerEvent{
		Event: &larkcallback.CardActionTriggerRequest{
			Operator: &larkcallback.Operator{OpenID: "ou_1"},
			Action: &larkcallback.CallBackAction{
				Value: map[string]any{
					"workflow_id": "approvaldemo:key",
					"run_id":      "run-1",
					"approval_id": "publish",
					"decision":    "approved",
				},
				FormValue: map[string]any{"comment": "ok"},
			},
		},
	}

	response, err := runtime.HandleCardAction(context.Background(), event, decider)
	if err != nil {
		t.Fatalf("HandleCardAction() error = %v", err)
	}
	if response.Toast == nil || response.Toast.Type != "success" {
		t.Fatalf("response = %#v", response)
	}
	if decider.request.WorkflowID != "approvaldemo:key" ||
		decider.request.ApprovalID != "publish" ||
		decider.request.Decision != approval.Approved ||
		decider.request.Comment != "ok" ||
		decider.request.Operator != "ou_1" ||
		decider.request.Channel != "feishu" {
		t.Fatalf("decision request = %#v", decider.request)
	}
}

func TestHandleCardActionRejectsUnknownOperator(t *testing.T) {
	runtime := feishu.NewRuntimeWithMessages(enabledConfig(), nil, nil)
	decider := &fakeDecider{}
	event := &larkcallback.CardActionTriggerEvent{
		Event: &larkcallback.CardActionTriggerRequest{
			Operator: &larkcallback.Operator{OpenID: "ou_other"},
			Action: &larkcallback.CallBackAction{
				Value: map[string]any{},
			},
		},
	}

	response, err := runtime.HandleCardAction(context.Background(), event, decider)
	if err != nil {
		t.Fatalf("HandleCardAction() error = %v", err)
	}
	if response.Toast == nil || response.Toast.Type != "error" {
		t.Fatalf("response = %#v", response)
	}
	if decider.request.WorkflowID != "" {
		t.Fatal("decider was called for unauthorized operator")
	}
}
