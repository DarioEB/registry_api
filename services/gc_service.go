package services

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"sync"
	"time"

	"github.com/DarioEB/logdeb"
	"github.com/robfig/cron/v3"
	"gorm.io/gorm"

	"registry_dashboard_api/models"
	"registry_dashboard_api/models/dto"
)

// Sentinel errors for GC operations.
var ErrGCAlreadyRunning = errors.New("gc already running")
var ErrInvalidSchedule = errors.New("invalid cron schedule")

// GCService manages garbage collection execution, state, and cron scheduling.
// GC state lives in memory — a server restart resets status to idle with no lastRun.
type GCService struct {
	db           *gorm.DB
	imageService *ImageService
	logger       *logdeb.Logdeb
	registryURL  string
	registryUser string
	registryPass string
	httpClient   *http.Client

	mu        sync.Mutex
	status    string
	startedAt time.Time
	lastRun   *dto.GCLastRun

	scheduler   *cron.Cron
	cronEntryID cron.EntryID
}

// NewGCService creates a GCService, loads config from DB, and starts the cron
// scheduler if enabled.
func NewGCService(db *gorm.DB, imageService *ImageService, logger *logdeb.Logdeb) *GCService {
	s := &GCService{
		db:           db,
		imageService: imageService,
		logger:       logger,
		registryURL:  imageService.registryURL,
		registryUser: imageService.registryAdminUser,
		registryPass: imageService.registryAdminPass,
		httpClient:   imageService.httpClient,
		status:       "idle",
		scheduler:    cron.New(),
	}

	// Always start the scheduler so reconfigureCron can add entries later
	s.scheduler.Start()

	// Load config and schedule initial cron if enabled
	var cfg models.GCConfig
	if err := db.First(&cfg).Error; err != nil {
		logger.Error("failed to load gc config", "error", err)
	} else if cfg.Enabled {
		entryID, err := s.scheduler.AddFunc(cfg.Schedule, func() {
			if _, err := s.RunGC(); err != nil {
				s.logger.Error("scheduled GC failed to start", "error", err)
			}
		})
		if err != nil {
			logger.Error("failed to schedule GC cron", "error", err, "schedule", cfg.Schedule)
		} else {
			s.cronEntryID = entryID
			logger.Info("GC cron scheduled", "schedule", cfg.Schedule)
		}
	}

	return s
}

// RunGC starts an asynchronous garbage collection. Returns immediately with an
// error if a GC is already running.
func (s *GCService) RunGC() (time.Time, error) {
	s.mu.Lock()
	if s.status == "running" {
		s.mu.Unlock()
		return time.Time{}, ErrGCAlreadyRunning
	}
	s.status = "running"
	s.startedAt = time.Now().UTC()
	startedAt := s.startedAt
	s.mu.Unlock()

	s.logger.Info("GC started")

	go s.executeGC()

	return startedAt, nil
}

// GetStatus returns the current GC state and last run results.
func (s *GCService) GetStatus() dto.GCStatusResponse {
	s.mu.Lock()
	defer s.mu.Unlock()

	resp := dto.GCStatusResponse{
		Status:  s.status,
		LastRun: s.lastRun,
	}
	if s.status == "running" {
		startedAt := s.startedAt
		resp.StartedAt = &startedAt
	}
	return resp
}

// GetConfig reads the GC configuration from PostgreSQL.
func (s *GCService) GetConfig() (dto.GCConfigResponse, error) {
	var cfg models.GCConfig
	if err := s.db.First(&cfg).Error; err != nil {
		return dto.GCConfigResponse{}, fmt.Errorf("read gc config: %w", err)
	}
	return dto.GCConfigResponse{
		Schedule: cfg.Schedule,
		Enabled:  cfg.Enabled,
	}, nil
}

// UpdateConfig validates and persists new GC configuration, then reconfigures
// the cron scheduler in-place without restarting the server.
func (s *GCService) UpdateConfig(req dto.UpdateGCConfigRequest) (dto.GCConfigResponse, error) {
	var cfg models.GCConfig
	if err := s.db.First(&cfg).Error; err != nil {
		return dto.GCConfigResponse{}, fmt.Errorf("read gc config: %w", err)
	}

	if req.Schedule != nil {
		if _, err := cron.ParseStandard(*req.Schedule); err != nil {
			return dto.GCConfigResponse{}, fmt.Errorf("%w: %v", ErrInvalidSchedule, err)
		}
		cfg.Schedule = *req.Schedule
	}
	if req.Enabled != nil {
		cfg.Enabled = *req.Enabled
	}

	if err := s.db.Save(&cfg).Error; err != nil {
		return dto.GCConfigResponse{}, fmt.Errorf("save gc config: %w", err)
	}

	// Reconfigure cron scheduler
	s.reconfigureCron(cfg.Schedule, cfg.Enabled)

	s.logger.Info("GC config updated", "schedule", cfg.Schedule, "enabled", cfg.Enabled)

	return dto.GCConfigResponse{
		Schedule: cfg.Schedule,
		Enabled:  cfg.Enabled,
	}, nil
}

// ── Private helpers ─────────────────────────────────────────────────────────

