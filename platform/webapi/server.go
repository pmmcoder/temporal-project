package webapi

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io/fs"
	"log/slog"
	"mime"
	"net/http"
	"net/url"
	"path"
	"strconv"
	"strings"
	"time"

	"personal-workflow-platform/platform/approvals"
	"personal-workflow-platform/platform/catalog"
	"personal-workflow-platform/platform/runs"
	"personal-workflow-platform/platform/schedules"
	"personal-workflow-platform/platform/sdk"
	"personal-workflow-platform/platform/sdk/approval"
)

type Catalog interface {
	List(context.Context) ([]catalog.Definition, error)
	Get(context.Context, string) (catalog.Definition, error)
}

type Options struct {
	Catalog      Catalog
	Runs         RunStarter
	RunReader    RunReader
	Approvals    ApprovalManager
	Schedules    ScheduleManager
	AllowedHosts []string
	Assets       fs.FS
}

type RunStarter interface {
	Start(context.Context, runs.StartRequest) (runs.Run, error)
}

type RunReader interface {
	List(context.Context, runs.ListRequest) (runs.RunPage, error)
	Get(context.Context, runs.RunRef) (runs.RunDetail, error)
	Cancel(context.Context, runs.RunRef) error
	Signal(context.Context, runs.SignalRequest) error
}

type ApprovalManager interface {
	List(context.Context) ([]approvals.PendingApproval, error)
	Decide(context.Context, approvals.DecisionRequest) error
}

type ScheduleManager interface {
	List(context.Context) ([]schedules.Schedule, error)
	Create(context.Context, schedules.CreateRequest) (schedules.Schedule, error)
	Update(context.Context, schedules.UpdateRequest) error
	Pause(context.Context, string) error
	Resume(context.Context, string) error
	Delete(context.Context, string) error
}

type Server struct {
	catalog   Catalog
	runs      RunStarter
	runReader RunReader
	approvals ApprovalManager
	schedules ScheduleManager
}

func New(options Options) (http.Handler, error) {
	if options.Catalog == nil {
		return nil, errors.New("webapi: catalog is required")
	}

	server := &Server{
		catalog:   options.Catalog,
		runs:      options.Runs,
		runReader: options.RunReader,
		approvals: options.Approvals,
		schedules: options.Schedules,
	}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/v1/healthz", server.handleHealth)
	mux.HandleFunc("GET /api/v1/workflows", server.handleWorkflows)
	mux.HandleFunc("POST /api/v1/workflows/{id}/runs", server.handleStartWorkflow)
	mux.HandleFunc("GET /api/v1/runs", server.handleListRuns)
	mux.HandleFunc("GET /api/v1/runs/{workflowID}/{runID}", server.handleGetRun)
	mux.HandleFunc("POST /api/v1/runs/{workflowID}/{runID}/cancel", server.handleCancelRun)
	mux.HandleFunc("POST /api/v1/runs/{workflowID}/{runID}/signals/{name}", server.handleSignalRun)
	mux.HandleFunc("GET /api/v1/approvals", server.handleListApprovals)
	mux.HandleFunc("POST /api/v1/approvals/{workflowID}/{runID}/{approvalID}/decision", server.handleApprovalDecision)
	mux.HandleFunc("GET /api/v1/schedules", server.handleListSchedules)
	mux.HandleFunc("POST /api/v1/schedules", server.handleCreateSchedule)
	mux.HandleFunc("PATCH /api/v1/schedules/{id}", server.handleUpdateSchedule)
	mux.HandleFunc("POST /api/v1/schedules/{id}/pause", server.handlePauseSchedule)
	mux.HandleFunc("POST /api/v1/schedules/{id}/resume", server.handleResumeSchedule)
	mux.HandleFunc("DELETE /api/v1/schedules/{id}", server.handleDeleteSchedule)
	if options.Assets != nil {
		mux.Handle("GET /", spaHandler(options.Assets))
	} else {
		mux.HandleFunc("GET /", server.handleIndex)
	}

	allowedHosts := options.AllowedHosts
	if len(allowedHosts) == 0 {
		allowedHosts = []string{"127.0.0.1:8080", "localhost:8080"}
	}

	return requestIDMiddleware(securityMiddleware(mux, allowedHosts)), nil
}

