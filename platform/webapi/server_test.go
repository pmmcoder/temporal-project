package webapi_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"personal-workflow-platform/platform/approvals"
	"personal-workflow-platform/platform/catalog"
	"personal-workflow-platform/platform/runs"
	"personal-workflow-platform/platform/schedules"
	"personal-workflow-platform/platform/sdk"
	"personal-workflow-platform/platform/sdk/approval"
	"personal-workflow-platform/platform/webapi"

	"go.temporal.io/sdk/workflow"
)

type helloInput struct {
	Name string `json:"name" description:"姓名"`
}

type fakeRunStarter struct {
	request runs.StartRequest
	run     runs.Run
	err     error
}

type fakeRunReader struct {
	page      runs.RunPage
	detail    runs.RunDetail
	listErr   error
	getErr    error
	cancelErr error
	cancelled runs.RunRef
	signalErr error
	signalReq runs.SignalRequest
}

type fakeApprovalManager struct {
	items   []approvals.PendingApproval
	request approvals.DecisionRequest
	err     error
}

type fakeScheduleManager struct {
	items     []schedules.Schedule
	create    schedules.CreateRequest
	pausedID  string
	resumedID string
	deletedID string
	update    schedules.UpdateRequest
	err       error
}

func (m *fakeScheduleManager) List(context.Context) ([]schedules.Schedule, error) {
	return m.items, m.err
}

func (m *fakeScheduleManager) Create(
	_ context.Context,
	request schedules.CreateRequest,
) (schedules.Schedule, error) {
	m.create = request
	return schedules.Schedule{ID: "greeting-abc12345"}, m.err
}

func (m *fakeScheduleManager) Pause(_ context.Context, id string) error {
	m.pausedID = id
	return m.err
}

func (m *fakeScheduleManager) Update(_ context.Context, request schedules.UpdateRequest) error {
	m.update = request
	return m.err
}

func (m *fakeScheduleManager) Resume(_ context.Context, id string) error {
	m.resumedID = id
	return m.err
}

func (m *fakeScheduleManager) Delete(_ context.Context, id string) error {
	m.deletedID = id
	return m.err
}

func (m *fakeApprovalManager) List(context.Context) ([]approvals.PendingApproval, error) {
	return m.items, m.err
}

func (m *fakeApprovalManager) Decide(_ context.Context, request approvals.DecisionRequest) error {
	m.request = request
	return m.err
}

func (r *fakeRunReader) List(_ context.Context, _ runs.ListRequest) (runs.RunPage, error) {
	return r.page, r.listErr
}

func (r *fakeRunReader) Get(_ context.Context, _ runs.RunRef) (runs.RunDetail, error) {
	return r.detail, r.getErr
}

func (r *fakeRunReader) Cancel(_ context.Context, ref runs.RunRef) error {
	r.cancelled = ref
	return r.cancelErr
}

func (r *fakeRunReader) Signal(_ context.Context, request runs.SignalRequest) error {
	r.signalReq = request
	return r.signalErr
}

func (s *fakeRunStarter) Start(_ context.Context, request runs.StartRequest) (runs.Run, error) {
	s.request = request
	return s.run, s.err
}

func helloWorkflow(ctx workflow.Context, in helloInput) (string, error) {
	return in.Name, nil
}

