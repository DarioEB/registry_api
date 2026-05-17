package handlers

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/DarioEB/logdeb"
	"github.com/gin-gonic/gin"

	"registry_dashboard_api/models/dto"
	"registry_dashboard_api/services"
)

// mockImageService is a test double for imageServicer.
type mockImageService struct {
	getImagesFn    func() ([]dto.ImageListItem, error)
	getImageTagsFn func(imageName string) ([]dto.ImageTag, error)
}

func (m *mockImageService) GetImages() ([]dto.ImageListItem, error) {
	return m.getImagesFn()
}

func (m *mockImageService) GetImageTags(imageName string) ([]dto.ImageTag, error) {
	return m.getImageTagsFn(imageName)
}

// testImageLogger is a shared logger for all image handler tests, avoiding one
// leaked logdeb instance per test invocation.
var testImageLogger *logdeb.Logdeb

func init() {
	var err error
	testImageLogger, err = logdeb.New(logdeb.DefaultConfig())
	if err != nil {
		panic("failed to create test logger: " + err.Error())
	}
}

// testImageRouter wires ImageHandler to a minimal Gin engine for testing.
func testImageRouter(svc imageServicer) *gin.Engine {
	r := gin.New()
	h := NewImageHandler(svc, testImageLogger)
	r.GET("/api/images", h.ListImages)
	r.GET("/api/images/:imageName/tags", h.ListImageTags)
	return r
}

// ── ListImages tests ──────────────────────────────────────────────────────────

func TestListImages_Success_Returns200WithData(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)
	svc := &mockImageService{
		getImagesFn: func() ([]dto.ImageListItem, error) {
			return []dto.ImageListItem{
				{Name: "myapp", Tags: []string{"latest", "v1.0"}, Size: 10240, PushedAt: now, Author: "CI", IsDangling: false},
			}, nil
		},
	}

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/images", nil)
	testImageRouter(svc).ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d (body: %s)", w.Code, w.Body.String())
	}

	var body []dto.ImageListItem
	if err := json.NewDecoder(w.Body).Decode(&body); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if len(body) != 1 {
		t.Fatalf("expected 1 image, got %d", len(body))
	}
	if body[0].Name != "myapp" {
		t.Errorf("expected name 'myapp', got %q", body[0].Name)
	}
	if body[0].Size != 10240 {
		t.Errorf("expected size 10240, got %d", body[0].Size)
	}
}

func TestListImages_EmptyRegistry_Returns200WithEmptyArray(t *testing.T) {
	svc := &mockImageService{
		getImagesFn: func() ([]dto.ImageListItem, error) {
			return []dto.ImageListItem{}, nil
		},
	}

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/images", nil)
	testImageRouter(svc).ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", w.Code)
	}

	// Body must be "[]" not "null"
	body := w.Body.String()
	// json.Decoder on "[]" produces an empty slice, not null
	var result []dto.ImageListItem
	if err := json.Unmarshal([]byte(body), &result); err != nil {
		t.Fatalf("failed to unmarshal body: %v", err)
	}
	if result == nil {
		t.Error("expected empty array [] in response body, got null")
	}
}

func TestListImages_NilSliceFromService_Returns200WithEmptyArray(t *testing.T) {
	// Verify the handler normalises nil slice → [] to avoid JSON null
	svc := &mockImageService{
		getImagesFn: func() ([]dto.ImageListItem, error) {
			return nil, nil
		},
	}

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/images", nil)
	testImageRouter(svc).ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", w.Code)
	}
	var result []dto.ImageListItem
	if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
		t.Fatalf("failed to unmarshal: %v", err)
	}
	if result == nil {
		t.Error("expected [] not null")
	}
}

func TestListImages_RegistryUnavailable_Returns503(t *testing.T) {
	svc := &mockImageService{
		getImagesFn: func() ([]dto.ImageListItem, error) {
			return nil, fmt.Errorf("%w: connection refused", services.ErrRegistryUnavailable)
		},
	}

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/images", nil)
	testImageRouter(svc).ServeHTTP(w, req)

	if w.Code != http.StatusServiceUnavailable {
		t.Errorf("expected 503, got %d (body: %s)", w.Code, w.Body.String())
	}
	assertJSONError(t, w, "registry unavailable")
}

