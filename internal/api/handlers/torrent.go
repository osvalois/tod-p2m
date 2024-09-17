package handlers

import (
	"context"
	"net/http"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/render"

	"tod-p2m/internal/torrent"
)

const (
	infoCacheTTL    = 5 * time.Minute
	infoCacheSize   = 1000
	cleanupInterval = 10 * time.Minute
)

type cachedTorrentInfo struct {
	Info      torrent.TorrentInfo
	CacheTime time.Time
}

type TorrentInfoCache struct {
	cache map[string]cachedTorrentInfo
	mu    sync.RWMutex
}

func NewTorrentInfoCache() *TorrentInfoCache {
	c := &TorrentInfoCache{
		cache: make(map[string]cachedTorrentInfo),
	}
	go c.cleanupRoutine()
	return c
}

func (c *TorrentInfoCache) Get(infoHash string) (torrent.TorrentInfo, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	if cachedInfo, ok := c.cache[infoHash]; ok && time.Since(cachedInfo.CacheTime) < infoCacheTTL {
		return cachedInfo.Info, true
	}
	return torrent.TorrentInfo{}, false
}

func (c *TorrentInfoCache) Set(infoHash string, info torrent.TorrentInfo) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(c.cache) >= infoCacheSize {
		c.evictOldest()
	}
	c.cache[infoHash] = cachedTorrentInfo{
		Info:      info,
		CacheTime: time.Now(),
	}
}

func (c *TorrentInfoCache) evictOldest() {
	var oldestKey string
	var oldestTime time.Time
	for k, v := range c.cache {
		if oldestTime.IsZero() || v.CacheTime.Before(oldestTime) {
			oldestKey = k
			oldestTime = v.CacheTime
		}
	}
	delete(c.cache, oldestKey)
}

func (c *TorrentInfoCache) cleanupRoutine() {
	ticker := time.NewTicker(cleanupInterval)
	defer ticker.Stop()

	for range ticker.C {
		c.mu.Lock()
		for k, v := range c.cache {
			if time.Since(v.CacheTime) > infoCacheTTL {
				delete(c.cache, k)
			}
		}
		c.mu.Unlock()
	}
}

func GetTorrentInfo(tm *torrent.Manager, cache *TorrentInfoCache) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		infoHash := chi.URLParam(r, "infoHash")

		if cachedInfo, ok := cache.Get(infoHash); ok {
			render.JSON(w, r, cachedInfo)
			return
		}

		t, err := tm.GetTorrent(infoHash)
		if err != nil {
			render.Status(r, http.StatusInternalServerError)
			render.JSON(w, r, map[string]string{"error": err.Error()})
			return
		}

		// Use a context with timeout for info retrieval
		ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
		defer cancel()

		// Wait for info with a timeout
		select {
		case <-t.GotInfo():
			// Continue with processing
		case <-ctx.Done():
			render.Status(r, http.StatusGatewayTimeout)
			render.JSON(w, r, map[string]string{"error": "Timeout waiting for torrent info"})
			return
		}

		info := torrent.TorrentInfo{
			InfoHash: t.InfoHash().String(),
			Name:     t.Name(),
			Files:    make([]torrent.FileInfo, 0, len(t.Files())),
		}

		for i, f := range t.Files() {
			info.Files = append(info.Files, torrent.FileInfo{
				ID:       i,
				Name:     f.DisplayPath(),
				Size:     f.Length(),
				Progress: float64(f.BytesCompleted()) / float64(f.Length()),
			})
		}

		cache.Set(infoHash, info)
		render.JSON(w, r, info)
	}
}
