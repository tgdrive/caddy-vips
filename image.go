package caddyvips

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/caddyserver/caddy/v2/modules/caddyhttp"
)

const (
	imagePipelineVersion       = "v2"
	defaultImageQuality        = 82
	defaultImageMaxDimension   = 8192
	defaultImageMaxPixels      = 40_000_000
	defaultImageMaxSourceBytes = 64 << 20
	defaultImageCacheControl   = "public, max-age=86400, stale-while-revalidate=604800"
)

var imageQueryKeys = []string{
	"w", "width", "h", "height", "fit", "gravity", "q", "quality", "format", "f", "dpr",
	"rotate", "flip", "without_enlargement", "background",
}

type imageProcessor interface {
	Transform(source []byte, spec imageSpec) ([]byte, string, error)
}

type imageSpec struct {
	Width              int
	Height             int
	Fit                string
	Gravity            string
	Quality            int
	Format             string
	DPR                float64
	Rotate             int
	Flip               string
	WithoutEnlargement bool
	Background         string
	Negotiated         bool
}

func (s imageSpec) canonical() string {
	return fmt.Sprintf(
		"pipeline=%s&w=%d&h=%d&fit=%s&gravity=%s&q=%d&format=%s&dpr=%g&rotate=%d&flip=%s&without_enlargement=%t&background=%s",
		imagePipelineVersion, s.Width, s.Height, s.Fit, s.Gravity, s.Quality, s.Format, s.DPR, s.Rotate, s.Flip, s.WithoutEnlargement, s.Background,
	)
}

