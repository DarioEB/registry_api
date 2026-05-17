package handlers

import (
	"errors"
	"net/http"

	"github.com/DarioEB/logdeb"
	"github.com/gin-gonic/gin"

	"registry_dashboard_api/models/dto"
	"registry_dashboard_api/services"
)

// imageServicer is the subset of *services.ImageService used by ImageHandler.
// The interface allows isolated unit testing without a live Docker registry.
type imageServicer interface {
	GetImages() ([]dto.ImageListItem, error)
	GetImageTags(imageName string) ([]dto.ImageTag, error)
}

// ImageHandler handles HTTP requests for image-related endpoints.
type ImageHandler struct {
	imageService imageServicer
	logger       *logdeb.Logdeb
}

// NewImageHandler creates a new ImageHandler.
func NewImageHandler(imageService imageServicer, logger *logdeb.Logdeb) *ImageHandler {
	return &ImageHandler{imageService: imageService, logger: logger}
}

// ListImages handles GET /api/images.
// Returns all images sorted by pushedAt descending. Always returns an array (never null).
func (h *ImageHandler) ListImages(c *gin.Context) {
	images, err := h.imageService.GetImages()
	if err != nil {
		if errors.Is(err, services.ErrRegistryUnavailable) {
			h.logger.Error("registry unavailable", "endpoint", "/api/images", "error", err)
			c.JSON(http.StatusServiceUnavailable, gin.H{"error": "registry unavailable"})
			return
		}
		if errors.Is(err, services.ErrImageNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"error": "image not found"})
			return
		}
		h.logger.Error("failed to list images", "endpoint", "/api/images", "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal server error"})
		return
	}

	// Ensure JSON array is never null when registry returns no images.
	if images == nil {
		images = []dto.ImageListItem{}
	}
	c.JSON(http.StatusOK, images)
}

// ListImageTags handles GET /api/images/:imageName/tags.
// Returns all tags for the specified image. Always returns an array (never null).
func (h *ImageHandler) ListImageTags(c *gin.Context) {
	imageName := c.Param("imageName")

	tags, err := h.imageService.GetImageTags(imageName)
	if err != nil {
		if errors.Is(err, services.ErrRegistryUnavailable) {
			h.logger.Error("registry unavailable", "endpoint", "/api/images/:imageName/tags", "imageName", imageName, "error", err)
			c.JSON(http.StatusServiceUnavailable, gin.H{"error": "registry unavailable"})
			return
		}
		if errors.Is(err, services.ErrImageNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"error": "image not found"})
			return
		}
		h.logger.Error("failed to list image tags", "endpoint", "/api/images/:imageName/tags", "imageName", imageName, "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal server error"})
		return
	}

	// Ensure JSON array is never null when image has no tags.
	if tags == nil {
		tags = []dto.ImageTag{}
	}
	c.JSON(http.StatusOK, tags)
}
