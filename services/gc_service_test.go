package services

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/DarioEB/logdeb"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"

	"registry_dashboard_api/models"
	"registry_dashboard_api/models/dto"
)

// testGCLogger creates a logger for GC tests.
func testGCLogger() *logdeb.Logdeb {
	l, _ := logdeb.New(logdeb.DefaultConfig())
	return l
}

// testDB tries to connect to a test database. Returns nil if unavailable.
func testDB() *gorm.DB {
	dsn := "host=localhost user=dashboard_user password=dashboard_pass dbname=dashboard port=5432 sslmode=disable"
	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{})
	if err != nil {
		return nil
	}
	return db
}

// ── GC State Machine Tests ──────────────────────────────────────────────────

func TestGCService_InitialStatusIdle(t *testing.T) {
	// Create a GCService without DB (will log error but not crash)
	db := testDB()
	if db == nil {
		t.Skip("database not available")
	}

	logger := testGCLogger()
	imgService := NewImageService("http://localhost:5000", "admin", "pass")
	svc := NewGCService(db, imgService, logger)

	status := svc.GetStatus()
	if status.Status != "idle" {
		t.Errorf("expected idle, got %s", status.Status)
	}
	if status.LastRun != nil {
		t.Error("expected nil lastRun on fresh service")
	}
	if status.StartedAt != nil {
		t.Error("expected nil startedAt when idle")
	}
}

func TestGCService_RunGC_TransitionsToRunning(t *testing.T) {
	// Mock registry that returns empty catalog (no images to delete)
	mux := http.NewServeMux()
	mux.HandleFunc("GET /v2/_catalog", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(catalogResponse{Repositories: []string{}})
	})
	ts := httptest.NewServer(mux)
	defer ts.Close()

	db := testDB()
	if db == nil {
		t.Skip("database not available")
	}

	logger := testGCLogger()
	imgService := NewImageService(ts.URL, "admin", "pass")
	svc := NewGCService(db, imgService, logger)

	_, err := svc.RunGC()
	if err != nil {
		t.Fatalf("RunGC failed: %v", err)
	}

	// Status should be running immediately after RunGC returns
	status := svc.GetStatus()
	if status.Status != "running" && status.Status != "completed" {
		t.Errorf("expected running or completed, got %s", status.Status)
	}

	// Wait for goroutine to complete
	time.Sleep(200 * time.Millisecond)

	status = svc.GetStatus()
	if status.Status != "completed" {
		t.Errorf("expected completed after empty catalog GC, got %s", status.Status)
	}
	if status.LastRun == nil {
		t.Fatal("expected lastRun to be set")
	}
	if status.LastRun.DeletedCount != 0 {
		t.Errorf("expected 0 deleted, got %d", status.LastRun.DeletedCount)
	}
}

func TestGCService_RunGC_AlreadyRunning(t *testing.T) {
	// Slow registry to keep GC in running state
	mux := http.NewServeMux()
	mux.HandleFunc("GET /v2/_catalog", func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(500 * time.Millisecond)
		json.NewEncoder(w).Encode(catalogResponse{Repositories: []string{}})
	})
	ts := httptest.NewServer(mux)
	defer ts.Close()

	db := testDB()
	if db == nil {
		t.Skip("database not available")
	}

	logger := testGCLogger()
	imgService := NewImageService(ts.URL, "admin", "pass")
	svc := NewGCService(db, imgService, logger)

	// Start first GC
	if _, err := svc.RunGC(); err != nil {
		t.Fatalf("first RunGC failed: %v", err)
	}

	// Second GC should fail
	_, err := svc.RunGC()
	if err == nil {
		t.Fatal("expected error for concurrent GC")
	}
	if err != ErrGCAlreadyRunning {
		t.Errorf("expected ErrGCAlreadyRunning, got %v", err)
	}
}

func TestGCService_RunGC_RegistryUnavailable(t *testing.T) {
	// Registry that returns 500
	mux := http.NewServeMux()
	mux.HandleFunc("GET /v2/_catalog", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	})
	ts := httptest.NewServer(mux)
	defer ts.Close()

	db := testDB()
	if db == nil {
		t.Skip("database not available")
	}

	logger := testGCLogger()
	imgService := NewImageService(ts.URL, "admin", "pass")
	svc := NewGCService(db, imgService, logger)

	_, _ = svc.RunGC()
	time.Sleep(200 * time.Millisecond)

	status := svc.GetStatus()
	if status.Status != "failed" {
		t.Errorf("expected failed, got %s", status.Status)
	}
	if status.LastRun == nil || status.LastRun.Error == "" {
		t.Error("expected error in lastRun")
	}
}