func (h *Handler) imageRequest(r *http.Request) (imageSpec, bool, error) {
	q := r.URL.Query()
	requested := slices.ContainsFunc(imageQueryKeys, q.Has)
	if !requested {
		return imageSpec{}, false, nil
	}

	parseAlias := func(names ...string) (string, error) {
		var value string
		var seen string
		for _, name := range names {
			values, ok := q[name]
			if !ok {
				continue
			}
			if len(values) != 1 || values[0] == "" {
				return "", fmt.Errorf("varc: image parameter %s must occur exactly once", name)
			}
			if seen != "" {
				return "", fmt.Errorf("varc: conflicting image parameters %s and %s", seen, name)
			}
			seen, value = name, values[0]
		}
		return value, nil
	}
	parseInt := func(names ...string) (int, error) {
		raw, err := parseAlias(names...)
		if err != nil || raw == "" {
			return 0, err
		}
		v, parseErr := strconv.Atoi(raw)
		if parseErr != nil || v < 0 {
			return 0, fmt.Errorf("varc: invalid image parameter %s=%q", names[0], raw)
		}
		return v, nil
	}

	width, err := parseInt("w", "width")
	if err != nil {
		return imageSpec{}, true, err
	}
	height, err := parseInt("h", "height")
	if err != nil {
		return imageSpec{}, true, err
	}
	if width == 0 && height == 0 {
		return imageSpec{}, true, fmt.Errorf("varc: image transform requires w or h")
	}

	dpr := 1.0
	if raw, parseErr := parseAlias("dpr"); parseErr != nil {
		return imageSpec{}, true, parseErr
	} else if raw != "" {
		dpr, err = strconv.ParseFloat(raw, 64)
		if err != nil || dpr <= 0 || dpr > 4 {
			return imageSpec{}, true, fmt.Errorf("varc: invalid image parameter dpr=%q", raw)
		}
	}
	width = int(float64(width)*dpr + 0.5)
	height = int(float64(height)*dpr + 0.5)
	maxDimension := h.MaxDimension
	if maxDimension <= 0 {
		maxDimension = defaultImageMaxDimension
	}
	if width > maxDimension || height > maxDimension {
		return imageSpec{}, true, fmt.Errorf("varc: requested image dimensions exceed %d", maxDimension)
	}
	maxPixels := h.MaxPixels
	if maxPixels <= 0 {
		maxPixels = defaultImageMaxPixels
	}
	if width > 0 && height > 0 && int64(width)*int64(height) > maxPixels {
		return imageSpec{}, true, fmt.Errorf("varc: requested image exceeds pixel limit")
	}

	fit, err := parseAlias("fit")
	if err != nil {
		return imageSpec{}, true, err
	}
	fit = strings.ToLower(fit)
	if fit == "" {
		fit = "contain"
	}
	switch fit {
	case "contain", "cover", "fill", "inside", "outside":
	default:
		return imageSpec{}, true, fmt.Errorf("varc: unsupported image fit %q", fit)
	}

	gravity, err := parseAlias("gravity")
	if err != nil {
		return imageSpec{}, true, err
	}
	gravity = strings.ToLower(gravity)
	if gravity == "" {
		gravity = "center"
	}
	switch gravity {
	case "center", "attention", "entropy", "top", "bottom", "left", "right":
	default:
		return imageSpec{}, true, fmt.Errorf("varc: unsupported image gravity %q", gravity)
	}

	quality := h.Quality
	if quality == 0 {
		quality = defaultImageQuality
	}
	if raw, parseErr := parseAlias("q", "quality"); parseErr != nil {
		return imageSpec{}, true, parseErr
	} else if raw != "" {
		quality, err = strconv.Atoi(raw)
		if err != nil || quality < 1 || quality > 100 {
			return imageSpec{}, true, fmt.Errorf("varc: invalid image quality %q", raw)
		}
	}

	format, err := parseAlias("format", "f")
	if err != nil {
		return imageSpec{}, true, err
	}
	format = strings.ToLower(format)
	negotiated := false
	if format == "" || format == "auto" {
		format = negotiateImageFormat(r.Header.Get("Accept"))
		negotiated = true
	}
	if format == "jpg" {
		format = "jpeg"
	}
	switch format {
	case "jpeg", "png", "webp":
	default:
		return imageSpec{}, true, fmt.Errorf("varc: unsupported image format %q", format)
	}

	rotate, err := parseInt("rotate")
	if err != nil {
		return imageSpec{}, true, err
	}
	switch rotate {
	case 0, 90, 180, 270:
	default:
		return imageSpec{}, true, fmt.Errorf("varc: rotate must be 0, 90, 180, or 270")
	}
	flip, err := parseAlias("flip")
	if err != nil {
		return imageSpec{}, true, err
	}
	flip = strings.ToLower(flip)
	switch flip {
	case "", "horizontal", "vertical":
	default:
		return imageSpec{}, true, fmt.Errorf("varc: unsupported image flip %q", flip)
	}
	withoutEnlargement := true
	if raw, parseErr := parseAlias("without_enlargement"); parseErr != nil {
		return imageSpec{}, true, parseErr
	} else if raw != "" {
		withoutEnlargement, err = strconv.ParseBool(raw)
		if err != nil {
			return imageSpec{}, true, fmt.Errorf("varc: invalid without_enlargement=%q", raw)
		}
	}
	background, err := parseAlias("background")
	if err != nil {
		return imageSpec{}, true, err
	}
	background = strings.TrimPrefix(strings.ToLower(background), "#")
	if background == "" {
		background = "ffffff"
	}
	if len(background) != 6 {
		return imageSpec{}, true, fmt.Errorf("varc: background must be a 6-digit hex color")
	}
	if _, err := hex.DecodeString(background); err != nil {
		return imageSpec{}, true, fmt.Errorf("varc: invalid background color %q", background)
	}

	return imageSpec{
		Width: width, Height: height, Fit: fit, Gravity: gravity, Quality: quality, Format: format,
		DPR: dpr, Rotate: rotate, Flip: flip, WithoutEnlargement: withoutEnlargement,
		Background: background, Negotiated: negotiated,
	}, true, nil
}

