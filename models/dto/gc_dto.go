package dto

import "time"

// GCRunResponse is returned by POST /api/gc/run on success (202).
type GCRunResponse struct {
	Status    string    `json:"status"`
	StartedAt time.Time `json:"startedAt"`
}

// GCStatusResponse is returned by GET /api/gc/status.
type GCStatusResponse struct {
	Status    string     `json:"status"`
	StartedAt *time.Time `json:"startedAt,omitempty"`
	LastRun   *GCLastRun `json:"lastRun"`
}

// GCLastRun holds the results of the most recent GC execution.
type GCLastRun struct {
	DeletedCount int       `json:"deletedCount"`
	FreedBytes   int64     `json:"freedBytes"`
	CompletedAt  time.Time `json:"completedAt"`
	Error        string    `json:"error,omitempty"`
}

// GCConfigResponse is returned by GET /api/gcConfig.
type GCConfigResponse struct {
	Schedule string `json:"schedule"`
	Enabled  bool   `json:"enabled"`
}

// UpdateGCConfigRequest is the body for PUT /api/gcConfig.
// Pointers distinguish missing fields from zero values.
type UpdateGCConfigRequest struct {
	Schedule *string `json:"schedule"`
	Enabled  *bool   `json:"enabled"`
}
