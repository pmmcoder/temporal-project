package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"time"
)

type workflowDefinition struct {
	ID string `json:"id"`
}

type startResponse struct {
	WorkflowID string `json:"workflowId"`
	RunID      string `json:"runId"`
	Existing   bool   `json:"existing"`
}

type runDetail struct {
	Status string `json:"status"`
	Output any    `json:"output"`
}

type pendingApproval struct {
	ApprovalID string `json:"approvalId"`
	WorkflowID string `json:"workflowId"`
	RunID      string `json:"runId"`
}

type scheduleResponse struct {
	ID string `json:"id"`
}

func main() {
	baseURL := flag.String("base-url", "http://127.0.0.1:8080", "platform API base URL")
	timeout := flag.Duration("timeout", 30*time.Second, "workflow completion timeout")
	flag.Parse()

	if err := run(*baseURL, *timeout); err != nil {
		fmt.Fprintln(os.Stderr, "smoke:", err)
		os.Exit(1)
	}
	fmt.Println("smoke: workflow platform is healthy")
}

func run(baseURL string, timeout time.Duration) error {
	var workflows struct {
		Workflows []workflowDefinition `json:"workflows"`
	}
	if err := requestJSON(http.MethodGet, baseURL+"/api/v1/workflows", nil, &workflows); err != nil {
		return fmt.Errorf("list workflows: %w", err)
	}
	found := false
	for _, workflow := range workflows.Workflows {
		if workflow.ID == "greeting" {
			found = true
			break
		}
	}
	if !found {
		return errors.New("greeting workflow is not registered")
	}

	key := fmt.Sprintf("smoke-%d", time.Now().UnixNano())
	startBody := map[string]any{
		"input":          map[string]any{"names": []string{"Compose"}},
		"idempotencyKey": key,
	}
	var started startResponse
	if err := requestJSON(
		http.MethodPost,
		baseURL+"/api/v1/workflows/greeting/runs",
		startBody,
		&started,
	); err != nil {
		return fmt.Errorf("start workflow: %w", err)
	}
	if started.WorkflowID == "" || started.RunID == "" {
		return errors.New("start response is missing workflow or run ID")
	}

	var duplicate startResponse
	if err := requestJSON(
		http.MethodPost,
		baseURL+"/api/v1/workflows/greeting/runs",
		startBody,
		&duplicate,
	); err != nil {
		return fmt.Errorf("repeat idempotent start: %w", err)
	}
	if !duplicate.Existing || duplicate.RunID != started.RunID {
		return errors.New("idempotent start did not return the existing run")
	}

	detailURL := fmt.Sprintf(
		"%s/api/v1/runs/%s/%s",
		baseURL,
		url.PathEscape(started.WorkflowID),
		url.PathEscape(started.RunID),
	)
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		var detail runDetail
		if err := requestJSON(http.MethodGet, detailURL, nil, &detail); err != nil {
			return fmt.Errorf("get workflow detail: %w", err)
		}
		if detail.Status == "succeeded" {
			if detail.Output == nil {
				return errors.New("completed workflow has no output")
			}
			break
		}
		if detail.Status != "running" {
			return fmt.Errorf("workflow ended with status %q", detail.Status)
		}
		time.Sleep(500 * time.Millisecond)
	}
	if time.Now().After(deadline) {
		return fmt.Errorf("workflow did not complete within %s", timeout)
	}

	if err := smokeApproval(baseURL, timeout); err != nil {
		return err
	}
	if err := smokeSchedule(baseURL); err != nil {
		return err
	}
	return nil
}

func smokeApproval(baseURL string, timeout time.Duration) error {
	started, err := startApproval(baseURL)
	if err != nil {
		return err
	}
	found, err := waitForApproval(baseURL, started, timeout)
	if err != nil {
		return fmt.Errorf("wait for approval: %w", err)
	}

	endpoint := fmt.Sprintf(
		"%s/api/v1/approvals/%s/%s/%s/decision",
		baseURL,
		url.PathEscape(started.WorkflowID),
		url.PathEscape(started.RunID),
		url.PathEscape(found.ApprovalID),
	)
	if err := requestJSON(http.MethodPost, endpoint, map[string]any{
		"decision": "approved",
		"comment":  "smoke",
	}, &map[string]any{}); err != nil {
		return fmt.Errorf("approve workflow: %w", err)
	}
	return waitForRun(baseURL, started, timeout)
}