func (s *Server) handleUpdateSchedule(w http.ResponseWriter, r *http.Request) {
	if s.schedules == nil {
		writeError(w, http.StatusServiceUnavailable, "TEMPORAL_UNAVAILABLE", "schedules are not configured")
		return
	}
	if contentType := r.Header.Get("Content-Type"); !strings.HasPrefix(contentType, "application/json") {
		writeError(w, http.StatusUnsupportedMediaType, "UNSUPPORTED_MEDIA_TYPE", "Content-Type must be application/json")
		return
	}
	var body struct {
		Name     string          `json:"name"`
		Input    json.RawMessage `json:"input"`
		Cron     string          `json:"cron"`
		Timezone string          `json:"timezone"`
		Paused   bool            `json:"paused"`
	}
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_INPUT", "request body is invalid JSON")
		return
	}
	err := s.schedules.Update(r.Context(), schedules.UpdateRequest{
		ID:       r.PathValue("id"),
		Name:     body.Name,
		Input:    body.Input,
		Cron:     body.Cron,
		Timezone: body.Timezone,
		Paused:   body.Paused,
	})
	if err != nil {
		writeScheduleError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "updated"})
}

func (s *Server) handleSignalRun(w http.ResponseWriter, r *http.Request) {
	if s.runReader == nil {
		writeError(w, http.StatusServiceUnavailable, "TEMPORAL_UNAVAILABLE", "workflow runs are not configured")
		return
	}
	if contentType := r.Header.Get("Content-Type"); !strings.HasPrefix(contentType, "application/json") {
		writeError(w, http.StatusUnsupportedMediaType, "UNSUPPORTED_MEDIA_TYPE", "Content-Type must be application/json")
		return
	}
	var body struct {
		Input json.RawMessage `json:"input"`
	}
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_INPUT", "request body is invalid JSON")
		return
	}
	err := s.runReader.Signal(r.Context(), runs.SignalRequest{
		WorkflowID: r.PathValue("workflowID"),
		RunID:      r.PathValue("runID"),
		Name:       r.PathValue("name"),
		Input:      body.Input,
	})
	if err != nil {
		writeRunError(w, err)
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]string{"status": "signal_submitted"})
}

func (s *Server) handleListSchedules(w http.ResponseWriter, r *http.Request) {
	if s.schedules == nil {
		writeError(w, http.StatusServiceUnavailable, "TEMPORAL_UNAVAILABLE", "schedules are not configured")
		return
	}
	items, err := s.schedules.List(r.Context())
	if err != nil {
		writeScheduleError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"schedules": items})
}

func (s *Server) handleCreateSchedule(w http.ResponseWriter, r *http.Request) {
	if s.schedules == nil {
		writeError(w, http.StatusServiceUnavailable, "TEMPORAL_UNAVAILABLE", "schedules are not configured")
		return
	}
	if contentType := r.Header.Get("Content-Type"); !strings.HasPrefix(contentType, "application/json") {
		writeError(w, http.StatusUnsupportedMediaType, "UNSUPPORTED_MEDIA_TYPE", "Content-Type must be application/json")
		return
	}
	var body struct {
		Name       string          `json:"name"`
		WorkflowID string          `json:"workflowId"`
		Input      json.RawMessage `json:"input"`
		Cron       string          `json:"cron"`
		Timezone   string          `json:"timezone"`
		Paused     bool            `json:"paused"`
	}
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_INPUT", "request body is invalid JSON")
		return
	}
	schedule, err := s.schedules.Create(r.Context(), schedules.CreateRequest{
		Name:         body.Name,
		DefinitionID: body.WorkflowID,
		Input:        body.Input,
		Cron:         body.Cron,
		Timezone:     body.Timezone,
		Paused:       body.Paused,
	})
	if err != nil {
		writeScheduleError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, schedule)
}

