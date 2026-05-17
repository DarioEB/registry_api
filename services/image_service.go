package services

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"sort"
	"time"

	"golang.org/x/sync/errgroup"

	"registry_dashboard_api/models/dto"
)

// ErrRegistryUnavailable is returned when the Docker registry is unreachable
// (connection refused, timeout, DNS failure, or 5xx response).
// Callers should map this to 503 Service Unavailable.
var ErrRegistryUnavailable = errors.New("registry unavailable")

// ErrImageNotFound is returned when the registry responds with 404 for an
// image or tag lookup. Callers should map this to 404 Not Found.
var ErrImageNotFound = errors.New("image not found")

// maxErrorBodySize caps how many bytes we read from registry error responses
// to prevent unbounded memory consumption from unexpected payloads.
const maxErrorBodySize = 1024

// ImageService fetches image metadata from a Docker Registry HTTP API v2 endpoint.
// It wraps all registry requests with Basic Auth using the provided admin credentials.
type ImageService struct {
	registryURL       string
	registryAdminUser string
	registryAdminPass string
	httpClient        *http.Client
}

// NewImageService creates an ImageService backed by the given registry URL and credentials.
// An http.Client with a 10-second timeout is created to bound individual requests.
func NewImageService(registryURL, adminUser, adminPass string) *ImageService {
	return &ImageService{
		registryURL:       registryURL,
		registryAdminUser: adminUser,
		registryAdminPass: adminPass,
		httpClient:        &http.Client{Timeout: 10 * time.Second},
	}
}

// ── Registry response types (private) ────────────────────────────────────────

type catalogResponse struct {
	Repositories []string `json:"repositories"`
}

type tagsResponse struct {
	Name string   `json:"name"`
	Tags []string `json:"tags"` // may be null when image has no tags
}

type manifestLayer struct {
	Size int64 `json:"size"`
}

type manifestConfig struct {
	Digest string `json:"digest"`
	Size   int64  `json:"size"`
}

type manifestV2 struct {
	SchemaVersion int            `json:"schemaVersion"`
	Config        manifestConfig `json:"config"`
	Layers        []manifestLayer `json:"layers"`
}

type imageConfig struct {
	Created string `json:"created"` // RFC3339Nano string
	Author  string `json:"author"`
}

// ── Public methods ────────────────────────────────────────────────────────────

// GetImages fetches the complete image list from the registry, enriched with
// metadata (tags, size, pushedAt, author, isDangling). Images are ordered by
// pushedAt descending (most recent first). Dangling images (no tags) have
// size=0, zero pushedAt, and empty author.
func (s *ImageService) GetImages() ([]dto.ImageListItem, error) {
	ctx := context.Background()

	// 1. Fetch catalog
	var catalog catalogResponse
	if err := s.doRegistryJSON(ctx, http.MethodGet, "/v2/_catalog", nil, &catalog); err != nil {
		return nil, err
	}

	// 2. Fetch metadata for each image concurrently to meet AC#1 "< 3 seconds".
	// errgroup cancels all goroutines on first error and propagates it.
	images := make([]dto.ImageListItem, len(catalog.Repositories))
	g, gctx := errgroup.WithContext(ctx)
	g.SetLimit(10) // cap concurrent registry calls

	for i, name := range catalog.Repositories {
		g.Go(func() error {
			item, err := s.buildImageListItem(gctx, name)
			if err != nil {
				return err
			}
			images[i] = item
			return nil
		})
	}

	if err := g.Wait(); err != nil {
		return nil, err
	}

	// 3. Sort by pushedAt descending (most recent first).
	sort.Slice(images, func(i, j int) bool {
		return images[i].PushedAt.After(images[j].PushedAt)
	})

	return images, nil
}

// GetImageTags fetches the tag list for a specific image, each enriched with
// digest, size, and pushedAt metadata.
func (s *ImageService) GetImageTags(imageName string) ([]dto.ImageTag, error) {
	ctx := context.Background()

	var tagsResp tagsResponse
	if err := s.doRegistryJSON(ctx, http.MethodGet, fmt.Sprintf("/v2/%s/tags/list", imageName), nil, &tagsResp); err != nil {
		return nil, err
	}

	tags := make([]dto.ImageTag, 0, len(tagsResp.Tags))

	for _, tag := range tagsResp.Tags {
		manifest, digest, err := s.getManifestV2(ctx, imageName, tag)
		if err != nil {
			return nil, err
		}

		var pushedAt time.Time
		if manifest.Config.Digest != "" {
			cfg, err := s.getConfigBlob(ctx, imageName, manifest.Config.Digest)
			if err != nil {
				return nil, err
			}
			pushedAt = parseCreated(cfg.Created)
		}

		tags = append(tags, dto.ImageTag{
			Tag:      tag,
			Digest:   digest,
			Size:     layersTotalSize(manifest.Layers),
			PushedAt: pushedAt,
		})
	}

	return tags, nil
}

// ── Private helpers ───────────────────────────────────────────────────────────

