package caddyvips

import (
	"net/http/httptest"
	"strings"
	"testing"
)

func TestImageRequestCanonicalizesParameters(t *testing.T) {
	h := &Handler{}
	r := httptest.NewRequest("GET", "http://example.test/photo.jpg?w=200&h=100&fit=cover&q=75&format=webp&dpr=2", nil)

	spec, requested, err := h.imageRequest(r)
	if err != nil {
		t.Fatal(err)
	}
	if !requested {
		t.Fatal("expected image transform request")
	}
	if spec.Width != 400 || spec.Height != 200 || spec.Fit != "cover" || spec.Quality != 75 || spec.Format != "webp" || spec.DPR != 2 {
		t.Fatalf("unexpected spec: %+v", spec)
	}
}

func TestImageRequestRejectsOversizedDimensions(t *testing.T) {
	h := &Handler{MaxDimension: 1000}
	r := httptest.NewRequest("GET", "http://example.test/photo.jpg?w=1001", nil)

	_, requested, err := h.imageRequest(r)
	if !requested || err == nil {
		t.Fatalf("expected rejected image request, requested=%v err=%v", requested, err)
	}
}

func TestSourceRequestWithoutImageQuery(t *testing.T) {
	h := &Handler{}
	r := httptest.NewRequest("GET", "http://example.test/photo.jpg?token=abc&w=200&format=webp&h=100", nil)

	got := h.sourceRequestWithoutImageQuery(r)
	if got.URL.RawQuery != "token=abc" {
		t.Fatalf("unexpected source query: %q", got.URL.RawQuery)
	}
	if r.URL.Query().Get("w") != "200" {
		t.Fatal("original request was mutated")
	}
}

func TestImageCachePathDependsOnTransform(t *testing.T) {
	h := &Handler{CacheDir: t.TempDir()}
	first := h.imageCachePath("source", "etag-a", imageSpec{Width: 100, Height: 100, Fit: "cover", Gravity: "center", Quality: 80, Format: "webp", DPR: 1, WithoutEnlargement: true, Background: "ffffff"})
	second := h.imageCachePath("source", "etag-a", imageSpec{Width: 200, Height: 100, Fit: "cover", Gravity: "center", Quality: 80, Format: "webp", DPR: 1, WithoutEnlargement: true, Background: "ffffff"})
	if first == second {
		t.Fatal("different transforms produced the same cache path")
	}
	if !strings.Contains(first, h.CacheDir) {
		t.Fatalf("cache path does not use image cache directory: %q", first)
	}
}
