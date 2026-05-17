package dto

import "time"

// UserResponse is returned by all user endpoints. Excludes password_hash.
type UserResponse struct {
	ID        uint      `json:"id"`
	Username  string    `json:"username"`
	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
}

// CreateUserRequest is the body for POST /api/users.
type CreateUserRequest struct {
	Username string `json:"username" binding:"required,min=1,max=50,alphanum"`
	Password string `json:"password" binding:"required,min=6"`
}

// UpdatePasswordRequest is the body for PUT /api/users/:username.
type UpdatePasswordRequest struct {
	NewPassword string `json:"newPassword" binding:"required,min=6"`
}

// DeleteUserResponse is returned by DELETE /api/users/:username.
type DeleteUserResponse struct {
	Message string `json:"message"`
}
