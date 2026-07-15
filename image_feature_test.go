package caddyvips

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestImageRequestNegotiatesWebP(t *testing.T) {
	h := &Handler{}
	r := httptest.NewRequest(http.MethodGet, "http://example.test/photo.jpg?w=320", nil)
	r.Header.Set("Accept", "image/avif,image/webp,image/*;q=0.8")

	spec, requested, err := h.imageRequest(r)
	if err != nil {
		t.Fatal(err)
	}
	if !requested || spec.Format != "webp" || !spec.Negotiated {
		t.Fatalf("unexpected negotiated spec: %+v", spec)
	}
}

func TestImageRequestRejectsConflictingAliases(t *testing.T) {
	h := &Handler{}
	r := httptest.NewRequest(http.MethodGet, "http://example.test/photo.jpg?w=320&width=640", nil)

	_, requested, err := h.imageRequest(r)
	if !requested || err == nil {
		t.Fatalf("expected conflicting aliases to fail, requested=%v err=%v", requested, err)
	}
}

func TestImageCachePathDependsOnSourceFingerprint(t *testing.T) {
	h := &Handler{CacheDir: t.TempDir()}
	spec := imageSpec{Width: 320, Height: 180, Fit: "cover", Gravity: "center", Quality: 82, Format: "webp", DPR: 1, WithoutEnlargement: true, Background: "ffffff"}

	first := h.imageCachePath("source", "etag-a", spec)
	second := h.imageCachePath("source", "etag-b", spec)
	if first == second {
		t.Fatal("changed source fingerprint reused derivative cache path")
	}
}

func TestWriteImageConditionalETag(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "image.webp")
	if err := os.WriteFile(path, []byte("image"), 0o600); err != nil {
		t.Fatal(err)
	}
	modTime := time.Now().Add(-time.Minute).Truncate(time.Second)
	if err := os.Chtimes(path, modTime, modTime); err != nil {
		t.Fatal(err)
	}

	h := &Handler{}
	r := httptest.NewRequest(http.MethodGet, "http://example.test/image.webp", nil)
	r.Header.Set("If-None-Match", `"etag"`)
	w := httptest.NewRecorder()

	err := h.writeImage(w, r, cachedImage{Path: path, ContentType: "image/webp", Size: 5, ModTime: modTime, ETag: `"etag"`}, "HIT", true)
	if err != nil {
		t.Fatal(err)
	}
	if w.Code != http.StatusNotModified {
		t.Fatalf("expected 304, got %d", w.Code)
	}
	if w.Header().Get("Vary") != "Accept" {
		t.Fatalf("expected Vary: Accept, got %q", w.Header().Get("Vary"))
	}
}
