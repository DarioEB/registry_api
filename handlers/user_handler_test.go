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

// mockUserService implements userServicer for testing.
type mockUserService struct {
	listUsersFn      func() ([]dto.UserResponse, error)
	createUserFn     func(dto.CreateUserRequest) (dto.UserResponse, error)
	updatePasswordFn func(string, dto.UpdatePasswordRequest) (dto.UserResponse, error)
	deleteUserFn     func(string) error
}

func (m *mockUserService) ListUsers() ([]dto.UserResponse, error) {
	return m.listUsersFn()
}

func (m *mockUserService) CreateUser(req dto.CreateUserRequest) (dto.UserResponse, error) {
	return m.createUserFn(req)
}

func (m *mockUserService) UpdatePassword(username string, req dto.UpdatePasswordRequest) (dto.UserResponse, error) {
	return m.updatePasswordFn(username, req)
}

func (m *mockUserService) DeleteUser(username string) error {
	return m.deleteUserFn(username)
}

var testUserHandlerLogger *logdeb.Logdeb

func init() {
	var err error
	testUserHandlerLogger, err = logdeb.New(logdeb.DefaultConfig())
	if err != nil {
		panic("failed to create test logger: " + err.Error())
	}
}

func testUserRouter(svc userServicer) *gin.Engine {
	r := gin.New()
	h := NewUserHandler(svc, testUserHandlerLogger)
	r.GET("/api/users", h.ListUsers)
	r.POST("/api/users", h.CreateUser)
	r.PUT("/api/users/:username", h.UpdatePassword)
	r.DELETE("/api/users/:username", h.DeleteUser)
	return r
}

func TestListUsers_Success(t *testing.T) {
	now := time.Now().UTC()
	svc := &mockUserService{
		listUsersFn: func() ([]dto.UserResponse, error) {
			return []dto.UserResponse{
				{ID: 1, Username: "admin", CreatedAt: now},
				{ID: 2, Username: "dev1", CreatedAt: now},
			}, nil
		},
	}
	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/api/users", nil)
	testUserRouter(svc).ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	var users []dto.UserResponse
	json.Unmarshal(w.Body.Bytes(), &users)
	if len(users) != 2 {
		t.Fatalf("expected 2 users, got %d", len(users))
	}
}

func TestListUsers_Empty(t *testing.T) {
	svc := &mockUserService{
		listUsersFn: func() ([]dto.UserResponse, error) {
			return []dto.UserResponse{}, nil
		},
	}
	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/api/users", nil)
	testUserRouter(svc).ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	if body := w.Body.String(); body != "[]" {
		t.Fatalf("expected empty array, got %s", body)
	}
}

func TestCreateUser_Success(t *testing.T) {
	now := time.Now().UTC()
	svc := &mockUserService{
		createUserFn: func(req dto.CreateUserRequest) (dto.UserResponse, error) {
			return dto.UserResponse{ID: 1, Username: req.Username, CreatedAt: now}, nil
		},
	}
	body := `{"username":"newuser","password":"secret123"}`
	w := httptest.NewRecorder()
	req, _ := http.NewRequest("POST", "/api/users", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	testUserRouter(svc).ServeHTTP(w, req)

	if w.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", w.Code, w.Body.String())
	}
	var user dto.UserResponse
	json.Unmarshal(w.Body.Bytes(), &user)
	if user.Username != "newuser" {
		t.Fatalf("expected username 'newuser', got '%s'", user.Username)
	}
}

func TestCreateUser_UsernameExists(t *testing.T) {
	svc := &mockUserService{
		createUserFn: func(req dto.CreateUserRequest) (dto.UserResponse, error) {
			return dto.UserResponse{}, services.ErrUsernameExists
		},
	}
	body := `{"username":"admin","password":"secret123"}`
	w := httptest.NewRecorder()
	req, _ := http.NewRequest("POST", "/api/users", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	testUserRouter(svc).ServeHTTP(w, req)

	if w.Code != http.StatusUnprocessableEntity {
		t.Fatalf("expected 422, got %d", w.Code)
	}
}

func TestCreateUser_BadJSON(t *testing.T) {
	svc := &mockUserService{}
	body := `{"username":""}` // missing required password, empty username
	w := httptest.NewRecorder()
	req, _ := http.NewRequest("POST", "/api/users", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	testUserRouter(svc).ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", w.Code, w.Body.String())
	}
}

func TestUpdatePassword_Success(t *testing.T) {
	now := time.Now().UTC()
	svc := &mockUserService{
		updatePasswordFn: func(username string, req dto.UpdatePasswordRequest) (dto.UserResponse, error) {
			return dto.UserResponse{ID: 1, Username: username, CreatedAt: now}, nil
		},
	}
	body := `{"newPassword":"newpass123"}`
	w := httptest.NewRecorder()
	req, _ := http.NewRequest("PUT", "/api/users/admin", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	testUserRouter(svc).ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
}

func TestUpdatePassword_NotFound(t *testing.T) {
	svc := &mockUserService{
		updatePasswordFn: func(username string, req dto.UpdatePasswordRequest) (dto.UserResponse, error) {
			return dto.UserResponse{}, services.ErrUserNotFound
		},
	}
	body := `{"newPassword":"newpass123"}`
	w := httptest.NewRecorder()
	req, _ := http.NewRequest("PUT", "/api/users/ghost", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	testUserRouter(svc).ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", w.Code)
	}
}

func TestDeleteUser_Success(t *testing.T) {
	svc := &mockUserService{
		deleteUserFn: func(username string) error {
			return nil
		},
	}
	w := httptest.NewRecorder()
	req, _ := http.NewRequest("DELETE", "/api/users/dev1", nil)
	testUserRouter(svc).ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	var resp dto.DeleteUserResponse
	json.Unmarshal(w.Body.Bytes(), &resp)
	if resp.Message != "user deleted" {
		t.Fatalf("expected 'user deleted', got '%s'", resp.Message)
	}
}

func TestDeleteUser_NotFound(t *testing.T) {
	svc := &mockUserService{
		deleteUserFn: func(username string) error {
			return services.ErrUserNotFound
		},
	}
	w := httptest.NewRecorder()
	req, _ := http.NewRequest("DELETE", "/api/users/ghost", nil)
	testUserRouter(svc).ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", w.Code)
	}
}

// Verify that internal errors map to 500.
func TestListUsers_InternalError(t *testing.T) {
	svc := &mockUserService{
		listUsersFn: func() ([]dto.UserResponse, error) {
			return nil, errors.New("db connection lost")
		},
	}
	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/api/users", nil)
	testUserRouter(svc).ServeHTTP(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d", w.Code)
	}
}
