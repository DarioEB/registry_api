package handlers

import (
	"errors"
	"net/http"

	"github.com/DarioEB/logdeb"
	"github.com/gin-gonic/gin"

	"registry_dashboard_api/models/dto"
	"registry_dashboard_api/services"
)

// userServicer is the subset of *services.UserService used by UserHandler.
type userServicer interface {
	ListUsers() ([]dto.UserResponse, error)
	CreateUser(req dto.CreateUserRequest) (dto.UserResponse, error)
	UpdatePassword(username string, req dto.UpdatePasswordRequest) (dto.UserResponse, error)
	DeleteUser(username string) error
}

// UserHandler handles HTTP requests for user management endpoints.
type UserHandler struct {
	userService userServicer
	logger      *logdeb.Logdeb
}

// NewUserHandler creates a new UserHandler.
func NewUserHandler(userService userServicer, logger *logdeb.Logdeb) *UserHandler {
	return &UserHandler{userService: userService, logger: logger}
}

// ListUsers handles GET /api/users.
func (h *UserHandler) ListUsers(c *gin.Context) {
	users, err := h.userService.ListUsers()
	if err != nil {
		h.logger.Error("failed to list users", "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal server error"})
		return
	}
	if users == nil {
		users = []dto.UserResponse{}
	}
	c.JSON(http.StatusOK, users)
}

// CreateUser handles POST /api/users.
func (h *UserHandler) CreateUser(c *gin.Context) {
	var req dto.CreateUserRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	user, err := h.userService.CreateUser(req)
	if err != nil {
		if errors.Is(err, services.ErrUsernameExists) {
			c.JSON(http.StatusUnprocessableEntity, gin.H{"error": "username already exists"})
			return
		}
		h.logger.Error("failed to create user", "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal server error"})
		return
	}
	c.JSON(http.StatusCreated, user)
}

// UpdatePassword handles PUT /api/users/:username.
func (h *UserHandler) UpdatePassword(c *gin.Context) {
	username := c.Param("username")

	var req dto.UpdatePasswordRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	user, err := h.userService.UpdatePassword(username, req)
	if err != nil {
		if errors.Is(err, services.ErrUserNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"error": "user not found"})
			return
		}
		h.logger.Error("failed to update password", "username", username, "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal server error"})
		return
	}
	c.JSON(http.StatusOK, user)
}

// DeleteUser handles DELETE /api/users/:username.
func (h *UserHandler) DeleteUser(c *gin.Context) {
	username := c.Param("username")

	if err := h.userService.DeleteUser(username); err != nil {
		if errors.Is(err, services.ErrUserNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"error": "user not found"})
			return
		}
		h.logger.Error("failed to delete user", "username", username, "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal server error"})
		return
	}
	c.JSON(http.StatusOK, dto.DeleteUserResponse{Message: "user deleted"})
}
