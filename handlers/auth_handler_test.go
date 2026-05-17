package handlers

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"

	"registry_dashboard_api/models"
	"registry_dashboard_api/services"
)

func init() {
	gin.SetMode(gin.TestMode)
}

// mockAuthService is a test double for authServicer.
type mockAuthService struct {
	validateFn func(username, password string) (*models.User, error)
	generateFn func(username string) (string, error)
}

func (m *mockAuthService) ValidateCredentials(username, password string) (*models.User, error) {
	return m.validateFn(username, password)
}

func (m *mockAuthService) GenerateToken(username string) (string, error) {
	return m.generateFn(username)
}

// testAuthRouter wires AuthHandler to a minimal Gin engine for testing.
func testAuthRouter(svc authServicer) *gin.Engine {
	r := gin.New()
	h := NewAuthHandler(svc, false) // cookieSecure=false in tests
	r.POST("/api/auth/login", h.Login)
	r.POST("/api/auth/logout", h.Logout)
	return r
}

func jsonBody(t *testing.T, v any) *bytes.Buffer {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("failed to marshal body: %v", err)
	}
	return bytes.NewBuffer(b)
}

// ── Login tests ───────────────────────────────────────────────────────────────

func TestLogin_MalformedJSON_Returns400(t *testing.T) {
	svc := &mockAuthService{}
	r := testAuthRouter(svc)

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/auth/login", bytes.NewBufferString("not-json"))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", w.Code)
	}
	assertJSONError(t, w, "invalid request")
}

func TestLogin_MissingFields_Returns400(t *testing.T) {
	svc := &mockAuthService{}
	r := testAuthRouter(svc)

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/auth/login",
		jsonBody(t, map[string]string{"username": "alice"}))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", w.Code)
	}
}

func TestLogin_InvalidCredentials_Returns401(t *testing.T) {
	svc := &mockAuthService{
		validateFn: func(_, _ string) (*models.User, error) {
			return nil, services.ErrInvalidCredentials
		},
	}
	r := testAuthRouter(svc)

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/auth/login",
		jsonBody(t, map[string]string{"username": "alice", "password": "wrong"}))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", w.Code)
	}
	assertJSONError(t, w, "invalid credentials")
}

// TestLogin_DBError_Returns500 verifies that infrastructure errors are NOT reported
// as 401 (which would mislead the operator into thinking their password is wrong).
func TestLogin_DBError_Returns500(t *testing.T) {
	svc := &mockAuthService{
		validateFn: func(_, _ string) (*models.User, error) {
			return nil, fmt.Errorf("database error: connection refused")
		},
	}
	r := testAuthRouter(svc)

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/auth/login",
		jsonBody(t, map[string]string{"username": "alice", "password": "pass"}))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Errorf("expected 500 for DB error, got %d (body: %s)", w.Code, w.Body.String())
	}
	assertJSONError(t, w, "internal server error")
}

func TestLogin_ValidCredentials_Returns200WithCookie(t *testing.T) {
	svc := &mockAuthService{
		validateFn: func(_, _ string) (*models.User, error) {
			return &models.User{Username: "alice"}, nil
		},
		generateFn: func(_ string) (string, error) {
			return "mock.jwt.token", nil
		},
	}
	r := testAuthRouter(svc)

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/auth/login",
		jsonBody(t, map[string]string{"username": "alice", "password": "correct"}))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d (body: %s)", w.Code, w.Body.String())
	}

	authCookie := findCookie(t, w, "auth_token")

	if authCookie.Value != "mock.jwt.token" {
		t.Errorf("expected cookie value 'mock.jwt.token', got %q", authCookie.Value)
	}
	if !authCookie.HttpOnly {
		t.Error("expected auth_token cookie to be HttpOnly")
	}
	if authCookie.MaxAge != 3600 {
		t.Errorf("expected MaxAge 3600, got %d", authCookie.MaxAge)
	}
	if authCookie.SameSite != http.SameSiteLaxMode {
		t.Errorf("expected SameSite=Lax, got %v", authCookie.SameSite)
	}
}

func TestLogin_TokenGenerationFailure_Returns500(t *testing.T) {
	svc := &mockAuthService{
		validateFn: func(_, _ string) (*models.User, error) {
			return &models.User{Username: "alice"}, nil
		},
		generateFn: func(_ string) (string, error) {
			return "", errors.New("signing failed")
		},
	}
	r := testAuthRouter(svc)

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/auth/login",
		jsonBody(t, map[string]string{"username": "alice", "password": "correct"}))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Errorf("expected 500, got %d", w.Code)
	}
	assertJSONError(t, w, "internal server error")
}

// ── Logout tests ──────────────────────────────────────────────────────────────

func TestLogout_Returns200AndClearsCookie(t *testing.T) {
	svc := &mockAuthService{}
	r := testAuthRouter(svc)

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/auth/logout", nil)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", w.Code)
	}

	authCookie := findCookie(t, w, "auth_token")

	if authCookie.MaxAge >= 0 {
		t.Errorf("expected MaxAge < 0 to delete cookie, got %d", authCookie.MaxAge)
	}
	if authCookie.SameSite != http.SameSiteLaxMode {
		t.Errorf("expected SameSite=Lax, got %v", authCookie.SameSite)
	}
}

// ── Helpers ───────────────────────────────────────────────────────────────────

// findCookie finds a named Set-Cookie header in the response, failing the test if absent.
func findCookie(t *testing.T, w *httptest.ResponseRecorder, name string) *http.Cookie {
	t.Helper()
	for _, c := range w.Result().Cookies() {
		if c.Name == name {
			return c
		}
	}
	t.Fatalf("expected %q cookie in response, got none (cookies: %v)", name, w.Result().Cookies())
	return nil
}

// assertJSONError checks that the response body has {"error": "<msg>"}.
func assertJSONError(t *testing.T, w *httptest.ResponseRecorder, msg string) {
	t.Helper()
	var body map[string]string
	if err := json.NewDecoder(w.Body).Decode(&body); err != nil {
		t.Fatalf("failed to decode body: %v", err)
	}
	if body["error"] != msg {
		t.Errorf("expected error %q, got %q", msg, body["error"])
	}
}