func negotiateImageFormat(accept string) string {
	accept = strings.ToLower(accept)
	if strings.Contains(accept, "image/webp") {
		return "webp"
	}
	if strings.Contains(accept, "image/png") {
		return "png"
	}
	return "jpeg"
}

func (h *Handler) serveImage(w http.ResponseWriter, r *http.Request, next caddyhttp.Handler, spec imageSpec) error {
	sourceRequest := h.sourceRequestWithoutImageQuery(r)
	cacheBase := h.imageCachePath(sourceRequest.URL.RequestURI(), spec)
	if result, ok := h.readCachedImage(cacheBase); ok {
		return h.writeImage(w, r, result, "HIT", spec.Negotiated)
	}

	value, err, shared := h.flights.Do("image:"+cacheBase, func() (any, error) {
		if result, ok := h.readCachedImage(cacheBase); ok {
			return result, nil
		}

		recorder := httptest.NewRecorder()
		if err := next.ServeHTTP(recorder, sourceRequest); err != nil {
			return nil, err
		}
		response := recorder.Result()
		defer response.Body.Close()
		if response.StatusCode < 200 || response.StatusCode >= 300 {
			return nil, &sourceResponseError{response: response}
		}

		limit := h.MaxSourceBytes
		if limit <= 0 {
			limit = defaultImageMaxSourceBytes
		}
		if response.ContentLength > limit {
			return nil, caddyhttp.Error(http.StatusRequestEntityTooLarge, fmt.Errorf("caddy-vips: source size %d exceeds limit %d", response.ContentLength, limit))
		}
		source, readErr := io.ReadAll(io.LimitReader(response.Body, limit+1))
		if readErr != nil {
			return nil, caddyhttp.Error(http.StatusBadGateway, fmt.Errorf("caddy-vips: read source: %w", readErr))
		}
		if int64(len(source)) > limit {
			return nil, caddyhttp.Error(http.StatusRequestEntityTooLarge, fmt.Errorf("caddy-vips: source exceeds limit %d", limit))
		}

		output, contentType, transformErr := h.imageProcessor.Transform(source, spec)
		if transformErr != nil {
			return nil, transformErr
		}
		path := replaceImageExtension(cacheBase, contentType)
		if writeErr := writeFileAtomic(path, output); writeErr != nil {
			return nil, fmt.Errorf("caddy-vips: cache write: %w", writeErr)
		}
		if pruneErr := h.pruneImageCache(path); pruneErr != nil {
			return nil, fmt.Errorf("caddy-vips: cache prune: %w", pruneErr)
		}
		info, statErr := os.Stat(path)
		if statErr != nil {
			return nil, statErr
		}
		return cachedImage{Path: path, ContentType: contentType, Size: info.Size(), ModTime: info.ModTime(), ETag: imageETag(cacheBase)}, nil
	})
	if err != nil {
		var sourceErr *sourceResponseError
		if errors.As(err, &sourceErr) {
			copyHeader(w.Header(), sourceErr.response.Header)
			w.WriteHeader(sourceErr.response.StatusCode)
			_, copyErr := io.Copy(w, sourceErr.response.Body)
			return copyErr
		}
		return caddyhttp.Error(http.StatusBadGateway, err)
	}
	result := value.(cachedImage)
	status := "MISS"
	if shared {
		status = "HIT"
	}
	return h.writeImage(w, r, result, status, spec.Negotiated)
}

type sourceResponseError struct {
	response *http.Response
}

func (e *sourceResponseError) Error() string {
	return fmt.Sprintf("caddy-vips: source returned %s", e.response.Status)
}

type cachedImage struct {
	Path        string
	ContentType string
	Size        int64
	ModTime     time.Time
	ETag        string
}

