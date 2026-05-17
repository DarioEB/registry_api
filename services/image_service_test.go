package services

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// ── Pure function tests ──────────────────────────────────────────────────────

func TestParseCreated_RFC3339Nano(t *testing.T) {
	got := parseCreated("2024-06-15T10:30:00.123456789Z")
	if got.IsZero() {
		t.Error("expected non-zero time for valid RFC3339Nano input")
	}
	if got.Year() != 2024 || got.Month() != 6 || got.Day() != 15 {
		t.Errorf("unexpected date: %v", got)
	}
}

func TestParseCreated_RFC3339(t *testing.T) {
	got := parseCreated("2024-06-15T10:30:00Z")
	if got.IsZero() {
		t.Error("expected non-zero time for valid RFC3339 input")
	}
}

func TestParseCreated_Empty(t *testing.T) {
	got := parseCreated("")
	if !got.IsZero() {
		t.Errorf("expected zero time for empty input, got %v", got)
	}
}

func TestParseCreated_Invalid(t *testing.T) {
	got := parseCreated("not-a-date")
	if !got.IsZero() {
		t.Errorf("expected zero time for invalid input, got %v", got)
	}
}

func TestLayersTotalSize(t *testing.T) {
	layers := []manifestLayer{{Size: 100}, {Size: 200}, {Size: 300}}
	got := layersTotalSize(layers)
	if got != 600 {
		t.Errorf("expected 600, got %d", got)
	}
}

func TestLayersTotalSize_Empty(t *testing.T) {
	got := layersTotalSize(nil)
	if got != 0 {
		t.Errorf("expected 0 for nil layers, got %d", got)
	}
}

func TestLayersTotalSize_Single(t *testing.T) {
	got := layersTotalSize([]manifestLayer{{Size: 42}})
	if got != 42 {
		t.Errorf("expected 42, got %d", got)
	}
}

// ── Registry HTTP tests (httptest server) ────────────────────────────────────

// registryMux builds a mock Docker registry HTTP API v2 with a single image
// "myapp" having tag "latest" with two layers (1000 + 2000 bytes).
func registryMux() *http.ServeMux {
	mux := http.NewServeMux()

	mux.HandleFunc("GET /v2/_catalog", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(catalogResponse{Repositories: []string{"myapp"}})
	})

	mux.HandleFunc("GET /v2/myapp/tags/list", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(tagsResponse{Name: "myapp", Tags: []string{"latest"}})
	})

	mux.HandleFunc("GET /v2/myapp/manifests/latest", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Docker-Content-Digest", "sha256:manifestdigest")
		json.NewEncoder(w).Encode(manifestV2{
			SchemaVersion: 2,
			Config:        manifestConfig{Digest: "sha256:cfgdigest", Size: 100},
			Layers:        []manifestLayer{{Size: 1000}, {Size: 2000}},
		})
	})

	mux.HandleFunc("GET /v2/myapp/blobs/sha256:cfgdigest", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(imageConfig{Created: "2024-06-15T10:00:00Z", Author: "CI"})
	})

	return mux
}

func TestGetImages_Success(t *testing.T) {
	server := httptest.NewServer(registryMux())
	defer server.Close()

	svc := NewImageService(server.URL, "admin", "pass")
	images, err := svc.GetImages()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(images) != 1 {
		t.Fatalf("expected 1 image, got %d", len(images))
	}

	img := images[0]
	if img.Name != "myapp" {
		t.Errorf("expected name 'myapp', got %q", img.Name)
	}
	if len(img.Tags) != 1 || img.Tags[0] != "latest" {
		t.Errorf("expected tags [latest], got %v", img.Tags)
	}
	if img.Size != 3000 {
		t.Errorf("expected size 3000 (1000+2000), got %d", img.Size)
	}
	if img.Author != "CI" {
		t.Errorf("expected author 'CI', got %q", img.Author)
	}
	if img.IsDangling {
		t.Error("expected isDangling=false for image with tags")
	}
	if img.PushedAt.IsZero() {
		t.Error("expected non-zero pushedAt")
	}
}

