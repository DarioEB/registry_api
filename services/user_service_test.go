package services

import (
	"os"
	"strings"
	"testing"

	"github.com/DarioEB/logdeb"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"

	"registry_dashboard_api/models"
	"registry_dashboard_api/models/dto"
)

// testUserLogger is a shared logger for user service tests.
var testUserLogger *logdeb.Logdeb

func init() {
	var err error
	testUserLogger, err = logdeb.New(logdeb.DefaultConfig())
	if err != nil {
		panic("failed to create test logger: " + err.Error())
	}
}

// setupUserTestDB connects to the test database and cleans the users table.
func setupUserTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("TEST_DATABASE_URL not set, skipping database tests")
	}
	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{})
	if err != nil {
		t.Fatalf("failed to connect to test database: %v", err)
	}
	// Clean up before test
	db.Exec("DELETE FROM users")
	return db
}

func TestCreateUser_HashesPassword(t *testing.T) {
	db := setupUserTestDB(t)
	htpasswd := NewHtpasswdService("/dev/null", db, testUserLogger)
	svc := NewUserService(db, htpasswd, testUserLogger)

	resp, err := svc.CreateUser(dto.CreateUserRequest{Username: "hashtest", Password: "secret123"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.Username != "hashtest" {
		t.Fatalf("expected username 'hashtest', got '%s'", resp.Username)
	}

	// Verify hash is stored (not plaintext)
	var user models.User
	db.Where("username = ?", "hashtest").First(&user)
	if user.PasswordHash == "secret123" {
		t.Fatal("password stored as plaintext instead of bcrypt hash")
	}
	if len(user.PasswordHash) < 50 {
		t.Fatalf("password hash too short (%d chars), expected bcrypt hash", len(user.PasswordHash))
	}
}

func TestCreateUser_DuplicateUsername(t *testing.T) {
	db := setupUserTestDB(t)
	htpasswd := NewHtpasswdService("/dev/null", db, testUserLogger)
	svc := NewUserService(db, htpasswd, testUserLogger)

	_, err := svc.CreateUser(dto.CreateUserRequest{Username: "dupe", Password: "secret123"})
	if err != nil {
		t.Fatalf("first create failed: %v", err)
	}

	_, err = svc.CreateUser(dto.CreateUserRequest{Username: "dupe", Password: "secret456"})
	if err != ErrUsernameExists {
		t.Fatalf("expected ErrUsernameExists, got %v", err)
	}
}

func TestListUsers_ExcludesPasswordHash(t *testing.T) {
	db := setupUserTestDB(t)
	htpasswd := NewHtpasswdService("/dev/null", db, testUserLogger)
	svc := NewUserService(db, htpasswd, testUserLogger)

	svc.CreateUser(dto.CreateUserRequest{Username: "listuser", Password: "secret123"})

	users, err := svc.ListUsers()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(users) != 1 {
		t.Fatalf("expected 1 user, got %d", len(users))
	}
	// UserResponse struct does not have PasswordHash field — compilation proves exclusion
	if users[0].Username != "listuser" {
		t.Fatalf("expected 'listuser', got '%s'", users[0].Username)
	}
}

func TestDeleteUser_SyncsHtpasswd(t *testing.T) {
	db := setupUserTestDB(t)

	// Use a real temp file for htpasswd to verify sync
	tmpFile, err := os.CreateTemp("", "htpasswd-test-*")
	if err != nil {
		t.Fatalf("failed to create temp file: %v", err)
	}
	defer os.Remove(tmpFile.Name())
	tmpFile.Close()

	htpasswd := NewHtpasswdService(tmpFile.Name(), db, testUserLogger)
	svc := NewUserService(db, htpasswd, testUserLogger)

	svc.CreateUser(dto.CreateUserRequest{Username: "tobedeleted", Password: "secret123"})
	svc.CreateUser(dto.CreateUserRequest{Username: "tokeep", Password: "secret456"})

	// Delete one user
	if err := svc.DeleteUser("tobedeleted"); err != nil {
		t.Fatalf("delete failed: %v", err)
	}

	// Verify htpasswd contains only remaining user
	content, err := os.ReadFile(tmpFile.Name())
	if err != nil {
		t.Fatalf("failed to read htpasswd: %v", err)
	}
	if strings.Contains(string(content), "tobedeleted") {
		t.Fatal("htpasswd still contains deleted user")
	}
	if !strings.Contains(string(content), "tokeep") {
		t.Fatal("htpasswd missing remaining user")
	}
}