func TestWorkflowCatalogEndpoint(t *testing.T) {
	registry := sdk.NewRegistry()
	sdk.Workflow(registry, sdk.WorkflowDef[helloInput, string]{
		ID:          "hello",
		DisplayName: "Hello",
		Description: "say hello",
		Fn:          helloWorkflow,
		Example:     helloInput{Name: "Alice"},
	})
	catalogView, err := catalog.New(registry)
	if err != nil {
		t.Fatalf("catalog.New() error = %v", err)
	}

	handler, err := webapi.New(webapi.Options{Catalog: catalogView})
	if err != nil {
		t.Fatalf("webapi.New() error = %v", err)
	}

	request := httptest.NewRequest(http.MethodGet, "/api/v1/workflows", nil)
	request.Host = "127.0.0.1:8080"
	response := httptest.NewRecorder()

	handler.ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body=%s", response.Code, http.StatusOK, response.Body.String())
	}
	if response.Header().Get("X-Request-ID") == "" {
		t.Error("X-Request-ID header is empty")
	}

	var body struct {
		Workflows []struct {
			ID           string          `json:"id"`
			DisplayName  string          `json:"displayName"`
			InputSchema  json.RawMessage `json:"inputSchema"`
			ExampleInput json.RawMessage `json:"exampleInput"`
		} `json:"workflows"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(body.Workflows) != 1 {
		t.Fatalf("workflows length = %d, want 1", len(body.Workflows))
	}
	if body.Workflows[0].ID != "hello" || body.Workflows[0].DisplayName != "Hello" {
		t.Errorf("workflow = %#v, want hello/Hello", body.Workflows[0])
	}
	if len(body.Workflows[0].InputSchema) == 0 || len(body.Workflows[0].ExampleInput) == 0 {
		t.Error("workflow schema or example is empty")
	}
}

func TestHealthEndpoint(t *testing.T) {
	registry := sdk.NewRegistry()
	catalogView, err := catalog.New(registry)
	if err != nil {
		t.Fatalf("catalog.New() error = %v", err)
	}
	handler, err := webapi.New(webapi.Options{Catalog: catalogView})
	if err != nil {
		t.Fatalf("webapi.New() error = %v", err)
	}

	request := httptest.NewRequest(http.MethodGet, "/api/v1/healthz", nil)
	request.Host = "127.0.0.1:8080"
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body=%s", response.Code, http.StatusOK, response.Body.String())
	}
}

func TestStartWorkflowEndpoint(t *testing.T) {
	registry := sdk.NewRegistry()
	sdk.Workflow(registry, sdk.WorkflowDef[helloInput, string]{
		ID:          "hello",
		DisplayName: "Hello",
		Fn:          helloWorkflow,
		Example:     helloInput{Name: "Alice"},
	})
	catalogView, err := catalog.New(registry)
	if err != nil {
		t.Fatalf("catalog.New() error = %v", err)
	}
	starter := &fakeRunStarter{
		run: runs.Run{WorkflowID: "hello:key", RunID: "run-1"},
	}
	handler, err := webapi.New(webapi.Options{
		Catalog: catalogView,
		Runs:    starter,
	})
	if err != nil {
		t.Fatalf("webapi.New() error = %v", err)
	}

	request := httptest.NewRequest(
		http.MethodPost,
		"/api/v1/workflows/hello/runs",
		strings.NewReader(`{"input":{"name":"Alice"},"idempotencyKey":"key"}`),
	)
	request.Host = "127.0.0.1:8080"
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Origin", "http://127.0.0.1:8080")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)

	if response.Code != http.StatusCreated {
		t.Fatalf("status = %d, want %d; body=%s", response.Code, http.StatusCreated, response.Body.String())
	}
	if starter.request.DefinitionID != "hello" || starter.request.IdempotencyKey != "key" {
		t.Fatalf("start request = %#v", starter.request)
	}

	var body struct {
		WorkflowID string `json:"workflowId"`
		RunID      string `json:"runId"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if body.WorkflowID != "hello:key" || body.RunID != "run-1" {
		t.Errorf("response = %#v", body)
	}
}

