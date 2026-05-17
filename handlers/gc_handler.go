package handlers

import (
	"errors"
	"net/http"
	"time"

	"github.com/DarioEB/logdeb"
	"github.com/gin-gonic/gin"

	"registry_dashboard_api/models/dto"
	"registry_dashboard_api/services"
)

// gcServicer is the subset of *services.GCService used by GCHandler.
type gcServicer interface {
	RunGC() (time.Time, error)
	GetStatus() dto.GCStatusResponse
	GetConfig() (dto.GCConfigResponse, error)
	UpdateConfig(req dto.UpdateGCConfigRequest) (dto.GCConfigResponse, error)
}

// GCHandler handles HTTP requests for garbage collection endpoints.
type GCHandler struct {
	gcService gcServicer
	logger    *logdeb.Logdeb
}

// NewGCHandler creates a new GCHandler.
func NewGCHandler(gcService gcServicer, logger *logdeb.Logdeb) *GCHandler {
	return &GCHandler{gcService: gcService, logger: logger}
}

// RunGC handles POST /api/gc/run.
// Starts async garbage collection and returns 202 immediately.
func (h *GCHandler) RunGC(c *gin.Context) {
	startedAt, err := h.gcService.RunGC()
	if err != nil {
		if errors.Is(err, services.ErrGCAlreadyRunning) {
			c.JSON(http.StatusConflict, gin.H{"error": "gc already running"})
			return
		}
		if errors.Is(err, services.ErrRegistryUnavailable) {
			h.logger.Error("registry unavailable", "endpoint", "/api/gc/run", "error", err)
			c.JSON(http.StatusServiceUnavailable, gin.H{"error": "registry unavailable"})
			return
		}
		h.logger.Error("failed to start GC", "endpoint", "/api/gc/run", "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal server error"})
		return
	}

	c.JSON(http.StatusAccepted, dto.GCRunResponse{
		Status:    "running",
		StartedAt: startedAt,
	})
}

// GetStatus handles GET /api/gc/status.
// Returns current GC state and last run results.
func (h *GCHandler) GetStatus(c *gin.Context) {
	status := h.gcService.GetStatus()
	c.JSON(http.StatusOK, status)
}

// GetConfig handles GET /api/gcConfig.
// Returns the current GC schedule configuration.
func (h *GCHandler) GetConfig(c *gin.Context) {
	cfg, err := h.gcService.GetConfig()
	if err != nil {
		h.logger.Error("failed to get GC config", "endpoint", "/api/gcConfig", "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal server error"})
		return
	}
	c.JSON(http.StatusOK, cfg)
}

// UpdateConfig handles PUT /api/gcConfig.
// Validates and persists new GC schedule configuration.
func (h *GCHandler) UpdateConfig(c *gin.Context) {
	var req dto.UpdateGCConfigRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request body"})
		return
	}

	cfg, err := h.gcService.UpdateConfig(req)
	if err != nil {
		if errors.Is(err, services.ErrInvalidSchedule) {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid cron schedule"})
			return
		}
		h.logger.Error("failed to update GC config", "endpoint", "/api/gcConfig", "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal server error"})
		return
	}
	c.JSON(http.StatusOK, cfg)
}
