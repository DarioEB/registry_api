package config

import (
	"strings"
	"testing"
)

// jwtSecretValid is a 32-character secret that satisfies the minimum-length requirement.
const jwtSecretValid = "test-jwt-secret-min-32-chars-ok!!"

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
		"JWT_SECRET":              jwtSecretValid,
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

	checks := map[string]struct{ got, want string }{
		"Port":               {cfg.Port, "8080"},
		"DBHost":             {cfg.DBHost, "localhost"},
		"DBPort":             {cfg.DBPort, "5432"},
		"DBName":             {cfg.DBName, "testdb"},
		"DBUser":             {cfg.DBUser, "testuser"},
		"DBPassword":         {cfg.DBPassword, "testpass"},
		"JWTSecret":          {cfg.JWTSecret, jwtSecretValid},
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

func TestLoad_JWTSecret_TooShort_ReturnsError(t *testing.T) {
	setAllEnv(t, map[string]string{"JWT_SECRET": "tooshort"})

	_, err := Load()
	if err == nil {
		t.Fatal("expected error for short JWT_SECRET, got nil")
	}
	if !strings.Contains(err.Error(), "JWT_SECRET") {
		t.Errorf("error should mention JWT_SECRET, got: %q", err.Error())
	}
}

func TestLoad_CookieSecure_DefaultsFalse(t *testing.T) {
	setAllEnv(t, nil)
	// Use t.Setenv("", "") to ensure COOKIE_SECURE is empty (t.Setenv auto-restores on cleanup).
	t.Setenv("COOKIE_SECURE", "")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.CookieSecure {
		t.Error("expected CookieSecure to be false when COOKIE_SECURE is empty")
	}
}

func TestLoad_CookieSecure_TrueWhenSet(t *testing.T) {
	setAllEnv(t, nil)
	t.Setenv("COOKIE_SECURE", "true")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !cfg.CookieSecure {
		t.Error("expected CookieSecure to be true when COOKIE_SECURE=true")
	}
}

func TestLoad_MissingRequiredVar(t *testing.T) {
	for _, varName := range requiredVarNames {
		t.Run("missing_"+varName, func(t *testing.T) {
			setAllEnv(t, nil)
			// Use t.Setenv with empty string — config.Load() treats "" as missing.
			// This avoids os.Unsetenv which modifies the global environment without cleanup.
			t.Setenv(varName, "")

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
