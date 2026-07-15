package caddyvips

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestPruneImageCacheEvictsLeastRecentlyUsed(t *testing.T) {
	dir := t.TempDir()
	h := &Handler{CacheDir: dir, MaxCacheBytes: 8}

	oldest := filepath.Join(dir, "oldest.webp")
	recent := filepath.Join(dir, "recent.webp")
	newest := filepath.Join(dir, "newest.webp")
	for _, path := range []string{oldest, recent, newest} {
		if err := os.WriteFile(path, []byte("1234"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	now := time.Now()
	if err := os.Chtimes(oldest, now.Add(-3*time.Hour), now.Add(-3*time.Hour)); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(recent, now.Add(-2*time.Hour), now.Add(-2*time.Hour)); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(newest, now.Add(-time.Hour), now.Add(-time.Hour)); err != nil {
		t.Fatal(err)
	}

	if err := h.pruneImageCache(newest); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(oldest); !os.IsNotExist(err) {
		t.Fatalf("oldest entry was not evicted: %v", err)
	}
	for _, path := range []string{recent, newest} {
		if _, err := os.Stat(path); err != nil {
			t.Fatalf("expected %s to remain: %v", path, err)
		}
	}
}

func TestPruneImageCacheProtectsCurrentDerivative(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "large.webp")
	if err := os.WriteFile(path, []byte("larger-than-limit"), 0o600); err != nil {
		t.Fatal(err)
	}
	h := &Handler{CacheDir: dir, MaxCacheBytes: 1}
	if err := h.pruneImageCache(path); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("current derivative was removed: %v", err)
	}
}

func TestReadCachedImageRefreshesLRUTime(t *testing.T) {
	dir := t.TempDir()
	base := filepath.Join(dir, "entry")
	path := base + ".webp"
	if err := os.WriteFile(path, []byte("image"), 0o600); err != nil {
		t.Fatal(err)
	}
	old := time.Now().Add(-time.Hour)
	if err := os.Chtimes(path, old, old); err != nil {
		t.Fatal(err)
	}
	h := &Handler{}
	if _, ok := h.readCachedImage(base); !ok {
		t.Fatal("expected cache hit")
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if !info.ModTime().After(old) {
		t.Fatalf("LRU timestamp was not refreshed: %v", info.ModTime())
	}
}