func TestGetImages_DanglingImage(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /v2/_catalog", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(catalogResponse{Repositories: []string{"orphan"}})
	})
	mux.HandleFunc("GET /v2/orphan/tags/list", func(w http.ResponseWriter, r *http.Request) {
		// Registry returns null tags for images with no tags
		json.NewEncoder(w).Encode(tagsResponse{Name: "orphan", Tags: nil})
	})
	server := httptest.NewServer(mux)
	defer server.Close()

	svc := NewImageService(server.URL, "admin", "pass")
	images, err := svc.GetImages()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(images) != 1 {
		t.Fatalf("expected 1 image, got %d", len(images))
	}
	if !images[0].IsDangling {
		t.Error("expected isDangling=true for image with null tags")
	}
	if images[0].Size != 0 {
		t.Errorf("expected size=0 for dangling image, got %d", images[0].Size)
	}
	if len(images[0].Tags) != 0 {
		t.Errorf("expected empty tags for dangling, got %v", images[0].Tags)
	}
}

func TestGetImages_EmptyCatalog(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /v2/_catalog", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(catalogResponse{Repositories: []string{}})
	})
	server := httptest.NewServer(mux)
	defer server.Close()

	svc := NewImageService(server.URL, "admin", "pass")
	images, err := svc.GetImages()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(images) != 0 {
		t.Errorf("expected 0 images, got %d", len(images))
	}
}

func TestGetImages_SortedByPushedAtDesc(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /v2/_catalog", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(catalogResponse{Repositories: []string{"old", "new"}})
	})
	mux.HandleFunc("GET /v2/old/tags/list", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(tagsResponse{Name: "old", Tags: []string{"v1"}})
	})
	mux.HandleFunc("GET /v2/new/tags/list", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(tagsResponse{Name: "new", Tags: []string{"v1"}})
	})
	mux.HandleFunc("GET /v2/old/manifests/v1", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Docker-Content-Digest", "sha256:old")
		json.NewEncoder(w).Encode(manifestV2{
			SchemaVersion: 2,
			Config:        manifestConfig{Digest: "sha256:oldcfg"},
			Layers:        []manifestLayer{{Size: 100}},
		})
	})
	mux.HandleFunc("GET /v2/new/manifests/v1", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Docker-Content-Digest", "sha256:new")
		json.NewEncoder(w).Encode(manifestV2{
			SchemaVersion: 2,
			Config:        manifestConfig{Digest: "sha256:newcfg"},
			Layers:        []manifestLayer{{Size: 200}},
		})
	})
	mux.HandleFunc("GET /v2/old/blobs/sha256:oldcfg", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(imageConfig{Created: "2020-01-01T00:00:00Z"})
	})
	mux.HandleFunc("GET /v2/new/blobs/sha256:newcfg", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(imageConfig{Created: "2025-06-01T00:00:00Z"})
	})
	server := httptest.NewServer(mux)
	defer server.Close()

	svc := NewImageService(server.URL, "admin", "pass")
	images, err := svc.GetImages()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(images) != 2 {
		t.Fatalf("expected 2 images, got %d", len(images))
	}
	if images[0].Name != "new" {
		t.Errorf("expected 'new' first (most recent), got %q", images[0].Name)
	}
	if images[1].Name != "old" {
		t.Errorf("expected 'old' second, got %q", images[1].Name)
	}
}

func TestGetImages_RegistryUnavailable(t *testing.T) {
	// Use a URL that will refuse connection
	svc := NewImageService("http://127.0.0.1:1", "admin", "pass")
	_, err := svc.GetImages()
	if err == nil {
		t.Fatal("expected error for unreachable registry")
	}
	if !errors.Is(err, ErrRegistryUnavailable) {
		t.Errorf("expected ErrRegistryUnavailable, got: %v", err)
	}
}

func TestGetImages_Registry5xx(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /v2/_catalog", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	})
	server := httptest.NewServer(mux)
	defer server.Close()

	svc := NewImageService(server.URL, "admin", "pass")
	_, err := svc.GetImages()
	if err == nil {
		t.Fatal("expected error for 500 registry")
	}
	if !errors.Is(err, ErrRegistryUnavailable) {
		t.Errorf("expected ErrRegistryUnavailable, got: %v", err)
	}
}