func TestListImages_ImageNotFound_Returns404(t *testing.T) {
	svc := &mockImageService{
		getImagesFn: func() ([]dto.ImageListItem, error) {
			return nil, fmt.Errorf("%w: /v2/gone/tags/list", services.ErrImageNotFound)
		},
	}

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/images", nil)
	testImageRouter(svc).ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("expected 404, got %d (body: %s)", w.Code, w.Body.String())
	}
	assertJSONError(t, w, "image not found")
}

func TestListImages_InternalError_Returns500(t *testing.T) {
	svc := &mockImageService{
		getImagesFn: func() ([]dto.ImageListItem, error) {
			return nil, errors.New("unexpected JSON decode error")
		},
	}

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/images", nil)
	testImageRouter(svc).ServeHTTP(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Errorf("expected 500, got %d", w.Code)
	}
	assertJSONError(t, w, "internal server error")
}

// ── ListImageTags tests ───────────────────────────────────────────────────────

func TestListImageTags_Success_Returns200WithData(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)
	svc := &mockImageService{
		getImageTagsFn: func(imageName string) ([]dto.ImageTag, error) {
			if imageName != "myapp" {
				return nil, errors.New("unexpected imageName")
			}
			return []dto.ImageTag{
				{Tag: "latest", Digest: "sha256:abc", Size: 5120, PushedAt: now},
				{Tag: "v1.0", Digest: "sha256:def", Size: 4096, PushedAt: now.Add(-time.Hour)},
			}, nil
		},
	}

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/images/myapp/tags", nil)
	testImageRouter(svc).ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d (body: %s)", w.Code, w.Body.String())
	}

	var body []dto.ImageTag
	if err := json.NewDecoder(w.Body).Decode(&body); err != nil {
		t.Fatalf("failed to decode: %v", err)
	}
	if len(body) != 2 {
		t.Fatalf("expected 2 tags, got %d", len(body))
	}
	if body[0].Tag != "latest" {
		t.Errorf("expected first tag 'latest', got %q", body[0].Tag)
	}
}

func TestListImageTags_EmptyTags_Returns200WithEmptyArray(t *testing.T) {
	svc := &mockImageService{
		getImageTagsFn: func(_ string) ([]dto.ImageTag, error) {
			return []dto.ImageTag{}, nil
		},
	}

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/images/myapp/tags", nil)
	testImageRouter(svc).ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", w.Code)
	}
	var result []dto.ImageTag
	if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
		t.Fatalf("failed to unmarshal: %v", err)
	}
	if result == nil {
		t.Error("expected [] not null")
	}
}

func TestListImageTags_ImageNotFound_Returns404(t *testing.T) {
	svc := &mockImageService{
		getImageTagsFn: func(_ string) ([]dto.ImageTag, error) {
			return nil, fmt.Errorf("%w: /v2/nonexistent/tags/list", services.ErrImageNotFound)
		},
	}

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/images/nonexistent/tags", nil)
	testImageRouter(svc).ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("expected 404, got %d (body: %s)", w.Code, w.Body.String())
	}
	assertJSONError(t, w, "image not found")
}

func TestListImageTags_RegistryUnavailable_Returns503(t *testing.T) {
	svc := &mockImageService{
		getImageTagsFn: func(_ string) ([]dto.ImageTag, error) {
			return nil, fmt.Errorf("%w: timeout", services.ErrRegistryUnavailable)
		},
	}

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/images/myapp/tags", nil)
	testImageRouter(svc).ServeHTTP(w, req)

	if w.Code != http.StatusServiceUnavailable {
		t.Errorf("expected 503, got %d", w.Code)
	}
	assertJSONError(t, w, "registry unavailable")
}

func TestListImageTags_InternalError_Returns500(t *testing.T) {
	svc := &mockImageService{
		getImageTagsFn: func(_ string) ([]dto.ImageTag, error) {
			return nil, errors.New("unexpected error")
		},
	}

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/images/myapp/tags", nil)
	testImageRouter(svc).ServeHTTP(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Errorf("expected 500, got %d", w.Code)
	}
	assertJSONError(t, w, "internal server error")
}

// ── Interface satisfaction ────────────────────────────────────────────────────

// TestImageService_SatisfiesImageServicer verifies at compile time that
// *services.ImageService implements the imageServicer interface.
func TestImageService_SatisfiesImageServicer(t *testing.T) {
	var _ imageServicer = (*services.ImageService)(nil)
}