func TestGCService_RunGC_WithDanglingImages(t *testing.T) {
	mux := http.NewServeMux()

	// Catalog with one dangling image
	mux.HandleFunc("GET /v2/_catalog", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(catalogResponse{Repositories: []string{"stale-app"}})
	})

	// Tags: empty (dangling)
	mux.HandleFunc("GET /v2/stale-app/tags/list", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(tagsResponse{Name: "stale-app", Tags: nil})
	})

	// Manifest for deletion
	mux.HandleFunc("GET /v2/stale-app/manifests/latest", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Docker-Content-Digest", "sha256:abc123")
		json.NewEncoder(w).Encode(manifestV2{
			SchemaVersion: 2,
			Layers:        []manifestLayer{{Size: 1000}, {Size: 2000}},
		})
	})

	// DELETE manifest
	mux.HandleFunc("DELETE /v2/stale-app/manifests/sha256:abc123", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusAccepted)
	})

	ts := httptest.NewServer(mux)
	defer ts.Close()

	db := testDB()
	if db == nil {
		t.Skip("database not available")
	}

	logger := testGCLogger()
	imgService := NewImageService(ts.URL, "admin", "pass")
	svc := NewGCService(db, imgService, logger)

	_, _ = svc.RunGC()
	time.Sleep(300 * time.Millisecond)

	status := svc.GetStatus()
	if status.Status != "completed" {
		t.Errorf("expected completed, got %s", status.Status)
	}
	if status.LastRun == nil {
		t.Fatal("expected lastRun")
	}
	if status.LastRun.DeletedCount != 1 {
		t.Errorf("expected 1 deleted, got %d", status.LastRun.DeletedCount)
	}
	if status.LastRun.FreedBytes != 3000 {
		t.Errorf("expected 3000 freed bytes, got %d", status.LastRun.FreedBytes)
	}
}

// ── Config Tests ────────────────────────────────────────────────────────────

func TestGCService_GetConfig(t *testing.T) {
	db := testDB()
	if db == nil {
		t.Skip("database not available")
	}

	// Ensure default row exists
	db.AutoMigrate(&models.GCConfig{})
	var count int64
	db.Model(&models.GCConfig{}).Count(&count)
	if count == 0 {
		db.Create(&models.GCConfig{Schedule: "0 3 * * *", Enabled: true})
	}

	logger := testGCLogger()
	imgService := NewImageService("http://localhost:5000", "admin", "pass")
	svc := NewGCService(db, imgService, logger)

	cfg, err := svc.GetConfig()
	if err != nil {
		t.Fatalf("GetConfig failed: %v", err)
	}
	if cfg.Schedule == "" {
		t.Error("expected non-empty schedule")
	}
}

func TestGCService_UpdateConfig_ValidSchedule(t *testing.T) {
	db := testDB()
	if db == nil {
		t.Skip("database not available")
	}

	logger := testGCLogger()
	imgService := NewImageService("http://localhost:5000", "admin", "pass")
	svc := NewGCService(db, imgService, logger)

	newSchedule := "0 0 * * 0"
	cfg, err := svc.UpdateConfig(dto.UpdateGCConfigRequest{Schedule: &newSchedule})
	if err != nil {
		t.Fatalf("UpdateConfig failed: %v", err)
	}
	if cfg.Schedule != "0 0 * * 0" {
		t.Errorf("expected '0 0 * * 0', got '%s'", cfg.Schedule)
	}

	// Restore default
	defaultSchedule := "0 3 * * *"
	svc.UpdateConfig(dto.UpdateGCConfigRequest{Schedule: &defaultSchedule})
}

func TestGCService_UpdateConfig_InvalidSchedule(t *testing.T) {
	db := testDB()
	if db == nil {
		t.Skip("database not available")
	}

	logger := testGCLogger()
	imgService := NewImageService("http://localhost:5000", "admin", "pass")
	svc := NewGCService(db, imgService, logger)

	badSchedule := "not a cron"
	_, err := svc.UpdateConfig(dto.UpdateGCConfigRequest{Schedule: &badSchedule})
	if err == nil {
		t.Fatal("expected error for invalid schedule")
	}
}
