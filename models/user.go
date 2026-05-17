package models

import "time"

// User maps to the users table created by migration 000001.
type User struct {
	ID           uint      `json:"id"       gorm:"primaryKey"`
	Username     string    `json:"username" gorm:"uniqueIndex;not null"`
	PasswordHash string    `json:"-"        gorm:"column:password_hash;not null"`
	CreatedAt    time.Time `json:"createdAt"`
	UpdatedAt    time.Time `json:"updatedAt"`
}