// buildImageListItem constructs an ImageListItem for the given repository name.
// For repos without tags, isDangling=true and size/pushedAt/author are zero values.
func (s *ImageService) buildImageListItem(ctx context.Context, name string) (dto.ImageListItem, error) {
	var tagsResp tagsResponse
	if err := s.doRegistryJSON(ctx, http.MethodGet, fmt.Sprintf("/v2/%s/tags/list", name), nil, &tagsResp); err != nil {
		return dto.ImageListItem{}, err
	}

	// Treat null tags as empty slice
	if tagsResp.Tags == nil {
		tagsResp.Tags = []string{}
	}

	item := dto.ImageListItem{
		Name:       name,
		Tags:       tagsResp.Tags,
		IsDangling: len(tagsResp.Tags) == 0,
	}

	if !item.IsDangling {
		// Use the first tag as representative for size/pushedAt/author metadata.
		// This is a pragmatic choice — all tags in a repo typically share the same base layers.
		representativeTag := tagsResp.Tags[0]

		manifest, _, err := s.getManifestV2(ctx, name, representativeTag)
		if err != nil {
			return dto.ImageListItem{}, err
		}
		item.Size = layersTotalSize(manifest.Layers)

		if manifest.Config.Digest != "" {
			cfg, err := s.getConfigBlob(ctx, name, manifest.Config.Digest)
			if err != nil {
				return dto.ImageListItem{}, err
			}
			item.PushedAt = parseCreated(cfg.Created)
			item.Author = cfg.Author
		}
	}

	return item, nil
}

// doRegistryRequest performs an authenticated HTTP request against the registry.
// It sets Basic Auth and returns the raw response. The caller is responsible for
// closing resp.Body. Network errors and 5xx responses are wrapped as ErrRegistryUnavailable.
func (s *ImageService) doRegistryRequest(ctx context.Context, method, path string, extraHeaders map[string]string) (*http.Response, error) {
	url := s.registryURL + path
	req, err := http.NewRequestWithContext(ctx, method, url, nil)
	if err != nil {
		return nil, fmt.Errorf("build request: %w", err)
	}
	req.SetBasicAuth(s.registryAdminUser, s.registryAdminPass)
	for k, v := range extraHeaders {
		req.Header.Set(k, v)
	}

	resp, err := s.httpClient.Do(req)
	if err != nil {
		// Covers: connection refused, timeout, DNS failure, TLS errors
		return nil, fmt.Errorf("%w: %v", ErrRegistryUnavailable, err)
	}
	if resp.StatusCode >= 500 {
		resp.Body.Close()
		return nil, fmt.Errorf("%w: registry returned %d for %s", ErrRegistryUnavailable, resp.StatusCode, path)
	}
	if resp.StatusCode == http.StatusNotFound {
		resp.Body.Close()
		return nil, fmt.Errorf("%w: %s", ErrImageNotFound, path)
	}
	return resp, nil
}

// doRegistryJSON performs an authenticated GET and decodes the JSON body into dst.
func (s *ImageService) doRegistryJSON(ctx context.Context, method, path string, extraHeaders map[string]string, dst any) error {
	resp, err := s.doRegistryRequest(ctx, method, path, extraHeaders)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, maxErrorBodySize))
		return fmt.Errorf("registry error %d for %s: %s", resp.StatusCode, path, string(body))
	}

	if err := json.NewDecoder(resp.Body).Decode(dst); err != nil {
		return fmt.Errorf("decode registry response for %s: %w", path, err)
	}
	return nil
}

// getManifestV2 fetches a Docker manifest v2 for the given image and reference (tag or digest).
// Returns the parsed manifest and the manifest digest from Docker-Content-Digest header.
func (s *ImageService) getManifestV2(ctx context.Context, imageName, ref string) (*manifestV2, string, error) {
	path := fmt.Sprintf("/v2/%s/manifests/%s", imageName, ref)
	headers := map[string]string{
		"Accept": "application/vnd.docker.distribution.manifest.v2+json",
	}

	resp, err := s.doRegistryRequest(ctx, http.MethodGet, path, headers)
	if err != nil {
		return nil, "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, maxErrorBodySize))
		return nil, "", fmt.Errorf("registry error %d fetching manifest %s@%s: %s", resp.StatusCode, imageName, ref, string(body))
	}

	digest := resp.Header.Get("Docker-Content-Digest")

	var manifest manifestV2
	if err := json.NewDecoder(resp.Body).Decode(&manifest); err != nil {
		return nil, "", fmt.Errorf("decode manifest for %s@%s: %w", imageName, ref, err)
	}
	return &manifest, digest, nil
}

// getConfigBlob fetches and parses the image config blob identified by the given digest.
func (s *ImageService) getConfigBlob(ctx context.Context, imageName, digest string) (*imageConfig, error) {
	path := fmt.Sprintf("/v2/%s/blobs/%s", imageName, digest)

	var cfg imageConfig
	if err := s.doRegistryJSON(ctx, http.MethodGet, path, nil, &cfg); err != nil {
		return nil, err
	}
	return &cfg, nil
}

// layersTotalSize sums the size of all manifest layers.
func layersTotalSize(layers []manifestLayer) int64 {
	var total int64
	for _, l := range layers {
		total += l.Size
	}
	return total
}

// parseCreated parses a registry config blob's "created" field.
// Tries RFC3339Nano first (Docker's default), falls back to RFC3339.
// Returns zero time if unparseable.
func parseCreated(created string) time.Time {
	if created == "" {
		return time.Time{}
	}
	if t, err := time.Parse(time.RFC3339Nano, created); err == nil {
		return t
	}
	if t, err := time.Parse(time.RFC3339, created); err == nil {
		return t
	}
	return time.Time{}
}