func (s *Server) handlePauseSchedule(w http.ResponseWriter, r *http.Request) {
	if s.schedules == nil {
		writeError(w, http.StatusServiceUnavailable, "TEMPORAL_UNAVAILABLE", "schedules are not configured")
		return
	}
	s.changeScheduleState(w, r, s.schedules.Pause)
}

func (s *Server) handleResumeSchedule(w http.ResponseWriter, r *http.Request) {
	if s.schedules == nil {
		writeError(w, http.StatusServiceUnavailable, "TEMPORAL_UNAVAILABLE", "schedules are not configured")
		return
	}
	s.changeScheduleState(w, r, s.schedules.Resume)
}

func (s *Server) handleDeleteSchedule(w http.ResponseWriter, r *http.Request) {
	if s.schedules == nil {
		writeError(w, http.StatusServiceUnavailable, "TEMPORAL_UNAVAILABLE", "schedules are not configured")
		return
	}
	s.changeScheduleState(w, r, s.schedules.Delete)
}

func (s *Server) changeScheduleState(
	w http.ResponseWriter,
	r *http.Request,
	change func(context.Context, string) error,
) {
	if s.schedules == nil {
		writeError(w, http.StatusServiceUnavailable, "TEMPORAL_UNAVAILABLE", "schedules are not configured")
		return
	}
	if err := change(r.Context(), r.PathValue("id")); err != nil {
		writeScheduleError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *Server) handleListApprovals(w http.ResponseWriter, r *http.Request) {
	if s.approvals == nil {
		writeError(w, http.StatusServiceUnavailable, "TEMPORAL_UNAVAILABLE", "approvals are not configured")
		return
	}
	items, err := s.approvals.List(r.Context())
	if err != nil {
		writeApprovalError(w, err)
		return
	}
	type responseItem struct {
		ApprovalID   string           `json:"approvalId"`
		WorkflowID   string           `json:"workflowId"`
		RunID        string           `json:"runId"`
		WorkflowType string           `json:"workflowType"`
		Title        string           `json:"title"`
		Summary      string           `json:"summary"`
		Fields       []approval.Field `json:"fields,omitempty"`
		AllowComment bool             `json:"allowComment"`
		CreatedAt    time.Time        `json:"createdAt"`
		Deadline     time.Time        `json:"deadline"`
	}
	result := make([]responseItem, 0, len(items))
	for _, item := range items {
		result = append(result, responseItem{
			ApprovalID:   item.ID,
			WorkflowID:   item.WorkflowID,
			RunID:        item.RunID,
			WorkflowType: item.WorkflowType,
			Title:        item.Title,
			Summary:      item.Summary,
			Fields:       item.Fields,
			AllowComment: item.AllowComment,
			CreatedAt:    item.CreatedAt,
			Deadline:     item.Deadline,
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{"approvals": result})
}

func (s *Server) handleApprovalDecision(w http.ResponseWriter, r *http.Request) {
	if s.approvals == nil {
		writeError(w, http.StatusServiceUnavailable, "TEMPORAL_UNAVAILABLE", "approvals are not configured")
		return
	}
	if contentType := r.Header.Get("Content-Type"); !strings.HasPrefix(contentType, "application/json") {
		writeError(w, http.StatusUnsupportedMediaType, "UNSUPPORTED_MEDIA_TYPE", "Content-Type must be application/json")
		return
	}

	var body struct {
		Decision approval.Decision `json:"decision"`
		Comment  string            `json:"comment"`
	}
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_INPUT", "request body is invalid JSON")
		return
	}

	err := s.approvals.Decide(r.Context(), approvals.DecisionRequest{
		WorkflowID: r.PathValue("workflowID"),
		RunID:      r.PathValue("runID"),
		ApprovalID: r.PathValue("approvalID"),
		Decision:   body.Decision,
		Comment:    body.Comment,
	})
	if err != nil {
		writeApprovalError(w, err)
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]string{"status": "decision_submitted"})
}

func (s *Server) handleListRuns(w http.ResponseWriter, r *http.Request) {
	if s.runReader == nil {
		writeError(w, http.StatusServiceUnavailable, "TEMPORAL_UNAVAILABLE", "workflow runs are not configured")
		return
	}

	pageSize := 0
	if value := r.URL.Query().Get("pageSize"); value != "" {
		parsed, err := strconv.Atoi(value)
		if err != nil || parsed < 1 || parsed > 100 {
			writeError(w, http.StatusBadRequest, "INVALID_INPUT", "pageSize must be between 1 and 100")
			return
		}
		pageSize = parsed
	}
	status := runs.Status(r.URL.Query().Get("status"))
	if status != "" && !validStatus(status) {
		writeError(w, http.StatusBadRequest, "INVALID_INPUT", "status is invalid")
		return
	}
	var from time.Time
	if value := r.URL.Query().Get("from"); value != "" {
		parsed, err := time.Parse(time.RFC3339, value)
		if err != nil {
			writeError(w, http.StatusBadRequest, "INVALID_INPUT", "from must be an RFC3339 timestamp")
			return
		}
		from = parsed
	}
	var to time.Time
	if value := r.URL.Query().Get("to"); value != "" {
		parsed, err := time.Parse(time.RFC3339, value)
		if err != nil {
			writeError(w, http.StatusBadRequest, "INVALID_INPUT", "to must be an RFC3339 timestamp")
			return
		}
		to = parsed
	}

	page, err := s.runReader.List(r.Context(), runs.ListRequest{
		Workflow:  r.URL.Query().Get("workflow"),
		Status:    status,
		PageSize:  pageSize,
		PageToken: r.URL.Query().Get("pageToken"),
		From:      from,
		To:        to,
	})
	if err != nil {
		writeRunError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, page)
}

func (s *Server) handleGetRun(w http.ResponseWriter, r *http.Request) {
	if s.runReader == nil {
		writeError(w, http.StatusServiceUnavailable, "TEMPORAL_UNAVAILABLE", "workflow runs are not configured")
		return
	}

	detail, err := s.runReader.Get(r.Context(), runs.RunRef{
		WorkflowID: r.PathValue("workflowID"),
		RunID:      r.PathValue("runID"),
	})
	if err != nil {
		writeRunError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, detail)
}

func (s *Server) handleCancelRun(w http.ResponseWriter, r *http.Request) {
	if s.runReader == nil {
		writeError(w, http.StatusServiceUnavailable, "TEMPORAL_UNAVAILABLE", "workflow runs are not configured")
		return
	}

	err := s.runReader.Cancel(r.Context(), runs.RunRef{
		WorkflowID: r.PathValue("workflowID"),
		RunID:      r.PathValue("runID"),
	})
	if err != nil {
		writeRunError(w, err)
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]string{"status": "cancellation_requested"})
}

