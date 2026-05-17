package services

import (
	"bytes"
	"fmt"
	"os"

	"github.com/DarioEB/logdeb"
	"gorm.io/gorm"

	"registry_dashboard_api/models"
)

// HtpasswdService synchronises the htpasswd file with the users table.
// PostgreSQL is the source of truth; the htpasswd file is a derived artefact
// consumed by registry:2 for basic auth.
type HtpasswdService struct {
	filePath string
	db       *gorm.DB
	logger   *logdeb.Logdeb
}

// NewHtpasswdService creates a new HtpasswdService.
func NewHtpasswdService(filePath string, db *gorm.DB, logger *logdeb.Logdeb) *HtpasswdService {
	return &HtpasswdService{filePath: filePath, db: db, logger: logger}
}

// Sync reads all users from PostgreSQL and atomically rewrites the htpasswd
// file. It uses a write-tmp-then-rename strategy to avoid leaving a corrupt
// file on crash or interruption.
func (s *HtpasswdService) Sync() error {
	var users []models.User
	if err := s.db.Find(&users).Error; err != nil {
		return fmt.Errorf("read users: %w", err)
	}

	var buf bytes.Buffer
	for _, u := range users {
		fmt.Fprintf(&buf, "%s:%s\n", u.Username, u.PasswordHash)
	}

	tmpPath := s.filePath + ".tmp"
	if err := os.WriteFile(tmpPath, buf.Bytes(), 0644); err != nil {
		return fmt.Errorf("write htpasswd tmp: %w", err)
	}
	if err := os.Rename(tmpPath, s.filePath); err != nil {
		// Clean up the temp file on rename failure.
		os.Remove(tmpPath)
		return fmt.Errorf("rename htpasswd: %w", err)
	}
	return nil
}
