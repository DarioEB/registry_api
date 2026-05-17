package models

import "time"

// GCConfig represents the garbage collection configuration stored in gc_configs table.
// There is always exactly one row (seeded by migration 000002).
type GCConfig struct {
	ID        uint      `gorm:"primaryKey" json:"id"`
	Schedule  string    `gorm:"type:varchar(100);not null;default:'0 3 * * *'" json:"schedule"`
	Enabled   bool      `gorm:"not null;default:true" json:"enabled"`
	UpdatedAt time.Time `gorm:"autoUpdateTime" json:"updatedAt"`
}

func (GCConfig) TableName() string {
	return "gc_configs"
}
