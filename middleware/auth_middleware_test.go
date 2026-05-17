package middleware

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"

	"registry_dashboard_api/services"
)

func init() {
	gin.SetMode(gin.TestMode)
}

// testRouter wraps a protected endpoint behind AuthMiddleware for test requests.
func testRouter(svc tokenValidator) *gin.Engine {
	r := gin.New()
	r.Use(AuthMiddleware(svc))
	r.GET("/protected", func(c *gin.Context) {
		username, _ := c.Get("username")
		c.JSON(200, gin.H{"username": username})
	})
	return r
}

func TestAuthMiddleware_MissingCookie_Returns401(t *testing.T) {
	svc := services.NewAuthService(nil, "test-secret-min-32-bytes-padding!!")
	r := testRouter(svc)

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/protected", nil)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", w.Code)
	}
	assertErrorBody(t, w, "unauthorized")
}

func TestAuthMiddleware_InvalidToken_Returns401(t *testing.T) {
	svc := services.NewAuthService(nil, "test-secret-min-32-bytes-padding!!")
	r := testRouter(svc)

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/protected", nil)
	req.AddCookie(&http.Cookie{Name: "auth_token", Value: "not.a.valid.jwt"})
	r.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", w.Code)
	}
	assertErrorBody(t, w, "unauthorized")
}

func TestAuthMiddleware_ValidToken_Passes(t *testing.T) {
	svc := services.NewAuthService(nil, "test-secret-min-32-bytes-padding!!")
	token, err := svc.GenerateToken("alice")
	if err != nil {
		t.Fatalf("GenerateToken error: %v", err)
	}

	r := testRouter(svc)

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/protected", nil)
	req.AddCookie(&http.Cookie{Name: "auth_token", Value: token})
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d (body: %s)", w.Code, w.Body.String())
	}

	var body map[string]string
	if err := json.NewDecoder(w.Body).Decode(&body); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if body["username"] != "alice" {
		t.Errorf("expected username 'alice', got %q", body["username"])
	}
}

func TestAuthMiddleware_WrongSecret_Returns401(t *testing.T) {
	signer := services.NewAuthService(nil, "correct-secret-min-32-bytes-!!!")
	verifier := services.NewAuthService(nil, "wrong-secret-min-32-bytes-padding")

	token, err := signer.GenerateToken("alice")
	if err != nil {
		t.Fatalf("GenerateToken error: %v", err)
	}

	r := testRouter(verifier)

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/protected", nil)
	req.AddCookie(&http.Cookie{Name: "auth_token", Value: token})
	r.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", w.Code)
	}
}

// assertErrorBody checks that the response body contains {"error": "<msg>"}.
func assertErrorBody(t *testing.T, w *httptest.ResponseRecorder, msg string) {
	t.Helper()
	var body map[string]string
	if err := json.NewDecoder(w.Body).Decode(&body); err != nil {
		t.Fatalf("failed to decode error body: %v", err)
	}
	if body["error"] != msg {
		t.Errorf("expected error %q, got %q", msg, body["error"])
	}
}
