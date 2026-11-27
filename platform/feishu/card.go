package feishu

import (
	"encoding/json"
	"fmt"
	"strings"

	"personal-workflow-platform/platform/sdk/approval"
)

type ApprovalCardRequest = approval.CardRequest
type DecisionCardRequest = approval.DecisionCardRequest

func RenderApprovalCard(request ApprovalCardRequest) (json.RawMessage, error) {
	fields := make([]string, 0, len(request.Approval.Fields))
	for _, field := range request.Approval.Fields {
		fields = append(fields, fmt.Sprintf("**%s**：%s", field.Label, field.Value))
	}
	summary := request.Approval.Summary
	if len(fields) > 0 {
		summary += "\n\n" + strings.Join(fields, "\n")
	}

	metadata := make([]string, 0, 2)
	if !request.CreatedAt.IsZero() {
		metadata = append(metadata, fmt.Sprintf(
			"**截止时间**：%s",
			request.CreatedAt.Add(request.Approval.Timeout).Format("2006-01-02 15:04"),
		))
	}
	if request.Approval.AllowComment {
		metadata = append(metadata, "**备注**：可选")
	}

	formElements := make([]any, 0, 3)
	if request.Approval.AllowComment {
		formElements = append(formElements, map[string]any{
			"tag":         "input",
			"name":        "comment",
			"input_type":  "text",
			"placeholder": textElement("备注（可选）"),
			"max_length":  200,
		})
	}
	formElements = append(formElements, map[string]any{
		"tag":       "column_set",
		"flex_mode": "bisect",
		"columns": []any{
			buttonColumn(approvalButton("approve", "通过", "primary_filled", "approved", request)),
			buttonColumn(approvalButton("reject", "拒绝", "danger_filled", "rejected", request)),
		},
	})

	bodyElements := []any{
		map[string]any{"tag": "markdown", "content": summary},
	}
	if len(metadata) > 0 {
		bodyElements = append(bodyElements, map[string]any{
			"tag":     "markdown",
			"content": strings.Join(metadata, "\n"),
		})
	}
	bodyElements = append(bodyElements, map[string]any{
		"tag":      "form",
		"name":     "approval_form",
		"elements": formElements,
	})
	bodyElements = append(bodyElements, map[string]any{
		"tag": "markdown",
		"content": fmt.Sprintf(
			"<font color='grey'>Workflow %s · Run %s</font>",
			shortWorkflowID(request.WorkflowID),
			shortRunID(request.RunID),
		),
	})

	card := map[string]any{
		"schema": "2.0",
		"config": map[string]any{
			"update_multi": true,
		},
		"header": map[string]any{
			"template": "grey",
			"title":    textElement(request.Approval.Title),
			"subtitle": textElement("待审批 · 请完成审批"),
		},
		"body": map[string]any{
			"elements": bodyElements,
		},
	}
	return json.Marshal(card)
}

func RenderDecisionCard(request DecisionCardRequest) (json.RawMessage, error) {
	status := "已拒绝"
	template := "red"
	if request.Decision.Decision == approval.Approved {
		status = "已通过"
		template = "green"
	}
	details := fmt.Sprintf(
		"**审批编号**：%s\n**处理人**：%s\n**处理渠道**：%s\n**时间**：%s",
		request.ApprovalID,
		request.Decision.Operator,
		request.Decision.Channel,
		request.Decision.DecidedAt.Format("2006-01-02 15:04:05"),
	)
	if request.Decision.Comment != "" {
		details += "\n**备注**：" + request.Decision.Comment
	}
	details += fmt.Sprintf(
		"\n\n<font color='grey'>Workflow %s · Run %s</font>",
		shortWorkflowID(request.WorkflowID),
		shortRunID(request.RunID),
	)
	title := request.Title
	if title == "" {
		title = status
	}
	card := map[string]any{
		"schema": "2.0",
		"config": map[string]any{
			"update_multi": true,
		},
		"header": map[string]any{
			"template": template,
			"title":    textElement(title),
			"subtitle": textElement(status + " · 审批已完成"),
		},
		"body": map[string]any{
			"elements": []any{
				map[string]any{"tag": "markdown", "content": details},
			},
		},
	}
	return json.Marshal(card)
}

func shortWorkflowID(value string) string {
	if len(value) <= 32 {
		return value
	}
	return value[:20] + "..."
}

func shortRunID(value string) string {
	if len(value) <= 8 {
		return value
	}
	return value[:8]
}

func buttonColumn(button map[string]any) map[string]any {
	button["width"] = "fill"
	return map[string]any{
		"tag":      "column",
		"width":    "weighted",
		"weight":   1,
		"elements": []any{button},
	}
}

func approvalButton(
	name string,
	label string,
	style string,
	decision string,
	request ApprovalCardRequest,
) map[string]any {
	return map[string]any{
		"tag":              "button",
		"name":             name,
		"form_action_type": "submit",
		"text":             textElement(label),
		"type":             style,
		"behaviors": []any{
			map[string]any{
				"type": "callback",
				"value": map[string]any{
					"workflow_id": request.WorkflowID,
					"run_id":      request.RunID,
					"approval_id": request.Approval.Key,
					"decision":    decision,
				},
			},
		},
	}
}

func textElement(content string) map[string]any {
	return map[string]any{
		"tag":     "plain_text",
		"content": content,
	}
}
