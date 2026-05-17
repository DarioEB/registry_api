package middleware

import (
	"github.com/gin-gonic/gin"

	"registry_dashboard_api/services"
)

// tokenValidator is satisfied by *services.AuthService, enabling isolated unit testing.
type tokenValidator interface {
	ValidateToken(tokenString string) (*services.Claims, error)
}

// AuthMiddleware validates the JWT stored in the "auth_token" httpOnly cookie.
// On success it sets "username" in the Gin context and calls Next().
// On failure it aborts with 401.
func AuthMiddleware(svc tokenValidator) gin.HandlerFunc {
	return func(c *gin.Context) {
		tokenString, err := c.Cookie("auth_token")
		if err != nil || tokenString == "" {
			c.AbortWithStatusJSON(401, gin.H{"error": "unauthorized"})
			return
		}

		claims, err := svc.ValidateToken(tokenString)
		if err != nil {
			c.AbortWithStatusJSON(401, gin.H{"error": "unauthorized"})
			return
		}

		c.Set("username", claims.Subject)
		c.Next()
	}
}