func (s *Server) handleStartWorkflow(w http.ResponseWriter, r *http.Request) {
	if s.runs == nil {
		writeError(w, http.StatusServiceUnavailable, "TEMPORAL_UNAVAILABLE", "workflow execution is not configured")
		return
	}
	if contentType := r.Header.Get("Content-Type"); !strings.HasPrefix(contentType, "application/json") {
		writeError(w, http.StatusUnsupportedMediaType, "UNSUPPORTED_MEDIA_TYPE", "Content-Type must be application/json")
		return
	}

	var request struct {
		Input          json.RawMessage `json:"input"`
		IdempotencyKey string          `json:"idempotencyKey"`
	}
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&request); err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_INPUT", "request body is invalid JSON")
		return
	}

	run, err := s.runs.Start(r.Context(), runs.StartRequest{
		DefinitionID:   r.PathValue("id"),
		Input:          request.Input,
		IdempotencyKey: request.IdempotencyKey,
	})
	if err != nil {
		slog.ErrorContext(
			r.Context(),
			"start workflow failed",
			"workflow_definition", requestPathID(r),
			"error", err,
		)
		writeRunError(w, err)
		return
	}

	status := http.StatusCreated
	if run.Existing {
		status = http.StatusOK
	}
	writeJSON(w, status, run)
}