func TestGetImageTags_Success(t *testing.T) {
	server := httptest.NewServer(registryMux())
	defer server.Close()

	svc := NewImageService(server.URL, "admin", "pass")
	tags, err := svc.GetImageTags("myapp")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(tags) != 1 {
		t.Fatalf("expected 1 tag, got %d", len(tags))
	}
	tag := tags[0]
	if tag.Tag != "latest" {
		t.Errorf("expected tag 'latest', got %q", tag.Tag)
	}
	if tag.Digest != "sha256:manifestdigest" {
		t.Errorf("expected digest 'sha256:manifestdigest', got %q", tag.Digest)
	}
	if tag.Size != 3000 {
		t.Errorf("expected size 3000, got %d", tag.Size)
	}
	if tag.PushedAt.IsZero() {
		t.Error("expected non-zero pushedAt")
	}
}

func TestGetImageTags_ImageNotFound(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /v2/gone/tags/list", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	})
	server := httptest.NewServer(mux)
	defer server.Close()

	svc := NewImageService(server.URL, "admin", "pass")
	_, err := svc.GetImageTags("gone")
	if err == nil {
		t.Fatal("expected error for 404")
	}
	if !errors.Is(err, ErrImageNotFound) {
		t.Errorf("expected ErrImageNotFound, got: %v", err)
	}
}

func TestGetImageTags_RegistryUnavailable(t *testing.T) {
	svc := NewImageService("http://127.0.0.1:1", "admin", "pass")
	_, err := svc.GetImageTags("myapp")
	if err == nil {
		t.Fatal("expected error for unreachable registry")
	}
	if !errors.Is(err, ErrRegistryUnavailable) {
		t.Errorf("expected ErrRegistryUnavailable, got: %v", err)
	}
}

func TestGetImages_BasicAuthSent(t *testing.T) {
	var capturedUser, capturedPass string
	mux := http.NewServeMux()
	mux.HandleFunc("GET /v2/_catalog", func(w http.ResponseWriter, r *http.Request) {
		capturedUser, capturedPass, _ = r.BasicAuth()
		json.NewEncoder(w).Encode(catalogResponse{Repositories: []string{}})
	})
	server := httptest.NewServer(mux)
	defer server.Close()

	svc := NewImageService(server.URL, "myuser", "mypass")
	_, err := svc.GetImages()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if capturedUser != "myuser" {
		t.Errorf("expected basic auth user 'myuser', got %q", capturedUser)
	}
	if capturedPass != "mypass" {
		t.Errorf("expected basic auth pass 'mypass', got %q", capturedPass)
	}
}

func TestGetImages_ManifestAcceptHeader(t *testing.T) {
	var capturedAccept string
	mux := http.NewServeMux()
	mux.HandleFunc("GET /v2/_catalog", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(catalogResponse{Repositories: []string{"app"}})
	})
	mux.HandleFunc("GET /v2/app/tags/list", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(tagsResponse{Name: "app", Tags: []string{"v1"}})
	})
	mux.HandleFunc("GET /v2/app/manifests/v1", func(w http.ResponseWriter, r *http.Request) {
		capturedAccept = r.Header.Get("Accept")
		w.Header().Set("Docker-Content-Digest", "sha256:test")
		json.NewEncoder(w).Encode(manifestV2{
			SchemaVersion: 2,
			Config:        manifestConfig{Digest: "sha256:cfg"},
			Layers:        []manifestLayer{{Size: 100}},
		})
	})
	mux.HandleFunc("GET /v2/app/blobs/sha256:cfg", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(imageConfig{Created: "2024-01-01T00:00:00Z"})
	})
	server := httptest.NewServer(mux)
	defer server.Close()

	svc := NewImageService(server.URL, "admin", "pass")
	_, _ = svc.GetImages()

	expected := "application/vnd.docker.distribution.manifest.v2+json"
	if capturedAccept != expected {
		t.Errorf("expected Accept header %q, got %q", expected, capturedAccept)
	}
}

// ── Timeout test ─────────────────────────────────────────────────────────────

func TestNewImageService_HasTimeout(t *testing.T) {
	svc := NewImageService("http://localhost", "u", "p")
	if svc.httpClient.Timeout != 10*time.Second {
		t.Errorf("expected 10s timeout, got %v", svc.httpClient.Timeout)
	}
}
