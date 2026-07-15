package caddyvips

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/caddyserver/caddy/v2/caddyconfig/caddyfile"
	"github.com/caddyserver/caddy/v2/caddyconfig/httpcaddyfile"
	"github.com/caddyserver/caddy/v2/modules/caddyhttp"
)

func init() { httpcaddyfile.RegisterHandlerDirective("vips", parseCaddyfile) }

func parseCaddyfile(h httpcaddyfile.Helper) (caddyhttp.MiddlewareHandler, error) {
	var m Handler
	if err := m.UnmarshalCaddyfile(h.Dispenser); err != nil {
		return nil, err
	}
	return &m, nil
}

func (h *Handler) UnmarshalCaddyfile(d *caddyfile.Dispenser) error {
	for d.Next() {
		if d.NextArg() {
			return d.ArgErr()
		}
		for nesting := d.Nesting(); d.NextBlock(nesting); {
			switch d.Val() {
			case "cache_dir":
				if !d.NextArg() {
					return d.ArgErr()
				}
				h.CacheDir = d.Val()
			case "quality":
				v, err := nextInt(d)
				if err != nil {
					return err
				}
				h.Quality = v
			case "max_dimension":
				v, err := nextInt(d)
				if err != nil {
					return err
				}
				h.MaxDimension = v
			case "max_pixels":
				v, err := nextInt64(d)
				if err != nil {
					return err
				}
				h.MaxPixels = v
			case "max_source_size":
				if !d.NextArg() {
					return d.ArgErr()
				}
				v, err := parseBytes(d.Val())
				if err != nil {
					return err
				}
				h.MaxSourceBytes = v
			case "cache_max_size", "max_cache_size":
				if !d.NextArg() {
					return d.ArgErr()
				}
				v, err := parseBytes(d.Val())
				if err != nil {
					return err
				}
				h.MaxCacheBytes = v
			case "debug_headers":
				if !d.NextArg() {
					return d.ArgErr()
				}
				v, err := strconv.ParseBool(d.Val())
				if err != nil {
					return err
				}
				h.DebugHeaders = v
			case "enable_logs":
				if !d.NextArg() {
					return d.ArgErr()
				}
				v, err := strconv.ParseBool(d.Val())
				if err != nil {
					return err
				}
				h.EnableLogs = v
			default:
				return d.Errf("unknown vips option %q", d.Val())
			}
		}
	}
	return nil
}

func nextInt(d *caddyfile.Dispenser) (int, error) {
	if !d.NextArg() {
		return 0, d.ArgErr()
	}
	return strconv.Atoi(d.Val())
}

func nextInt64(d *caddyfile.Dispenser) (int64, error) {
	if !d.NextArg() {
		return 0, d.ArgErr()
	}
	return strconv.ParseInt(d.Val(), 10, 64)
}

func parseBytes(s string) (int64, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0, fmt.Errorf("empty size")
	}
	lower := strings.ToLower(s)
	mult := int64(1)
	for _, suffix := range []struct {
		name string
		mul  int64
	}{
		{"kib", 1024}, {"kb", 1000}, {"k", 1024},
		{"mib", 1024 * 1024}, {"mb", 1000 * 1000}, {"m", 1024 * 1024},
		{"gib", 1024 * 1024 * 1024}, {"gb", 1000 * 1000 * 1000}, {"g", 1024 * 1024 * 1024},
	} {
		if strings.HasSuffix(lower, suffix.name) {
			mult = suffix.mul
			s = strings.TrimSpace(s[:len(s)-len(suffix.name)])
			break
		}
	}
	v, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return 0, err
	}
	if v < 0 {
		return 0, fmt.Errorf("negative size")
	}
	return int64(v * float64(mult)), nil
}