func waitForApproval(
	baseURL string,
	started startResponse,
	timeout time.Duration,
) (*pendingApproval, error) {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		var approvals struct {
			Approvals []pendingApproval `json:"approvals"`
		}
		if err := requestJSON(http.MethodGet, baseURL+"/api/v1/approvals", nil, &approvals); err != nil {
			return nil, err
		}
		for _, item := range approvals.Approvals {
			if item.WorkflowID == started.WorkflowID && item.RunID == started.RunID {
				copy := item
				return &copy, nil
			}
		}
		time.Sleep(500 * time.Millisecond)
	}
	return nil, errors.New("approval did not appear in pending list")
}

func startApproval(baseURL string) (startResponse, error) {
	key := fmt.Sprintf("approval-smoke-%d", time.Now().UnixNano())
	var started startResponse
	err := requestJSON(http.MethodPost, baseURL+"/api/v1/workflows/approvaldemo/runs", map[string]any{
		"input": map[string]any{
			"title":   "Smoke approval",
			"summary": "Created by make smoke",
		},
		"idempotencyKey": key,
	}, &started)
	return started, err
}

func waitForRun(baseURL string, started startResponse, timeout time.Duration) error {
	endpoint := fmt.Sprintf(
		"%s/api/v1/runs/%s/%s",
		baseURL,
		url.PathEscape(started.WorkflowID),
		url.PathEscape(started.RunID),
	)
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		var detail runDetail
		if err := requestJSON(http.MethodGet, endpoint, nil, &detail); err != nil {
			return err
		}
		if detail.Status == "succeeded" {
			return nil
		}
		if detail.Status != "running" {
			return fmt.Errorf("workflow ended with status %q", detail.Status)
		}
		time.Sleep(500 * time.Millisecond)
	}
	return fmt.Errorf("workflow did not complete within %s", timeout)
}

func smokeSchedule(baseURL string) error {
	var created scheduleResponse
	if err := requestJSON(http.MethodPost, baseURL+"/api/v1/schedules", map[string]any{
		"name":       "Smoke schedule",
		"workflowId": "greeting",
		"input":      map[string]any{"names": []string{"Schedule"}},
		"cron":       "0 9 * * *",
		"timezone":   "Asia/Shanghai",
	}, &created); err != nil {
		return fmt.Errorf("create schedule: %w", err)
	}
	if created.ID == "" {
		return errors.New("schedule response is missing ID")
	}

	deadline := time.Now().Add(10 * time.Second)
	for {
		var list struct {
			Schedules []scheduleResponse `json:"schedules"`
		}
		if err := requestJSON(http.MethodGet, baseURL+"/api/v1/schedules", nil, &list); err != nil {
			return fmt.Errorf("list schedules: %w", err)
		}
		for _, item := range list.Schedules {
			if item.ID == created.ID {
				goto found
			}
		}
		if time.Now().After(deadline) {
			return errors.New("created schedule is missing from list")
		}
		time.Sleep(500 * time.Millisecond)
	}

found:

	for _, action := range []string{"pause", "resume"} {
		endpoint := fmt.Sprintf("%s/api/v1/schedules/%s/%s", baseURL, url.PathEscape(created.ID), action)
		if err := requestJSON(http.MethodPost, endpoint, map[string]any{}, &map[string]any{}); err != nil {
			return fmt.Errorf("%s schedule: %w", action, err)
		}
	}
	if err := requestJSON(
		http.MethodDelete,
		fmt.Sprintf("%s/api/v1/schedules/%s", baseURL, url.PathEscape(created.ID)),
		map[string]any{},
		&map[string]any{},
	); err != nil {
		return fmt.Errorf("delete schedule: %w", err)
	}
	return nil
}

func requestJSON(method, endpoint string, body any, destination any) error {
	var encoded []byte
	var err error
	if body != nil {
		encoded, err = json.Marshal(body)
		if err != nil {
			return err
		}
	}

	request, err := http.NewRequest(method, endpoint, bytes.NewReader(encoded))
	if err != nil {
		return err
	}
	if body != nil {
		request.Header.Set("Content-Type", "application/json")
		request.Header.Set("Origin", origin(endpoint))
	}

	client := http.Client{Timeout: 5 * time.Second}
	response, err := client.Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return fmt.Errorf("%s returned HTTP %d", endpoint, response.StatusCode)
	}
	return json.NewDecoder(response.Body).Decode(destination)
}

func origin(endpoint string) string {
	parsed, err := url.Parse(endpoint)
	if err != nil {
		return ""
	}
	return parsed.Scheme + "://" + parsed.Host
}
