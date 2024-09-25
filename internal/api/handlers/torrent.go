package handlers

import (
	"context"
	"net/http"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/render"
	"github.com/rs/zerolog"
	"golang.org/x/sync/errgroup"
	"golang.org/x/sync/singleflight"

	"tod-p2m/internal/torrent"
)

type cachedTorrentInfo struct {
	Info      torrent.TorrentInfo
	CacheTime time.Time
}

type TorrentInfoCache struct {
	cache   sync.Map
	ttl     time.Duration
	maxSize int
	sfGroup singleflight.Group
}

func NewTorrentInfoCache(ttl time.Duration, maxSize int) *TorrentInfoCache {
	c := &TorrentInfoCache{
		ttl:     ttl,
		maxSize: maxSize,
	}
	go c.cleanupRoutine()
	return c
}

func (c *TorrentInfoCache) Get(infoHash string) (torrent.TorrentInfo, bool) {
	if value, ok := c.cache.Load(infoHash); ok {
		cachedInfo := value.(cachedTorrentInfo)
		if time.Since(cachedInfo.CacheTime) < c.ttl {
			return cachedInfo.Info, true
		}
	}
	return torrent.TorrentInfo{}, false
}

func (c *TorrentInfoCache) Set(infoHash string, info torrent.TorrentInfo) {
	c.cache.Store(infoHash, cachedTorrentInfo{
		Info:      info,
		CacheTime: time.Now(),
	})
	c.evictIfNeeded()
}

func (c *TorrentInfoCache) evictIfNeeded() {
	for {
		count := 0
		c.cache.Range(func(_, _ interface{}) bool {
			count++
			return true
		})
		if count <= c.maxSize {
			return
		}
		var oldestKey interface{}
		var oldestTime time.Time
		c.cache.Range(func(key, value interface{}) bool {
			info := value.(cachedTorrentInfo)
			if oldestTime.IsZero() || info.CacheTime.Before(oldestTime) {
				oldestKey = key
				oldestTime = info.CacheTime
			}
			return true
		})
		if oldestKey != nil {
			c.cache.Delete(oldestKey)
		}
	}
}

func (c *TorrentInfoCache) cleanupRoutine() {
	ticker := time.NewTicker(c.ttl)
	defer ticker.Stop()

	for range ticker.C {
		now := time.Now()
		c.cache.Range(func(key, value interface{}) bool {
			info := value.(cachedTorrentInfo)
			if now.Sub(info.CacheTime) > c.ttl {
				c.cache.Delete(key)
			}
			return true
		})
	}
}

func GetTorrentInfo(tm *torrent.Manager, cache *TorrentInfoCache, log zerolog.Logger) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		infoHash := chi.URLParam(r, "infoHash")
		if infoHash == "" {
			http.Error(w, "Missing infoHash parameter", http.StatusBadRequest)
			return
		}

		ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
		defer cancel()

		info, err, _ := cache.sfGroup.Do(infoHash, func() (interface{}, error) {
			if cachedInfo, ok := cache.Get(infoHash); ok {
				return cachedInfo, nil
			}

			t, err := tm.GetTorrent(infoHash)
			if err != nil {
				return nil, err
			}

			basicInfo := torrent.TorrentInfo{
				InfoHash: t.InfoHash().String(),
				Name:     t.Name(),
				Files:    []torrent.FileInfo{},
				Seeders:  -1,
				Leechers: -1,
			}

			eg, ctx := errgroup.WithContext(ctx)

			eg.Go(func() error {
				select {
				case <-t.GotInfo():
					completeInfo := torrent.TorrentInfo{
						InfoHash: t.InfoHash().String(),
						Name:     t.Name(),
						Files:    make([]torrent.FileInfo, 0, len(t.Files())),
						Seeders:  t.Stats().ConnectedSeeders,
						Leechers: t.Stats().ActivePeers - t.Stats().ConnectedSeeders,
					}

					for i, f := range t.Files() {
						completeInfo.Files = append(completeInfo.Files, torrent.FileInfo{
							ID:       i,
							Name:     f.DisplayPath(),
							Size:     f.Length(),
							Progress: float64(f.BytesCompleted()) / float64(f.Length()),
						})
					}

					cache.Set(infoHash, completeInfo)
					return nil
				case <-ctx.Done():
					return ctx.Err()
				}
			})

			if err := eg.Wait(); err != nil && err != context.DeadlineExceeded {
				log.Warn().Err(err).Str("infoHash", infoHash).Msg("Error while waiting for complete torrent info")
			}

			finalInfo, _ := cache.Get(infoHash)
			if finalInfo.Seeders == -1 {
				return basicInfo, nil
			}
			return finalInfo, nil
		})

		if err != nil {
			log.Error().Err(err).Str("infoHash", infoHash).Msg("Failed to get torrent info")
			http.Error(w, "Internal Server Error", http.StatusInternalServerError)
			return
		}

		render.JSON(w, r, info)
	}
}