func TestRunListDetailAndCancelEndpoints(t *testing.T) {
	registry := sdk.NewRegistry()
	catalogView, err := catalog.New(registry)
	if err != nil {
		t.Fatalf("catalog.New() error = %v", err)
	}
	startTime := time.Date(2026, 11, 20, 9, 0, 0, 0, time.UTC)
	reader := &fakeRunReader{
		page: runs.RunPage{
			Runs: []runs.RunSummary{{
				WorkflowID:   "greeting:key",
				RunID:        "run-1",
				WorkflowType: "greeting",
				Status:       runs.StatusRunning,
				StartTime:    &startTime,
			}},
		},
		detail: runs.RunDetail{
			RunSummary: runs.RunSummary{
				WorkflowID:   "greeting:key",
				RunID:        "run-1",
				WorkflowType: "greeting",
				Status:       runs.StatusRunning,
				StartTime:    &startTime,
			},
			TaskQueue:     "personal-workflows",
			HistoryLength: 20,
		},
	}
	handler, err := webapi.New(webapi.Options{
		Catalog:   catalogView,
		RunReader: reader,
	})
	if err != nil {
		t.Fatalf("webapi.New() error = %v", err)
	}

	listRequest := httptest.NewRequest(http.MethodGet, "/api/v1/runs?status=running", nil)
	listRequest.Host = "127.0.0.1:8080"
	listResponse := httptest.NewRecorder()
	handler.ServeHTTP(listResponse, listRequest)
	if listResponse.Code != http.StatusOK {
		t.Fatalf("list status = %d; body=%s", listResponse.Code, listResponse.Body.String())
	}
	if !strings.Contains(listResponse.Body.String(), `"workflowId":"greeting:key"`) {
		t.Errorf("list body = %s", listResponse.Body.String())
	}

	detailRequest := httptest.NewRequest(
		http.MethodGet,
		"/api/v1/runs/greeting%3Akey/run-1",
		nil,
	)
	detailRequest.Host = "127.0.0.1:8080"
	detailResponse := httptest.NewRecorder()
	handler.ServeHTTP(detailResponse, detailRequest)
	if detailResponse.Code != http.StatusOK {
		t.Fatalf("detail status = %d; body=%s", detailResponse.Code, detailResponse.Body.String())
	}
	if !strings.Contains(detailResponse.Body.String(), `"taskQueue":"personal-workflows"`) {
		t.Errorf("detail body = %s", detailResponse.Body.String())
	}

	cancelRequest := httptest.NewRequest(
		http.MethodPost,
		"/api/v1/runs/greeting%3Akey/run-1/cancel",
		nil,
	)
	cancelRequest.Host = "127.0.0.1:8080"
	cancelRequest.Header.Set("Content-Type", "application/json")
	cancelRequest.Header.Set("Origin", "http://127.0.0.1:8080")
	cancelResponse := httptest.NewRecorder()
	handler.ServeHTTP(cancelResponse, cancelRequest)
	if cancelResponse.Code != http.StatusAccepted {
		t.Fatalf("cancel status = %d; body=%s", cancelResponse.Code, cancelResponse.Body.String())
	}
	if reader.cancelled.WorkflowID != "greeting:key" || reader.cancelled.RunID != "run-1" {
		t.Errorf("cancel ref = %#v", reader.cancelled)
	}

	signalRequest := httptest.NewRequest(
		http.MethodPost,
		"/api/v1/runs/greeting%3Akey/run-1/signals/pause",
		strings.NewReader(`{"input":{"reason":"maintenance"}}`),
	)
	signalRequest.Host = "127.0.0.1:8080"
	signalRequest.Header.Set("Content-Type", "application/json")
	signalRequest.Header.Set("Origin", "http://127.0.0.1:8080")
	signalResponse := httptest.NewRecorder()
	handler.ServeHTTP(signalResponse, signalRequest)
	if signalResponse.Code != http.StatusAccepted {
		t.Fatalf("signal status = %d; body=%s", signalResponse.Code, signalResponse.Body.String())
	}
	if reader.signalReq.Name != "pause" || reader.signalReq.WorkflowID != "greeting:key" {
		t.Errorf("signal request = %#v", reader.signalReq)
	}
}