// executeGC runs the actual garbage collection in a goroutine.
func (s *GCService) executeGC() {
	ctx := context.Background()
	var deletedCount int
	var freedBytes int64
	var gcErr error

	defer func() {
		s.mu.Lock()
		now := time.Now().UTC()
		s.lastRun = &dto.GCLastRun{
			DeletedCount: deletedCount,
			FreedBytes:   freedBytes,
			CompletedAt:  now,
		}
		if gcErr != nil {
			s.status = "failed"
			s.lastRun.Error = gcErr.Error()
			s.logger.Error("GC failed", "error", gcErr, "deletedCount", deletedCount)
		} else {
			s.status = "completed"
			s.logger.Info("GC completed", "deletedCount", deletedCount, "freedBytes", freedBytes)
		}
		s.mu.Unlock()
	}()

	// 1. Get all images from registry
	images, err := s.imageService.GetImages()
	if err != nil {
		gcErr = fmt.Errorf("fetch images: %w", err)
		return
	}

	// 2. Filter dangling candidates
	var dangling []dto.ImageListItem
	for _, img := range images {
		if img.IsDangling {
			dangling = append(dangling, img)
		}
	}

	if len(dangling) == 0 {
		s.logger.Info("GC found no dangling images")
		return
	}

	s.logger.Info("GC found dangling candidates", "count", len(dangling))

	// 3. Re-verify each candidate is still dangling (safety check)
	for _, img := range dangling {
		var tagsResp struct {
			Tags []string `json:"tags"`
		}
		path := fmt.Sprintf("/v2/%s/tags/list", img.Name)
		if err := s.registryJSON(ctx, http.MethodGet, path, &tagsResp); err != nil {
			gcErr = fmt.Errorf("re-verify %s: %w", img.Name, err)
			return
		}
		if len(tagsResp.Tags) > 0 {
			gcErr = fmt.Errorf("safety abort: image %s now has tags %v — refusing to delete", img.Name, tagsResp.Tags)
			return
		}
	}

	// 4. Delete each dangling image's manifests
	for _, img := range dangling {
		size, err := s.deleteDanglingImage(ctx, img.Name)
		if err != nil {
			gcErr = fmt.Errorf("delete %s: %w", img.Name, err)
			return
		}
		deletedCount++
		freedBytes += size
	}
}

// deleteDanglingImage attempts to delete an untagged image's manifest via the
// registry API. For truly tagless images the registry may return 404 on manifest
// lookup — this is expected and counted as "already cleaned" (returns 0 freed bytes).
// Actual blob cleanup requires running `registry garbage-collect` on the host.
func (s *GCService) deleteDanglingImage(ctx context.Context, name string) (int64, error) {
	// Try fetching manifest — may 404 for truly tagless repos
	path := fmt.Sprintf("/v2/%s/manifests/latest", name)
	headers := map[string]string{
		"Accept": "application/vnd.docker.distribution.manifest.v2+json",
	}

	resp, err := s.registryRequest(ctx, http.MethodGet, path, headers)
	if err != nil {
		if errors.Is(err, ErrImageNotFound) {
			s.logger.Info("Dangling image has no fetchable manifest (already unreferenced)", "image", name)
			return 0, nil
		}
		return 0, err
	}
	defer resp.Body.Close()

	digest := resp.Header.Get("Docker-Content-Digest")
	if digest == "" {
		return 0, nil
	}

	var manifest manifestV2
	if err := json.NewDecoder(resp.Body).Decode(&manifest); err != nil {
		return 0, fmt.Errorf("decode manifest for %s: %w", name, err)
	}
	size := layersTotalSize(manifest.Layers)

	// DELETE the manifest by digest
	deletePath := fmt.Sprintf("/v2/%s/manifests/%s", name, digest)
	deleteResp, err := s.registryRequest(ctx, http.MethodDelete, deletePath, nil)
	if err != nil {
		if errors.Is(err, ErrImageNotFound) {
			return 0, nil
		}
		return 0, err
	}
	deleteResp.Body.Close()

	s.logger.Info("Deleted dangling manifest", "image", name, "digest", digest, "size", size)
	return size, nil
}

// registryRequest performs an authenticated HTTP request against the registry.
func (s *GCService) registryRequest(ctx context.Context, method, path string, extraHeaders map[string]string) (*http.Response, error) {
	url := s.registryURL + path
	req, err := http.NewRequestWithContext(ctx, method, url, nil)
	if err != nil {
		return nil, fmt.Errorf("build request: %w", err)
	}
	req.SetBasicAuth(s.registryUser, s.registryPass)
	for k, v := range extraHeaders {
		req.Header.Set(k, v)
	}

	resp, err := s.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrRegistryUnavailable, err)
	}
	if resp.StatusCode >= 500 {
		resp.Body.Close()
		return nil, fmt.Errorf("%w: registry returned %d for %s", ErrRegistryUnavailable, resp.StatusCode, path)
	}
	if resp.StatusCode == http.StatusNotFound {
		resp.Body.Close()
		return nil, fmt.Errorf("%w: %s", ErrImageNotFound, path)
	}
	return resp, nil
}

// registryJSON performs an authenticated request and decodes the JSON body.
func (s *GCService) registryJSON(ctx context.Context, method, path string, dst any) error {
	resp, err := s.registryRequest(ctx, method, path, nil)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, maxErrorBodySize))
		return fmt.Errorf("registry error %d for %s: %s", resp.StatusCode, path, string(body))
	}

	return json.NewDecoder(resp.Body).Decode(dst)
}

// reconfigureCron stops the old cron entry and starts a new one if enabled.
func (s *GCService) reconfigureCron(schedule string, enabled bool) {
	if s.cronEntryID != 0 {
		s.scheduler.Remove(s.cronEntryID)
		s.cronEntryID = 0
	}

	if !enabled {
		return
	}

	entryID, err := s.scheduler.AddFunc(schedule, func() {
		if _, err := s.RunGC(); err != nil {
			s.logger.Error("scheduled GC failed to start", "error", err)
		}
	})
	if err != nil {
		s.logger.Error("failed to reconfigure GC cron", "error", err, "schedule", schedule)
		return
	}
	s.cronEntryID = entryID
	// scheduler.Start() already called in constructor — no need to call again
}
