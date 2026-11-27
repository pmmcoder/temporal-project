package feishu_test

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"personal-workflow-platform/platform/feishu"
	"personal-workflow-platform/platform/sdk/approval"
)

func TestRenderApprovalCardUsesJSON2Form(t *testing.T) {
	cardJSON, err := feishu.RenderApprovalCard(feishu.ApprovalCardRequest{
		WorkflowID: "approvaldemo:key",
		RunID:      "run-1",
		CreatedAt:  time.Date(2026, 11, 20, 10, 0, 0, 0, time.UTC),
		Approval: approval.Req{
			Key:          "publish",
			Title:        "发布确认",
			Summary:      "准备发布",
			AllowComment: true,
			Timeout:      time.Hour,
			OnTimeout:    approval.Reject,
		},
	})
	if err != nil {
		t.Fatalf("RenderApprovalCard() error = %v", err)
	}

	var card map[string]any
	if err := json.Unmarshal(cardJSON, &card); err != nil {
		t.Fatalf("card JSON is invalid: %v", err)
	}
	if card["schema"] != "2.0" {
		t.Errorf("schema = %v", card["schema"])
	}
	config := card["config"].(map[string]any)
	if config["update_multi"] != true {
		t.Errorf("update_multi = %v", config["update_multi"])
	}
	header := card["header"].(map[string]any)
	if header["template"] != "grey" {
		t.Errorf("header template = %v, want grey", header["template"])
	}
	subtitle := header["subtitle"].(map[string]any)["content"].(string)
	if !strings.Contains(subtitle, "待审批") || strings.Contains(subtitle, "approvaldemo:key") {
		t.Errorf("subtitle = %q", subtitle)
	}
	body := card["body"].(map[string]any)
	elements := body["elements"].([]any)
	formFound := false
	for _, raw := range elements {
		element := raw.(map[string]any)
		if element["tag"] != "form" {
			continue
		}
		formFound = true
		formElements := element["elements"].([]any)
		if len(formElements) != 2 {
			t.Fatalf("form elements = %d, want input + button columns", len(formElements))
		}
		input, ok := formElements[0].(map[string]any)
		if !ok || input["tag"] != "input" ||
			input["input_type"] != "text" ||
			input["max_length"] != float64(200) {
			t.Errorf("input = %#v, want compact single-line input", formElements[0])
		}
		columns, ok := formElements[1].(map[string]any)
		if !ok || columns["tag"] != "column_set" {
			t.Fatalf("button container = %#v", formElements[1])
		}
		if len(columns["columns"].([]any)) != 2 {
			t.Errorf("button columns = %#v", columns["columns"])
		}
	}
	if !formFound {
		t.Fatal("approval form is missing")
	}
	if !strings.Contains(string(cardJSON), "Workflow approvaldemo:key") ||
		!strings.Contains(string(cardJSON), "Run run-1") {
		t.Errorf("short IDs are missing from card footer: %s", cardJSON)
	}
}

func TestRenderDecisionCardRemovesActions(t *testing.T) {
	cardJSON, err := feishu.RenderDecisionCard(feishu.DecisionCardRequest{
		WorkflowID: "approvaldemo:key",
		RunID:      "run-1",
		ApprovalID: "publish",
		Title:      "发布确认",
		Decision: approval.Result{
			Decision:  approval.Approved,
			Comment:   "ok",
			Operator:  "local",
			DecidedAt: time.Now(),
		},
	})
	if err != nil {
		t.Fatalf("RenderDecisionCard() error = %v", err)
	}
	var card map[string]any
	if err := json.Unmarshal(cardJSON, &card); err != nil {
		t.Fatalf("card JSON is invalid: %v", err)
	}
	if string(cardJSON) == "" {
		t.Fatal("decision card is empty")
	}
	if _, exists := card["body"].(map[string]any)["elements"]; !exists {
		t.Fatal("decision card body is missing")
	}
	header := card["header"].(map[string]any)
	if header["template"] != "green" {
		t.Errorf("approved header template = %v", header["template"])
	}
	if header["title"].(map[string]any)["content"] != "发布确认" {
		t.Errorf("decision title = %#v", header["title"])
	}
}