func TestSecurityMiddlewareRejectsUntrustedHostAndCrossSiteWrites(t *testing.T) {
	registry := sdk.NewRegistry()
	catalogView, err := catalog.New(registry)
	if err != nil {
		t.Fatalf("catalog.New() error = %v", err)
	}
	handler, err := webapi.New(webapi.Options{
		Catalog:      catalogView,
		AllowedHosts: []string{"127.0.0.1:8080"},
	})
	if err != nil {
		t.Fatalf("webapi.New() error = %v", err)
	}

	badHostRequest := httptest.NewRequest(http.MethodGet, "/api/v1/healthz", nil)
	badHostRequest.Host = "attacker.example"
	badHostResponse := httptest.NewRecorder()
	handler.ServeHTTP(badHostResponse, badHostRequest)
	if badHostResponse.Code != http.StatusForbidden {
		t.Fatalf("bad host status = %d, want 403", badHostResponse.Code)
	}

	crossSiteRequest := httptest.NewRequest(
		http.MethodPost,
		"/api/v1/workflows/greeting/runs",
		strings.NewReader(`{"input":{"names":["Alice"]}}`),
	)
	crossSiteRequest.Host = "127.0.0.1:8080"
	crossSiteRequest.Header.Set("Content-Type", "application/json")
	crossSiteRequest.Header.Set("Origin", "https://attacker.example")
	crossSiteResponse := httptest.NewRecorder()
	handler.ServeHTTP(crossSiteResponse, crossSiteRequest)
	if crossSiteResponse.Code != http.StatusForbidden {
		t.Fatalf("cross-site status = %d, want 403; body=%s", crossSiteResponse.Code, crossSiteResponse.Body.String())
	}
	if crossSiteResponse.Header().Get("Access-Control-Allow-Origin") != "" {
		t.Error("response must not contain CORS headers")
	}
}

func TestEmbeddedAssetsServeSPAFallback(t *testing.T) {
	registry := sdk.NewRegistry()
	catalogView, err := catalog.New(registry)
	if err != nil {
		t.Fatalf("catalog.New() error = %v", err)
	}
	handler, err := webapi.New(webapi.Options{
		Catalog: catalogView,
		Assets: fstest.MapFS{
			"index.html":    &fstest.MapFile{Data: []byte("<!doctype html><div id=app></div>")},
			"assets/app.js": &fstest.MapFile{Data: []byte("console.log('app')")},
		},
	})
	if err != nil {
		t.Fatalf("webapi.New() error = %v", err)
	}

	request := httptest.NewRequest(http.MethodGet, "/runs/greeting/run-1", nil)
	request.Host = "127.0.0.1:8080"
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d; body=%s", response.Code, response.Body.String())
	}
	if !strings.Contains(response.Body.String(), "<div id=app>") {
		t.Errorf("body = %s, want SPA index", response.Body.String())
	}
}

func TestApprovalEndpoints(t *testing.T) {
	registry := sdk.NewRegistry()
	catalogView, err := catalog.New(registry)
	if err != nil {
		t.Fatalf("catalog.New() error = %v", err)
	}
	manager := &fakeApprovalManager{
		items: []approvals.PendingApproval{{
			MemoApproval: approval.MemoApproval{
				ID:    "publish",
				Title: "发布",
			},
			WorkflowID:   "approvaldemo:key",
			RunID:        "run-1",
			WorkflowType: "approvaldemo",
		}},
	}
	handler, err := webapi.New(webapi.Options{
		Catalog:   catalogView,
		Approvals: manager,
	})
	if err != nil {
		t.Fatalf("webapi.New() error = %v", err)
	}

	listRequest := httptest.NewRequest(http.MethodGet, "/api/v1/approvals", nil)
	listRequest.Host = "127.0.0.1:8080"
	listResponse := httptest.NewRecorder()
	handler.ServeHTTP(listResponse, listRequest)
	if listResponse.Code != http.StatusOK {
		t.Fatalf("list status = %d; body=%s", listResponse.Code, listResponse.Body.String())
	}
	if !strings.Contains(listResponse.Body.String(), `"approvalId":"publish"`) {
		t.Errorf("list body = %s", listResponse.Body.String())
	}

	decisionRequest := httptest.NewRequest(
		http.MethodPost,
		"/api/v1/approvals/approvaldemo%3Akey/run-1/publish/decision",
		strings.NewReader(`{"decision":"approved","comment":"ok"}`),
	)
	decisionRequest.Host = "127.0.0.1:8080"
	decisionRequest.Header.Set("Content-Type", "application/json")
	decisionRequest.Header.Set("Origin", "http://127.0.0.1:8080")
	decisionResponse := httptest.NewRecorder()
	handler.ServeHTTP(decisionResponse, decisionRequest)
	if decisionResponse.Code != http.StatusAccepted {
		t.Fatalf("decision status = %d; body=%s", decisionResponse.Code, decisionResponse.Body.String())
	}
	if manager.request.ApprovalID != "publish" ||
		manager.request.Decision != approval.Approved ||
		manager.request.Comment != "ok" {
		t.Errorf("decision request = %#v", manager.request)
	}
}

