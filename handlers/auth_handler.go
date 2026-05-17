package handlers

import (
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"

	"registry_dashboard_api/models"
	"registry_dashboard_api/models/dto"
	"registry_dashboard_api/services"
)

// authServicer is the subset of services.AuthService used by AuthHandler.
// Using an interface allows isolated unit testing without a real database.
type authServicer interface {
	ValidateCredentials(username, password string) (*models.User, error)
	GenerateToken(username string) (string, error)
}

// AuthHandler handles HTTP requests for authentication endpoints.
type AuthHandler struct {
	authService  authServicer
	cookieSecure bool
}

// NewAuthHandler creates a new AuthHandler.
func NewAuthHandler(authService authServicer, cookieSecure bool) *AuthHandler {
	return &AuthHandler{authService: authService, cookieSecure: cookieSecure}
}

// Login handles POST /api/auth/login.
// On success it sets an httpOnly, SameSite=Lax cookie containing a signed JWT.
func (h *AuthHandler) Login(c *gin.Context) {
	var req dto.LoginRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(400, gin.H{"error": "invalid request"})
		return
	}

	_, err := h.authService.ValidateCredentials(req.Username, req.Password)
	if err != nil {
		if errors.Is(err, services.ErrInvalidCredentials) {
			c.JSON(401, gin.H{"error": "invalid credentials"})
			return
		}
		// Infrastructure failure (DB down, etc.) — do not reveal details to client.
		c.JSON(500, gin.H{"error": "internal server error"})
		return
	}

	tokenString, err := h.authService.GenerateToken(req.Username)
	if err != nil {
		c.JSON(500, gin.H{"error": "internal server error"})
		return
	}

	// SameSite=Lax blocks cross-site POST requests (CSRF protection) while
	// allowing the browser to send the cookie on same-site navigation.
	c.SetSameSite(http.SameSiteLaxMode)
	// maxAge 3600 = 1 hour, matching the JWT expiry.
	c.SetCookie("auth_token", tokenString, 3600, "/", "", h.cookieSecure, true)
	c.JSON(200, gin.H{"message": "login successful"})
}

// Logout handles POST /api/auth/logout.
// The route is protected by AuthMiddleware, so this handler only runs with a valid JWT.
func (h *AuthHandler) Logout(c *gin.Context) {
	c.SetSameSite(http.SameSiteLaxMode)
	// maxAge -1 sets Max-Age: 0 in the HTTP header, instructing the browser to delete the cookie.
	c.SetCookie("auth_token", "", -1, "/", "", h.cookieSecure, true)
	c.JSON(200, gin.H{"message": "logout successful"})
}