func requestPathID(r *http.Request) string {
	return r.PathValue("id")
}

func (s *Server) handleHealth(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *Server) handleWorkflows(w http.ResponseWriter, r *http.Request) {
	definitions, err := s.catalog.List(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "unable to list workflows")
		return
	}

	type workflowResponse struct {
		ID           string          `json:"id"`
		DisplayName  string          `json:"displayName"`
		Description  string          `json:"description"`
		InputSchema  json.RawMessage `json:"inputSchema"`
		ExampleInput json.RawMessage `json:"exampleInput"`
		Signals      []sdk.SignalDef `json:"signals,omitempty"`
	}

	workflows := make([]workflowResponse, 0, len(definitions))
	for _, definition := range definitions {
		workflows = append(workflows, workflowResponse{
			ID:           definition.ID,
			DisplayName:  definition.DisplayName,
			Description:  definition.Description,
			InputSchema:  definition.InputSchema,
			ExampleInput: definition.ExampleInput,
			Signals:      definition.Signals,
		})
	}

	writeJSON(w, http.StatusOK, struct {
		Workflows []workflowResponse `json:"workflows"`
	}{Workflows: workflows})
}

func (s *Server) handleIndex(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write([]byte("<!doctype html><html><body><h1>个人工作流平台</h1><p>WebUI 正在建设中。</p></body></html>"))
}

func spaHandler(assets fs.FS) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/api/") {
			writeError(w, http.StatusNotFound, "NOT_FOUND", "API route not found")
			return
		}

		name := strings.TrimPrefix(path.Clean(r.URL.Path), "/")
		if name == "" || name == "." {
			name = "index.html"
		}
		data, err := fs.ReadFile(assets, name)
		if err != nil {
			if path.Ext(name) != "" {
				http.NotFound(w, r)
				return
			}
			name = "index.html"
			data, err = fs.ReadFile(assets, name)
			if err != nil {
				http.NotFound(w, r)
				return
			}
		}

		if contentType := mime.TypeByExtension(path.Ext(name)); contentType != "" {
			w.Header().Set("Content-Type", contentType)
		}
		if strings.HasPrefix(name, "assets/") {
			w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
		} else {
			w.Header().Set("Cache-Control", "no-cache")
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(data)
	})
}

func requestIDMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestID := r.Header.Get("X-Request-ID")
		if requestID == "" {
			requestID = newRequestID()
		}
		w.Header().Set("X-Request-ID", requestID)
		next.ServeHTTP(w, r)
	})
}

func securityMiddleware(next http.Handler, allowedHosts []string) http.Handler {
	hosts := make(map[string]struct{}, len(allowedHosts))
	for _, host := range allowedHosts {
		hosts[strings.ToLower(strings.TrimSpace(host))] = struct{}{}
	}

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("Referrer-Policy", "no-referrer")

		if _, ok := hosts[strings.ToLower(r.Host)]; !ok {
			writeError(w, http.StatusForbidden, "FORBIDDEN_ORIGIN", "request host is not allowed")
			return
		}

		if isWriteMethod(r.Method) {
			origin := r.Header.Get("Origin")
			if origin != "" {
				parsed, err := url.Parse(origin)
				if err != nil || parsed.Host != r.Host ||
					(parsed.Scheme != "http" && parsed.Scheme != "https") {
					writeError(w, http.StatusForbidden, "FORBIDDEN_ORIGIN", "request origin is not allowed")
					return
				}
			} else if r.Header.Get("Sec-Fetch-Site") != "same-origin" {
				writeError(w, http.StatusForbidden, "FORBIDDEN_ORIGIN", "same-origin request metadata is required")
				return
			}
		}

		next.ServeHTTP(w, r)
	})
}

