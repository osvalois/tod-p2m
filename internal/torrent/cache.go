package torrent

import (
	"sync"

	"github.com/anacrolix/torrent"
)

type Cache struct {
	mu       sync.RWMutex
	torrents map[string]*torrent.Torrent
}

func NewCache() *Cache {
	return &Cache{
		torrents: make(map[string]*torrent.Torrent),
	}
}

func (c *Cache) Get(infoHash string) (*torrent.Torrent, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	t, ok := c.torrents[infoHash]
	return t, ok
}

func (c *Cache) Set(infoHash string, t *torrent.Torrent) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.torrents[infoHash] = t
}

func (c *Cache) Delete(infoHash string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.torrents, infoHash)
}
