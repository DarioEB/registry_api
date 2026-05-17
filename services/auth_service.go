package services

import (
	"errors"
	"fmt"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"

	"registry_dashboard_api/models"
)

// ErrInvalidCredentials is returned by ValidateCredentials when authentication fails
// due to a wrong username or password. It is distinct from infrastructure errors
// so callers can map it to 401 instead of 500.
var ErrInvalidCredentials = errors.New("invalid credentials")

// sentinelHash is a pre-computed bcrypt hash used to equalise response time when a
// username is not found, preventing user-enumeration via timing side-channels.
var sentinelHash []byte

func init() {
	h, err := bcrypt.GenerateFromPassword([]byte("__auth_sentinel_value__"), bcrypt.DefaultCost)
	if err != nil {
		panic("auth: failed to initialise sentinel hash: " + err.Error())
	}
	sentinelHash = h
}

// Claims wraps JWT registered claims. Exported so middleware can reference the type.
type Claims struct {
	jwt.RegisteredClaims
}

// UserRepository abstracts user lookups from the database.
// The production implementation is backed by GORM; tests inject a mock.
type UserRepository interface {
	FindByUsername(username string) (*models.User, error)
}

// gormUserRepository is the GORM-backed UserRepository.
type gormUserRepository struct {
	db *gorm.DB
}

func (r *gormUserRepository) FindByUsername(username string) (*models.User, error) {
	var user models.User
	if err := r.db.Where("username = ?", username).First(&user).Error; err != nil {
		return nil, err
	}
	return &user, nil
}

// AuthService handles credential validation, JWT generation and JWT validation.
type AuthService struct {
	userRepo  UserRepository
	jwtSecret string
}

// NewAuthService creates a new AuthService backed by a GORM database.
func NewAuthService(db *gorm.DB, jwtSecret string) *AuthService {
	return &AuthService{
		userRepo:  &gormUserRepository{db: db},
		jwtSecret: jwtSecret,
	}
}

// ValidateCredentials looks up the user by username and verifies the password against
// the stored bcrypt hash. It returns ErrInvalidCredentials for any auth failure,
// keeping the error message uniform to prevent user enumeration.
// A dummy bcrypt comparison is performed when the user is not found to equalise
// response time with the "wrong password" path (timing side-channel defence).
func (s *AuthService) ValidateCredentials(username, password string) (*models.User, error) {
	user, err := s.userRepo.FindByUsername(username)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			// Constant-time equaliser: prevents distinguishing "no such user" from
			// "wrong password" by measuring response latency.
			//nolint:errcheck
			bcrypt.CompareHashAndPassword(sentinelHash, []byte(password))
			return nil, ErrInvalidCredentials
		}
		return nil, fmt.Errorf("database error: %w", err)
	}

	if err := bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(password)); err != nil {
		return nil, ErrInvalidCredentials
	}

	return user, nil
}

// GenerateToken creates a signed HS256 JWT with the given username as subject and a 1-hour expiry.
func (s *AuthService) GenerateToken(username string) (string, error) {
	claims := Claims{
		RegisteredClaims: jwt.RegisteredClaims{
			Subject:   username,
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Hour)),
			IssuedAt:  jwt.NewNumericDate(time.Now()),
		},
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	return token.SignedString([]byte(s.jwtSecret))
}

// ValidateToken parses and validates a JWT string. Returns Claims on success.
func (s *AuthService) ValidateToken(tokenString string) (*Claims, error) {
	claims := &Claims{}
	token, err := jwt.ParseWithClaims(tokenString, claims, func(token *jwt.Token) (interface{}, error) {
		if _, ok := token.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, fmt.Errorf("unexpected signing method: %v", token.Header["alg"])
		}
		return []byte(s.jwtSecret), nil
	})
	if err != nil || !token.Valid {
		return nil, errors.New("invalid token")
	}
	return claims, nil
}