func isWriteMethod(method string) bool {
	switch method {
	case http.MethodPost, http.MethodPatch, http.MethodPut, http.MethodDelete:
		return true
	default:
		return false
	}
}

func newRequestID() string {
	var bytes [12]byte
	if _, err := rand.Read(bytes[:]); err != nil {
		return "request-id-unavailable"
	}
	return hex.EncodeToString(bytes[:])
}

func writeError(w http.ResponseWriter, status int, code, message string) {
	writeJSON(w, status, struct {
		Error struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}{
		Error: struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		}{
			Code:    code,
			Message: message,
		},
	})
}

func writeRunError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, runs.ErrInvalidInput):
		writeError(w, http.StatusBadRequest, "INVALID_INPUT", err.Error())
	case errors.Is(err, runs.ErrNotFound):
		writeError(w, http.StatusNotFound, "WORKFLOW_NOT_FOUND", "workflow definition not found")
	case errors.Is(err, runs.ErrInputConflict):
		writeError(w, http.StatusConflict, "RUN_ALREADY_EXISTS", err.Error())
	case errors.Is(err, runs.ErrRunNotFound):
		writeError(w, http.StatusNotFound, "RUN_NOT_FOUND", "workflow run not found")
	case errors.Is(err, runs.ErrInvalidState):
		writeError(w, http.StatusConflict, "INVALID_STATE", err.Error())
	case errors.Is(err, runs.ErrSignalNotDefined):
		writeError(w, http.StatusUnprocessableEntity, "SIGNAL_NOT_DEFINED", "signal is not declared by workflow")
	case errors.Is(err, runs.ErrTemporalUnavailable):
		writeError(w, http.StatusServiceUnavailable, "TEMPORAL_UNAVAILABLE", "Temporal is unavailable")
	default:
		writeError(w, http.StatusServiceUnavailable, "TEMPORAL_UNAVAILABLE", "unable to start workflow")
	}
}

func writeApprovalError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, approvals.ErrInvalidInput):
		writeError(w, http.StatusBadRequest, "INVALID_INPUT", err.Error())
	case errors.Is(err, approvals.ErrNotFound):
		writeError(w, http.StatusNotFound, "APPROVAL_NOT_FOUND", "approval not found")
	case errors.Is(err, approvals.ErrInvalidState):
		writeError(w, http.StatusConflict, "INVALID_STATE", "workflow is not running")
	case errors.Is(err, approvals.ErrTemporalUnavailable):
		writeError(w, http.StatusServiceUnavailable, "TEMPORAL_UNAVAILABLE", "Temporal is unavailable")
	default:
		writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "unable to process approval")
	}
}

func writeScheduleError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, schedules.ErrInvalidInput):
		writeError(w, http.StatusBadRequest, "INVALID_INPUT", err.Error())
	case errors.Is(err, schedules.ErrNotFound):
		writeError(w, http.StatusNotFound, "SCHEDULE_NOT_FOUND", "schedule or workflow not found")
	case errors.Is(err, schedules.ErrTemporalUnavailable):
		writeError(w, http.StatusServiceUnavailable, "TEMPORAL_UNAVAILABLE", "Temporal is unavailable")
	default:
		writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "unable to process schedule")
	}
}

func validStatus(status runs.Status) bool {
	switch status {
	case runs.StatusRunning,
		runs.StatusSucceeded,
		runs.StatusFailed,
		runs.StatusCanceled,
		runs.StatusTerminated,
		runs.StatusTimedOut,
		runs.StatusContinued:
		return true
	default:
		return false
	}
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}
