package handlers

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/DarioEB/logdeb"
	"github.com/gin-gonic/gin"

	"registry_dashboard_api/models/dto"
	"registry_dashboard_api/services"
)

// mockGCService is a test double for gcServicer.
type mockGCService struct {
	runGCFn      func() (time.Time, error)
	getStatusFn  func() dto.GCStatusResponse
	getConfigFn  func() (dto.GCConfigResponse, error)
	updateCfgFn  func(dto.UpdateGCConfigRequest) (dto.GCConfigResponse, error)
}

func (m *mockGCService) RunGC() (time.Time, error) {
	return m.runGCFn()
}

func (m *mockGCService) GetStatus() dto.GCStatusResponse {
	return m.getStatusFn()
}

func (m *mockGCService) GetConfig() (dto.GCConfigResponse, error) {
	return m.getConfigFn()
}

func (m *mockGCService) UpdateConfig(req dto.UpdateGCConfigRequest) (dto.GCConfigResponse, error) {
	return m.updateCfgFn(req)
}

// testGCLogger is a shared logger for GC handler tests.
var testGCHandlerLogger *logdeb.Logdeb

func init() {
	var err error
	testGCHandlerLogger, err = logdeb.New(logdeb.DefaultConfig())
	if err != nil {
		panic("failed to create test logger: " + err.Error())
	}
}

func testGCRouter(svc gcServicer) *gin.Engine {
	r := gin.New()
	h := NewGCHandler(svc, testGCHandlerLogger)
	r.POST("/api/gc/run", h.RunGC)
	r.GET("/api/gc/status", h.GetStatus)
	r.GET("/api/gcConfig", h.GetConfig)
	r.PUT("/api/gcConfig", h.UpdateConfig)
	return r
}

// ── RunGC tests ─────────────────────────────────────────────────────────────

func TestRunGC_Success_Returns202(t *testing.T) {
	svc := &mockGCService{
		runGCFn: func() (time.Time, error) { return time.Now().UTC(), nil },
	}

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/gc/run", nil)
	testGCRouter(svc).ServeHTTP(w, req)

	if w.Code != http.StatusAccepted {
		t.Errorf("expected 202, got %d (body: %s)", w.Code, w.Body.String())
	}

	var body dto.GCRunResponse
	if err := json.NewDecoder(w.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body.Status != "running" {
		t.Errorf("expected running, got %s", body.Status)
	}
}

func TestRunGC_AlreadyRunning_Returns409(t *testing.T) {
	svc := &mockGCService{
		runGCFn: func() (time.Time, error) { return time.Time{}, services.ErrGCAlreadyRunning },
	}

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/gc/run", nil)
	testGCRouter(svc).ServeHTTP(w, req)

	if w.Code != http.StatusConflict {
		t.Errorf("expected 409, got %d", w.Code)
	}
}

func TestRunGC_RegistryUnavailable_Returns503(t *testing.T) {
	svc := &mockGCService{
		runGCFn: func() (time.Time, error) { return time.Time{}, services.ErrRegistryUnavailable },
	}

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/gc/run", nil)
	testGCRouter(svc).ServeHTTP(w, req)

	if w.Code != http.StatusServiceUnavailable {
		t.Errorf("expected 503, got %d", w.Code)
	}
}

func TestRunGC_InternalError_Returns500(t *testing.T) {
	svc := &mockGCService{
		runGCFn: func() (time.Time, error) { return time.Time{}, errors.New("something broke") },
	}

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/gc/run", nil)
	testGCRouter(svc).ServeHTTP(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Errorf("expected 500, got %d", w.Code)
	}
}

// ── GetStatus tests ─────────────────────────────────────────────────────────

func TestGetStatus_Returns200(t *testing.T) {
	svc := &mockGCService{
		getStatusFn: func() dto.GCStatusResponse {
			return dto.GCStatusResponse{Status: "idle", LastRun: nil}
		},
	}

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/gc/status", nil)
	testGCRouter(svc).ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", w.Code)
	}

	var body dto.GCStatusResponse
	if err := json.NewDecoder(w.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body.Status != "idle" {
		t.Errorf("expected idle, got %s", body.Status)
	}
}

// ── GetConfig tests ─────────────────────────────────────────────────────────

func TestGetConfig_Returns200(t *testing.T) {
	svc := &mockGCService{
		getConfigFn: func() (dto.GCConfigResponse, error) {
			return dto.GCConfigResponse{Schedule: "0 3 * * *", Enabled: true}, nil
		},
	}

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/gcConfig", nil)
	testGCRouter(svc).ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", w.Code)
	}

	var body dto.GCConfigResponse
	if err := json.NewDecoder(w.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body.Schedule != "0 3 * * *" {
		t.Errorf("expected '0 3 * * *', got '%s'", body.Schedule)
	}
}

func TestGetConfig_Error_Returns500(t *testing.T) {
	svc := &mockGCService{
		getConfigFn: func() (dto.GCConfigResponse, error) {
			return dto.GCConfigResponse{}, errors.New("db error")
		},
	}

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/gcConfig", nil)
	testGCRouter(svc).ServeHTTP(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Errorf("expected 500, got %d", w.Code)
	}
}

// ── UpdateConfig tests ──────────────────────────────────────────────────────

func TestUpdateConfig_Success_Returns200(t *testing.T) {
	svc := &mockGCService{
		updateCfgFn: func(req dto.UpdateGCConfigRequest) (dto.GCConfigResponse, error) {
			return dto.GCConfigResponse{Schedule: *req.Schedule, Enabled: true}, nil
		},
	}

	body := `{"schedule": "0 0 * * 0", "enabled": true}`
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPut, "/api/gcConfig", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	testGCRouter(svc).ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d (body: %s)", w.Code, w.Body.String())
	}
}

func TestUpdateConfig_InvalidSchedule_Returns400(t *testing.T) {
	svc := &mockGCService{
		updateCfgFn: func(req dto.UpdateGCConfigRequest) (dto.GCConfigResponse, error) {
			return dto.GCConfigResponse{}, services.ErrInvalidSchedule
		},
	}

	body := `{"schedule": "bad cron"}`
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPut, "/api/gcConfig", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	testGCRouter(svc).ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", w.Code)
	}
}

func TestUpdateConfig_BadJSON_Returns400(t *testing.T) {
	svc := &mockGCService{}

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPut, "/api/gcConfig", strings.NewReader("not json"))
	req.Header.Set("Content-Type", "application/json")
	testGCRouter(svc).ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", w.Code)
	}
}

// ── Interface compliance ────────────────────────────────────────────────────

func TestGCService_SatisfiesGCServicer(t *testing.T) {
	var _ gcServicer = (*mockGCService)(nil)
}