func TestScheduleEndpoints(t *testing.T) {
	registry := sdk.NewRegistry()
	catalogView, err := catalog.New(registry)
	if err != nil {
		t.Fatalf("catalog.New() error = %v", err)
	}
	manager := &fakeScheduleManager{
		items: []schedules.Schedule{{
			ID:           "greeting-abc12345",
			Name:         "每日问候",
			DefinitionID: "greeting",
			Cron:         "0 9 * * *",
			Timezone:     "Asia/Shanghai",
		}},
	}
	handler, err := webapi.New(webapi.Options{
		Catalog:   catalogView,
		Schedules: manager,
	})
	if err != nil {
		t.Fatalf("webapi.New() error = %v", err)
	}

	listRequest := httptest.NewRequest(http.MethodGet, "/api/v1/schedules", nil)
	listRequest.Host = "127.0.0.1:8080"
	listResponse := httptest.NewRecorder()
	handler.ServeHTTP(listResponse, listRequest)
	if listResponse.Code != http.StatusOK {
		t.Fatalf("list status = %d; body=%s", listResponse.Code, listResponse.Body.String())
	}

	createRequest := httptest.NewRequest(
		http.MethodPost,
		"/api/v1/schedules",
		strings.NewReader(`{"name":"每日问候","workflowId":"greeting","input":{"name":"Alice"},"cron":"0 9 * * *","timezone":"Asia/Shanghai"}`),
	)
	createRequest.Host = "127.0.0.1:8080"
	createRequest.Header.Set("Content-Type", "application/json")
	createRequest.Header.Set("Origin", "http://127.0.0.1:8080")
	createResponse := httptest.NewRecorder()
	handler.ServeHTTP(createResponse, createRequest)
	if createResponse.Code != http.StatusCreated {
		t.Fatalf("create status = %d; body=%s", createResponse.Code, createResponse.Body.String())
	}
	if manager.create.DefinitionID != "greeting" || manager.create.Cron != "0 9 * * *" {
		t.Errorf("create request = %#v", manager.create)
	}

	pauseRequest := httptest.NewRequest(
		http.MethodPost,
		"/api/v1/schedules/greeting-abc12345/pause",
		strings.NewReader(`{}`),
	)
	pauseRequest.Host = "127.0.0.1:8080"
	pauseRequest.Header.Set("Content-Type", "application/json")
	pauseRequest.Header.Set("Origin", "http://127.0.0.1:8080")
	pauseResponse := httptest.NewRecorder()
	handler.ServeHTTP(pauseResponse, pauseRequest)
	if pauseResponse.Code != http.StatusOK {
		t.Fatalf("pause status = %d; body=%s", pauseResponse.Code, pauseResponse.Body.String())
	}
	if manager.pausedID != "greeting-abc12345" {
		t.Errorf("paused ID = %q", manager.pausedID)
	}

	updateRequest := httptest.NewRequest(
		http.MethodPatch,
		"/api/v1/schedules/greeting-abc12345",
		strings.NewReader(`{"name":"更新任务","input":{"name":"Bob"},"cron":"30 8 * * 1-5","timezone":"Asia/Shanghai","paused":true}`),
	)
	updateRequest.Host = "127.0.0.1:8080"
	updateRequest.Header.Set("Content-Type", "application/json")
	updateRequest.Header.Set("Origin", "http://127.0.0.1:8080")
	updateResponse := httptest.NewRecorder()
	handler.ServeHTTP(updateResponse, updateRequest)
	if updateResponse.Code != http.StatusOK {
		t.Fatalf("update status = %d; body=%s", updateResponse.Code, updateResponse.Body.String())
	}
	if manager.update.ID != "greeting-abc12345" || manager.update.Cron != "30 8 * * 1-5" {
		t.Errorf("update request = %#v", manager.update)
	}
}
