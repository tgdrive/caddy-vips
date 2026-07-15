package caddyvips

import (
	"io/fs"
	"os"
	"path/filepath"
	"sort"
)

type cacheEntry struct {
	path    string
	size    int64
	modTime int64
}

func (h *Handler) pruneImageCache(protectedPath string) error {
	if h.MaxCacheBytes <= 0 {
		return nil
	}
	h.cacheMu.Lock()
	defer h.cacheMu.Unlock()

	entries := make([]cacheEntry, 0)
	var total int64
	err := filepath.WalkDir(h.CacheDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			if os.IsNotExist(err) {
				return nil
			}
			return err
		}
		if d.IsDir() {
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		total += info.Size()
		entries = append(entries, cacheEntry{path: path, size: info.Size(), modTime: info.ModTime().UnixNano()})
		return nil
	})
	if err != nil {
		return err
	}
	if total <= h.MaxCacheBytes {
		return nil
	}

	sort.Slice(entries, func(i, j int) bool { return entries[i].modTime < entries[j].modTime })
	for _, entry := range entries {
		if total <= h.MaxCacheBytes {
			break
		}
		if entry.path == protectedPath {
			continue
		}
		if err := os.Remove(entry.path); err != nil && !os.IsNotExist(err) {
			return err
		}
		total -= entry.size
	}
	return nil
}
