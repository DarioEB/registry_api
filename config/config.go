package config

import (
	"fmt"

	"github.com/DarioEB/envdeb"
)

// Config holds all environment-based configuration for the application.
type Config struct {
	Port               string
	DBHost             string
	DBPort             string
	DBName             string
	DBUser             string
	DBPassword         string
	JWTSecret          string
	RegistryURL        string
	RegistryAdminUser  string
	RegistryAdminPass  string
	CORSAllowedOrigins string
	HtpasswdPath       string
	CookieSecure       bool // set to true in production behind nginx with HTTPS
}

// Load reads configuration from environment variables.
// It attempts to load a .env file first (ignored if not present, as vars
// may be injected directly by Docker or the OS). Returns an error naming
// the first missing required variable.
func Load() (*Config, error) {
	// Load .env file if present; ignore error — in Docker, vars come from env directly.
	_ = envdeb.Load()

	cfg := &Config{}

	required := []struct {
		name string
		dest *string
	}{
		{"PORT", &cfg.Port},
		{"DB_HOST", &cfg.DBHost},
		{"DB_PORT", &cfg.DBPort},
		{"DB_NAME", &cfg.DBName},
		{"DB_USER", &cfg.DBUser},
		{"DB_PASSWORD", &cfg.DBPassword},
		{"JWT_SECRET", &cfg.JWTSecret},
		{"REGISTRY_URL", &cfg.RegistryURL},
		{"REGISTRY_ADMIN_USER", &cfg.RegistryAdminUser},
		{"REGISTRY_ADMIN_PASSWORD", &cfg.RegistryAdminPass},
		{"CORS_ALLOWED_ORIGINS", &cfg.CORSAllowedOrigins},
		{"HTPASSWD_PATH", &cfg.HtpasswdPath},
	}

	for _, r := range required {
		val := envdeb.Get(r.name)
		if val == "" {
			return nil, fmt.Errorf("%s is required", r.name)
		}
		*r.dest = val
	}

	// JWT_SECRET must be at least 32 characters to provide adequate HMAC-SHA256 security.
	if len(cfg.JWTSecret) < 32 {
		return nil, fmt.Errorf("JWT_SECRET must be at least 32 characters (got %d)", len(cfg.JWTSecret))
	}

	// Optional: COOKIE_SECURE defaults to false for local dev; set to "true" in production.
	cfg.CookieSecure = envdeb.Get("COOKIE_SECURE") == "true"

	return cfg, nil
}
