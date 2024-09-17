package handlers

import (
	"net/http"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/render"

	"tod-p2m/internal/torrent"
)

type cachedTorrentInfo struct {
	Info      torrent.TorrentInfo
	CacheTime time.Time
}

var (
	infoCache     = make(map[string]cachedTorrentInfo)
	infoCacheMu   sync.RWMutex
	infoCacheTTL  = 5 * time.Minute
	infoCacheSize = 1000
)

func GetTorrentInfo(tm *torrent.Manager) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		infoHash := chi.URLParam(r, "infoHash")

		// Check cache first
		infoCacheMu.RLock()
		if cachedInfo, ok := infoCache[infoHash]; ok {
			infoCacheMu.RUnlock()
			render.JSON(w, r, cachedInfo.Info)
			return
		}
		infoCacheMu.RUnlock()

		t, err := tm.GetTorrent(infoHash)
		if err != nil {
			render.Status(r, http.StatusInternalServerError)
			render.JSON(w, r, map[string]string{"error": err.Error()})
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

		// Update cache
		infoCacheMu.Lock()
		if len(infoCache) >= infoCacheSize {
			// Evict oldest entry if cache is full
			var oldestKey string
			oldestTime := time.Now()
			for k, v := range infoCache {
				if v.CacheTime.Before(oldestTime) {
					oldestKey = k
					oldestTime = v.CacheTime
				}
			}
			delete(infoCache, oldestKey)
		}
		infoCache[infoHash] = cachedTorrentInfo{
			Info:      info,
			CacheTime: time.Now(),
		}
		infoCacheMu.Unlock()

		render.JSON(w, r, info)

		// Cleanup old cache entries
		go cleanupInfoCache()
	}
}

func cleanupInfoCache() {
	infoCacheMu.Lock()
	defer infoCacheMu.Unlock()

	now := time.Now()
	for k, v := range infoCache {
		if now.Sub(v.CacheTime) > infoCacheTTL {
			delete(infoCache, k)
		}
	}
}
