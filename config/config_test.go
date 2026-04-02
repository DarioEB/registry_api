package config

import (
	"os"
	"strings"
	"testing"
)

// requiredVarNames lists all env vars that config.Load() requires.
var requiredVarNames = []string{
	"PORT", "DB_HOST", "DB_PORT", "DB_NAME", "DB_USER",
	"DB_PASSWORD", "JWT_SECRET", "REGISTRY_URL",
	"REGISTRY_ADMIN_USER", "REGISTRY_ADMIN_PASSWORD", "CORS_ALLOWED_ORIGINS",
}

// setAllEnv sets all required environment variables using t.Setenv (auto-cleanup).
func setAllEnv(t *testing.T, overrides map[string]string) {
	t.Helper()
	defaults := map[string]string{
		"PORT":                    "8080",
		"DB_HOST":                 "localhost",
		"DB_PORT":                 "5432",
		"DB_NAME":                 "testdb",
		"DB_USER":                 "testuser",
		"DB_PASSWORD":             "testpass",
		"JWT_SECRET":              "testsecret",
		"REGISTRY_URL":            "http://localhost:5000",
		"REGISTRY_ADMIN_USER":     "admin",
		"REGISTRY_ADMIN_PASSWORD": "adminpass",
		"CORS_ALLOWED_ORIGINS":    "http://localhost:3000",
	}
	for k, v := range overrides {
		defaults[k] = v
	}
	for k, v := range defaults {
		t.Setenv(k, v)
	}
}

func TestLoad_AllVarsPresent(t *testing.T) {
	setAllEnv(t, nil)

	cfg, err := Load()
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}

	// Verify every field is mapped correctly
	checks := map[string]struct{ got, want string }{
		"Port":               {cfg.Port, "8080"},
		"DBHost":             {cfg.DBHost, "localhost"},
		"DBPort":             {cfg.DBPort, "5432"},
		"DBName":             {cfg.DBName, "testdb"},
		"DBUser":             {cfg.DBUser, "testuser"},
		"DBPassword":         {cfg.DBPassword, "testpass"},
		"JWTSecret":          {cfg.JWTSecret, "testsecret"},
		"RegistryURL":        {cfg.RegistryURL, "http://localhost:5000"},
		"RegistryAdminUser":  {cfg.RegistryAdminUser, "admin"},
		"RegistryAdminPass":  {cfg.RegistryAdminPass, "adminpass"},
		"CORSAllowedOrigins": {cfg.CORSAllowedOrigins, "http://localhost:3000"},
	}
	for field, c := range checks {
		if c.got != c.want {
			t.Errorf("Config.%s = %q, want %q", field, c.got, c.want)
		}
	}
}

func TestLoad_MissingRequiredVar(t *testing.T) {
	for _, varName := range requiredVarNames {
		t.Run("missing_"+varName, func(t *testing.T) {
			setAllEnv(t, nil)
			os.Unsetenv(varName)

			_, err := Load()
			if err == nil {
				t.Fatalf("expected error for missing %s, got nil", varName)
			}
			if !strings.Contains(err.Error(), varName) {
				t.Errorf("error message should contain %q, got: %q", varName, err.Error())
			}
		})
	}
}