func (h *Handler) writeImage(w http.ResponseWriter, r *http.Request, image cachedImage, cacheStatus string, negotiated bool) error {
	w.Header().Set("Content-Type", image.ContentType)
	w.Header().Set("Content-Length", strconv.FormatInt(image.Size, 10))
	w.Header().Set("Cache-Control", defaultImageCacheControl)
	w.Header().Set("ETag", image.ETag)
	w.Header().Set("Last-Modified", image.ModTime.UTC().Format(http.TimeFormat))
	if negotiated {
		w.Header().Set("Vary", "Accept")
	}
	if h.DebugHeaders {
		w.Header().Set("X-Caddy-Vips-Cache", cacheStatus)
	}
	if r.Header.Get("If-None-Match") == image.ETag {
		w.WriteHeader(http.StatusNotModified)
		return nil
	}
	if modifiedSince := r.Header.Get("If-Modified-Since"); modifiedSince != "" {
		if t, err := http.ParseTime(modifiedSince); err == nil && !image.ModTime.After(t.Add(time.Second)) {
			w.WriteHeader(http.StatusNotModified)
			return nil
		}
	}
	file, err := os.Open(image.Path)
	if err != nil {
		return caddyhttp.Error(http.StatusInternalServerError, err)
	}
	defer file.Close()
	w.WriteHeader(http.StatusOK)
	if r.Method == http.MethodHead {
		return nil
	}
	_, err = io.Copy(w, file)
	return err
}

func (h *Handler) sourceRequestWithoutImageQuery(r *http.Request) *http.Request {
	clone := r.Clone(r.Context())
	clone.URL = cloneURL(r.URL)
	q := clone.URL.Query()
	for _, key := range imageQueryKeys {
		q.Del(key)
	}
	clone.URL.RawQuery = encodeSortedQuery(q)
	return clone
}

func (h *Handler) imageCachePath(sourceKey string, spec imageSpec) string {
	dir := h.CacheDir
	if dir == "" {
		dir = defaultCacheDir
	}
	sum := sha256.Sum256([]byte(sourceKey + "\x00" + spec.canonical()))
	hexsum := hex.EncodeToString(sum[:])
	return filepath.Join(dir, hexsum[:2], hexsum[2:4], hexsum)
}

func imageETag(cacheBase string) string {
	sum := sha256.Sum256([]byte(cacheBase))
	return `"` + hex.EncodeToString(sum[:]) + `"`
}

func replaceImageExtension(path, contentType string) string {
	ext := ".img"
	switch contentType {
	case "image/jpeg":
		ext = ".jpg"
	case "image/png":
		ext = ".png"
	case "image/webp":
		ext = ".webp"
	}
	return path + ext
}

func (h *Handler) readCachedImage(base string) (cachedImage, bool) {
	for _, ext := range []string{".jpg", ".png", ".webp"} {
		path := base + ext
		info, err := os.Stat(path)
		if err == nil && info.Mode().IsRegular() {
			now := time.Now()
			_ = os.Chtimes(path, now, now)
			return cachedImage{Path: path, ContentType: contentTypeFromExtension(path), Size: info.Size(), ModTime: info.ModTime(), ETag: imageETag(base)}, true
		}
	}
	return cachedImage{}, false
}

func contentTypeFromExtension(path string) string {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".jpg", ".jpeg":
		return "image/jpeg"
	case ".png":
		return "image/png"
	case ".webp":
		return "image/webp"
	default:
		return "application/octet-stream"
	}
}

func writeFileAtomic(path string, data []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".varc-image-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)
	if _, err = tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err = tmp.Sync(); err != nil {
		tmp.Close()
		return err
	}
	if err = tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmpName, path)
}

func cloneURL(u *url.URL) *url.URL {
	copy := *u
	return &copy
}

func encodeSortedQuery(q url.Values) string {
	keys := make([]string, 0, len(q))
	for key := range q {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	var parts []string
	for _, key := range keys {
		values := append([]string(nil), q[key]...)
		sort.Strings(values)
		for _, value := range values {
			parts = append(parts, url.QueryEscape(key)+"="+url.QueryEscape(value))
		}
	}
	return strings.Join(parts, "&")
}
