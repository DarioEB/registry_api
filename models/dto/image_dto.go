package dto

import "time"

// ImageListItem is the JSON response item for GET /api/images.
// Tags is the list of tag names associated with the image (empty if dangling).
// Size is the total uncompressed layer size in bytes.
// PushedAt is derived from the image config blob's created timestamp.
// IsDangling is true when the repository has no tags.
type ImageListItem struct {
	Name       string    `json:"name"`
	Tags       []string  `json:"tags"`
	Size       int64     `json:"size"`
	PushedAt   time.Time `json:"pushedAt"`
	Author     string    `json:"author"`
	IsDangling bool      `json:"isDangling"`
}

// ImageTag is the JSON response item for GET /api/images/:imageName/tags.
// Digest is the manifest digest from the Docker-Content-Digest response header.
// Size is the total uncompressed layer size in bytes.
type ImageTag struct {
	Tag      string    `json:"tag"`
	Digest   string    `json:"digest"`
	Size     int64     `json:"size"`
	PushedAt time.Time `json:"pushedAt"`
}
