// internal/api/handlers/torrent.go
package handlers

import (
	"context"
	"net/http"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/render"
	"github.com/rs/zerolog"
	"golang.org/x/sync/singleflight"

	"tod-p2m/internal/torrent"
)

type cachedTorrentInfo struct {
	Info      torrent.TorrentInfo
	CacheTime time.Time
}

type TorrentInfoCache struct {
	cache   map[string]cachedTorrentInfo
	mu      sync.RWMutex
	ttl     time.Duration
	maxSize int
	sfGroup singleflight.Group
}

func NewTorrentInfoCache(ttl time.Duration, maxSize int) *TorrentInfoCache {
	c := &TorrentInfoCache{
		cache:   make(map[string]cachedTorrentInfo),
		ttl:     ttl,
		maxSize: maxSize,
	}
	go c.cleanupRoutine()
	return c
}

func (c *TorrentInfoCache) Get(infoHash string) (torrent.TorrentInfo, bool) {
	c.mu.RLock()
	cachedInfo, ok := c.cache[infoHash]
	c.mu.RUnlock()
	if ok && time.Since(cachedInfo.CacheTime) < c.ttl {
		return cachedInfo.Info, true
	}
	return torrent.TorrentInfo{}, false
}

func (c *TorrentInfoCache) Set(infoHash string, info torrent.TorrentInfo) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(c.cache) >= c.maxSize {
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
	ticker := time.NewTicker(c.ttl)
	defer ticker.Stop()

	for range ticker.C {
		c.mu.Lock()
		now := time.Now()
		for k, v := range c.cache {
			if now.Sub(v.CacheTime) > c.ttl {
				delete(c.cache, k)
			}
		}
		c.mu.Unlock()
	}
}

func GetTorrentInfo(tm *torrent.Manager, cache *TorrentInfoCache, log zerolog.Logger) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		infoHash := chi.URLParam(r, "infoHash")
		if infoHash == "" {
			http.Error(w, "Missing infoHash parameter", http.StatusBadRequest)
			return
		}

		info, err, _ := cache.sfGroup.Do(infoHash, func() (interface{}, error) {
			if cachedInfo, ok := cache.Get(infoHash); ok {
				return cachedInfo, nil
			}

			t, err := tm.GetTorrent(infoHash)
			if err != nil {
				return nil, err
			}

			ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
			defer cancel()

			select {
			case <-t.GotInfo():
			case <-ctx.Done():
				return nil, ctx.Err()
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
			return info, nil
		})

		if err != nil {
			log.Error().Err(err).Str("infoHash", infoHash).Msg("Failed to get torrent info")
			statusCode := http.StatusInternalServerError
			if err == context.DeadlineExceeded {
				statusCode = http.StatusRequestTimeout
			}
			http.Error(w, http.StatusText(statusCode), statusCode)
			return
		}

		render.JSON(w, r, info)
	}
}
