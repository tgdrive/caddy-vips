// Package caddyvips provides a Caddy HTTP middleware for libvips-backed image transformations.
package caddyvips

import (
	"fmt"
	"net/http"

	"github.com/caddyserver/caddy/v2"
	"github.com/caddyserver/caddy/v2/modules/caddyhttp"
	"golang.org/x/sync/singleflight"
)

const defaultCacheDir = "/var/cache/caddy/vips"

func init() { caddy.RegisterModule(new(Handler)) }

type Handler struct {
	CacheDir       string `json:"cache_dir,omitempty"`
	Quality        int    `json:"quality,omitempty"`
	MaxDimension   int    `json:"max_dimension,omitempty"`
	MaxPixels      int64  `json:"max_pixels,omitempty"`
	MaxSourceBytes int64  `json:"max_source_bytes,omitempty"`
	DebugHeaders   bool   `json:"debug_headers,omitempty"`

	imageProcessor imageProcessor
	flights        singleflight.Group
}

func (Handler) CaddyModule() caddy.ModuleInfo {
	return caddy.ModuleInfo{ID: "http.handlers.vips", New: func() caddy.Module { return new(Handler) }}
}

func (h *Handler) Provision(caddy.Context) error {
	if h.CacheDir == "" {
		h.CacheDir = defaultCacheDir
	}
	h.imageProcessor = newImageProcessor()
	return nil
}

func (h *Handler) Validate() error {
	if h.Quality < 0 || h.Quality > 100 {
		return fmt.Errorf("caddy-vips: quality must be between 1 and 100")
	}
	if h.MaxDimension < 0 || h.MaxPixels < 0 || h.MaxSourceBytes < 0 {
		return fmt.Errorf("caddy-vips: limits cannot be negative")
	}
	return nil
}

func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request, next caddyhttp.Handler) error {
	spec, requested, err := h.imageRequest(r)
	if err != nil {
		return caddyhttp.Error(http.StatusBadRequest, err)
	}
	if !requested {
		return next.ServeHTTP(w, r)
	}
	return h.serveImage(w, r, next, spec)
}

func copyHeader(dst, src http.Header) {
	for key, values := range src {
		for _, value := range values {
			dst.Add(key, value)
		}
	}
}

var (
	_ caddy.Provisioner           = (*Handler)(nil)
	_ caddy.Validator             = (*Handler)(nil)
	_ caddyhttp.MiddlewareHandler = (*Handler)(nil)
)
