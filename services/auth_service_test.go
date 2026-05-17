package services

import (
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"

	"registry_dashboard_api/models"
)

// ── Test helpers ──────────────────────────────────────────────────────────────

// mockUserRepo is a test double for UserRepository.
type mockUserRepo struct {
	findFn func(username string) (*models.User, error)
}

func (m *mockUserRepo) FindByUsername(username string) (*models.User, error) {
	return m.findFn(username)
}

// newTestService returns an AuthService with no DB (safe for JWT-only tests).
func newTestService(secret string) *AuthService {
	return NewAuthService(nil, secret)
}

// newTestServiceWithRepo returns an AuthService backed by a mock repository.
func newTestServiceWithRepo(repo UserRepository, secret string) *AuthService {
	return &AuthService{userRepo: repo, jwtSecret: secret}
}

const testSecret = "test-secret-min-32-bytes-padding!!"

// ── GenerateToken tests ───────────────────────────────────────────────────────

func TestGenerateToken_ReturnsNonEmptyString(t *testing.T) {
	svc := newTestService(testSecret)
	token, err := svc.GenerateToken("alice")
	if err != nil {
		t.Fatalf("GenerateToken error: %v", err)
	}
	if token == "" {
		t.Fatal("GenerateToken returned empty string")
	}
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		t.Errorf("expected 3 JWT segments, got %d", len(parts))
	}
}

// ── ValidateToken tests ───────────────────────────────────────────────────────

func TestValidateToken_ValidToken(t *testing.T) {
	svc := newTestService(testSecret)
	token, err := svc.GenerateToken("alice")
	if err != nil {
		t.Fatalf("GenerateToken error: %v", err)
	}

	claims, err := svc.ValidateToken(token)
	if err != nil {
		t.Fatalf("ValidateToken error: %v", err)
	}
	if claims.Subject != "alice" {
		t.Errorf("expected subject 'alice', got %q", claims.Subject)
	}
}

func TestValidateToken_ExpiredToken(t *testing.T) {
	svc := newTestService(testSecret)

	expiredClaims := Claims{
		RegisteredClaims: jwt.RegisteredClaims{
			Subject:   "alice",
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(-time.Hour)),
			IssuedAt:  jwt.NewNumericDate(time.Now().Add(-2 * time.Hour)),
		},
	}
	rawToken := jwt.NewWithClaims(jwt.SigningMethodHS256, expiredClaims)
	tokenString, err := rawToken.SignedString([]byte(svc.jwtSecret))
	if err != nil {
		t.Fatalf("failed to sign expired token: %v", err)
	}

	_, err = svc.ValidateToken(tokenString)
	if err == nil {
		t.Fatal("expected error for expired token, got nil")
	}
}

func TestValidateToken_WrongSecret(t *testing.T) {
	signer := newTestService("correct-secret-min-32-bytes-xxxx")
	verifier := newTestService("wrong-secret-min-32-bytes-xxxxxx")

	token, err := signer.GenerateToken("alice")
	if err != nil {
		t.Fatalf("GenerateToken error: %v", err)
	}

	_, err = verifier.ValidateToken(token)
	if err == nil {
		t.Fatal("expected error for wrong secret, got nil")
	}
}

func TestValidateToken_MalformedToken(t *testing.T) {
	svc := newTestService(testSecret)
	_, err := svc.ValidateToken("not.a.jwt")
	if err == nil {
		t.Fatal("expected error for malformed token, got nil")
	}
}

func TestValidateToken_EmptyToken(t *testing.T) {
	svc := newTestService(testSecret)
	_, err := svc.ValidateToken("")
	if err == nil {
		t.Fatal("expected error for empty token, got nil")
	}
}

// ── ValidateCredentials tests ─────────────────────────────────────────────────

func TestValidateCredentials_ValidUser(t *testing.T) {
	hash, err := bcrypt.GenerateFromPassword([]byte("correct-password"), bcrypt.MinCost)
	if err != nil {
		t.Fatalf("bcrypt error: %v", err)
	}
	repo := &mockUserRepo{
		findFn: func(username string) (*models.User, error) {
			if username == "alice" {
				return &models.User{Username: "alice", PasswordHash: string(hash)}, nil
			}
			return nil, gorm.ErrRecordNotFound
		},
	}
	svc := newTestServiceWithRepo(repo, testSecret)

	user, err := svc.ValidateCredentials("alice", "correct-password")
	if err != nil {
		t.Fatalf("expected nil error, got: %v", err)
	}
	if user.Username != "alice" {
		t.Errorf("expected user 'alice', got %q", user.Username)
	}
}

func TestValidateCredentials_WrongPassword_ReturnsErrInvalidCredentials(t *testing.T) {
	hash, err := bcrypt.GenerateFromPassword([]byte("correct-password"), bcrypt.MinCost)
	if err != nil {
		t.Fatalf("bcrypt error: %v", err)
	}
	repo := &mockUserRepo{
		findFn: func(_ string) (*models.User, error) {
			return &models.User{Username: "alice", PasswordHash: string(hash)}, nil
		},
	}
	svc := newTestServiceWithRepo(repo, testSecret)

	_, err = svc.ValidateCredentials("alice", "wrong-password")
	if !errors.Is(err, ErrInvalidCredentials) {
		t.Errorf("expected ErrInvalidCredentials, got: %v", err)
	}
}

func TestValidateCredentials_UserNotFound_ReturnsErrInvalidCredentials(t *testing.T) {
	repo := &mockUserRepo{
		findFn: func(_ string) (*models.User, error) {
			return nil, gorm.ErrRecordNotFound
		},
	}
	svc := newTestServiceWithRepo(repo, testSecret)

	_, err := svc.ValidateCredentials("nonexistent", "any-password")
	if !errors.Is(err, ErrInvalidCredentials) {
		t.Errorf("expected ErrInvalidCredentials, got: %v", err)
	}
}

func TestValidateCredentials_DatabaseError_ReturnsInfraError(t *testing.T) {
	dbErr := fmt.Errorf("connection refused")
	repo := &mockUserRepo{
		findFn: func(_ string) (*models.User, error) {
			return nil, dbErr
		},
	}
	svc := newTestServiceWithRepo(repo, testSecret)

	_, err := svc.ValidateCredentials("alice", "password")
	if err == nil {
		t.Fatal("expected error for DB failure, got nil")
	}
	if errors.Is(err, ErrInvalidCredentials) {
		t.Error("DB error should NOT be reported as ErrInvalidCredentials (would return 401 instead of 500)")
	}
}

// TestValidateCredentials_UniformErrorMessage ensures user-not-found and wrong-password
// return the same error string (prevents user enumeration via error message).
func TestValidateCredentials_UniformErrorMessage(t *testing.T) {
	hash, _ := bcrypt.GenerateFromPassword([]byte("correct"), bcrypt.MinCost)

	notFoundRepo := &mockUserRepo{findFn: func(_ string) (*models.User, error) {
		return nil, gorm.ErrRecordNotFound
	}}
	wrongPassRepo := &mockUserRepo{findFn: func(_ string) (*models.User, error) {
		return &models.User{PasswordHash: string(hash)}, nil
	}}

	svc1 := newTestServiceWithRepo(notFoundRepo, testSecret)
	svc2 := newTestServiceWithRepo(wrongPassRepo, testSecret)

	_, err1 := svc1.ValidateCredentials("nobody", "pass")
	_, err2 := svc2.ValidateCredentials("alice", "wrong")

	if err1.Error() != err2.Error() {
		t.Errorf("error messages differ: %q vs %q (enables user enumeration)", err1.Error(), err2.Error())
	}
}
