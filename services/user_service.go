package services

import (
	"errors"
	"fmt"

	"github.com/DarioEB/logdeb"
	"github.com/jackc/pgx/v5/pgconn"
	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"

	"registry_dashboard_api/models"
	"registry_dashboard_api/models/dto"
)

var (
	// ErrUsernameExists is returned when creating a user with a taken username.
	ErrUsernameExists = errors.New("username already exists")
	// ErrUserNotFound is returned when the target user does not exist.
	ErrUserNotFound = errors.New("user not found")
)

// UserService handles user CRUD and htpasswd synchronisation.
type UserService struct {
	db       *gorm.DB
	htpasswd *HtpasswdService
	logger   *logdeb.Logdeb
}

// NewUserService creates a new UserService.
func NewUserService(db *gorm.DB, htpasswd *HtpasswdService, logger *logdeb.Logdeb) *UserService {
	return &UserService{db: db, htpasswd: htpasswd, logger: logger}
}

// ListUsers returns all users without password hashes.
func (s *UserService) ListUsers() ([]dto.UserResponse, error) {
	var users []models.User
	if err := s.db.Order("created_at ASC").Find(&users).Error; err != nil {
		return nil, fmt.Errorf("list users: %w", err)
	}

	result := make([]dto.UserResponse, len(users))
	for i, u := range users {
		result[i] = dto.UserResponse{ID: u.ID, Username: u.Username, CreatedAt: u.CreatedAt, UpdatedAt: u.UpdatedAt}
	}
	return result, nil
}

// CreateUser hashes the password, persists the user, and syncs the htpasswd file.
func (s *UserService) CreateUser(req dto.CreateUserRequest) (dto.UserResponse, error) {
	// Explicit duplicate check for clean error message.
	var existing models.User
	if err := s.db.Where("username = ?", req.Username).First(&existing).Error; err == nil {
		return dto.UserResponse{}, ErrUsernameExists
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(req.Password), bcrypt.DefaultCost)
	if err != nil {
		return dto.UserResponse{}, fmt.Errorf("hash password: %w", err)
	}

	user := models.User{Username: req.Username, PasswordHash: string(hash)}
	if err := s.db.Create(&user).Error; err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			return dto.UserResponse{}, ErrUsernameExists
		}
		return dto.UserResponse{}, fmt.Errorf("create user: %w", err)
	}

	s.logger.Info("user created", "username", req.Username)

	if err := s.htpasswd.Sync(); err != nil {
		s.logger.Error("htpasswd sync failed after create", "username", req.Username, "error", err)
	}

	return dto.UserResponse{ID: user.ID, Username: user.Username, CreatedAt: user.CreatedAt, UpdatedAt: user.UpdatedAt}, nil
}

// UpdatePassword changes a user's password and syncs the htpasswd file.
func (s *UserService) UpdatePassword(username string, req dto.UpdatePasswordRequest) (dto.UserResponse, error) {
	var user models.User
	if err := s.db.Where("username = ?", username).First(&user).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return dto.UserResponse{}, ErrUserNotFound
		}
		return dto.UserResponse{}, fmt.Errorf("find user: %w", err)
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(req.NewPassword), bcrypt.DefaultCost)
	if err != nil {
		return dto.UserResponse{}, fmt.Errorf("hash password: %w", err)
	}

	user.PasswordHash = string(hash)
	if err := s.db.Save(&user).Error; err != nil {
		return dto.UserResponse{}, fmt.Errorf("update user: %w", err)
	}

	s.logger.Info("user password updated", "username", username)

	if err := s.htpasswd.Sync(); err != nil {
		s.logger.Error("htpasswd sync failed after update", "username", username, "error", err)
	}

	return dto.UserResponse{ID: user.ID, Username: user.Username, CreatedAt: user.CreatedAt, UpdatedAt: user.UpdatedAt}, nil
}

// DeleteUser removes a user from the database and syncs the htpasswd file.
func (s *UserService) DeleteUser(username string) error {
	var user models.User
	if err := s.db.Where("username = ?", username).First(&user).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return ErrUserNotFound
		}
		return fmt.Errorf("find user: %w", err)
	}

	if err := s.db.Delete(&user).Error; err != nil {
		return fmt.Errorf("delete user: %w", err)
	}

	s.logger.Info("user deleted", "username", username)

	if err := s.htpasswd.Sync(); err != nil {
		s.logger.Error("htpasswd sync failed after delete", "username", username, "error", err)
	}

	return nil
}
