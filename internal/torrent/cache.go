package torrent

import (
	"bytes"
	"container/list"
	"encoding/gob"
	"sync"
	"time"

	"github.com/anacrolix/torrent"
	"github.com/klauspost/compress/zstd"
)

type CacheEntry struct {
	Torrent      *torrent.Torrent
	Info         *TorrentInfo
	LastAccessed time.Time
	Compressed   []byte
}

type Cache struct {
	mu                 sync.RWMutex
	maxSize            int
	compressionEnabled bool
	encoder            *zstd.Encoder
	decoder            *zstd.Decoder
	stats              CacheStats
	shards             []*CacheShard
	cleanupInterval    time.Duration
	expirationTime     time.Duration
}

type CacheShard struct {
	mu       sync.RWMutex
	torrents map[string]*list.Element
	lru      *list.List
}

type CacheStats struct {
	Hits         int64
	Misses       int64
	Evictions    int64
	Size         int
	Compressions int64
}

func NewCache(maxSize int, shardCount int, cleanupInterval, expirationTime time.Duration) *Cache {
	encoder, _ := zstd.NewWriter(nil, zstd.WithEncoderLevel(zstd.SpeedFastest))
	decoder, _ := zstd.NewReader(nil)

	cache := &Cache{
		maxSize:            maxSize,
		compressionEnabled: true,
		encoder:            encoder,
		decoder:            decoder,
		shards:             make([]*CacheShard, shardCount),
		cleanupInterval:    cleanupInterval,
		expirationTime:     expirationTime,
	}

	for i := 0; i < shardCount; i++ {
		cache.shards[i] = &CacheShard{
			torrents: make(map[string]*list.Element),
			lru:      list.New(),
		}
	}

	go cache.cleanupRoutine()

	return cache
}

func (c *Cache) getShard(infoHash string) *CacheShard {
	return c.shards[fnv32(infoHash)%uint32(len(c.shards))]
}

func (c *Cache) Get(infoHash string) (*torrent.Torrent, *TorrentInfo, bool) {
	shard := c.getShard(infoHash)
	shard.mu.RLock()
	defer shard.mu.RUnlock()

	if elem, ok := shard.torrents[infoHash]; ok {
		shard.lru.MoveToFront(elem)
		entry := elem.Value.(*CacheEntry)
		entry.LastAccessed = time.Now()

		c.mu.Lock()
		c.stats.Hits++
		c.mu.Unlock()

		if c.compressionEnabled && entry.Compressed != nil {
			decompressed, err := c.decoder.DecodeAll(entry.Compressed, nil)
			if err != nil {
				return nil, nil, false
			}
			var info TorrentInfo
			err = gob.NewDecoder(bytes.NewReader(decompressed)).Decode(&info)
			if err != nil {
				return nil, nil, false
			}
			entry.Info = &info
		}

		return entry.Torrent, entry.Info, true
	}

	c.mu.Lock()
	c.stats.Misses++
	c.mu.Unlock()

	return nil, nil, false
}

func (c *Cache) Set(infoHash string, t *torrent.Torrent, info *TorrentInfo) {
	shard := c.getShard(infoHash)
	shard.mu.Lock()
	defer shard.mu.Unlock()

	if elem, ok := shard.torrents[infoHash]; ok {
		shard.lru.MoveToFront(elem)
		entry := elem.Value.(*CacheEntry)
		entry.Torrent = t
		entry.Info = info
		entry.LastAccessed = time.Now()
		if c.compressionEnabled {
			var buf bytes.Buffer
			err := gob.NewEncoder(&buf).Encode(info)
			if err == nil {
				entry.Compressed = c.encoder.EncodeAll(buf.Bytes(), nil)
				c.mu.Lock()
				c.stats.Compressions++
				c.mu.Unlock()
			}
		}
	} else {
		if shard.lru.Len() >= c.maxSize/len(c.shards) {
			c.evict(shard)
		}

		entry := &CacheEntry{Torrent: t, Info: info, LastAccessed: time.Now()}
		if c.compressionEnabled {
			var buf bytes.Buffer
			err := gob.NewEncoder(&buf).Encode(info)
			if err == nil {
				entry.Compressed = c.encoder.EncodeAll(buf.Bytes(), nil)
				c.mu.Lock()
				c.stats.Compressions++
				c.mu.Unlock()
			}
		}

		elem := shard.lru.PushFront(entry)
		shard.torrents[infoHash] = elem

		c.mu.Lock()
		c.stats.Size++
		c.mu.Unlock()
	}
}

func (c *Cache) Delete(infoHash string) {
	shard := c.getShard(infoHash)
	shard.mu.Lock()
	defer shard.mu.Unlock()

	if elem, ok := shard.torrents[infoHash]; ok {
		shard.lru.Remove(elem)
		delete(shard.torrents, infoHash)

		c.mu.Lock()
		c.stats.Size--
		c.mu.Unlock()
	}
}

func (c *Cache) evict(shard *CacheShard) {
	if elem := shard.lru.Back(); elem != nil {
		entry := elem.Value.(*CacheEntry)
		shard.lru.Remove(elem)
		delete(shard.torrents, entry.Torrent.InfoHash().String())

		c.mu.Lock()
		c.stats.Evictions++
		c.stats.Size--
		c.mu.Unlock()
	}
}

func (c *Cache) Preload(infoHashes []string) {
	for _, infoHash := range infoHashes {
		if _, _, exists := c.Get(infoHash); !exists {
			c.mu.Lock()
			c.stats.Misses++
			c.mu.Unlock()
		}
	}
}

func (c *Cache) GetStats() CacheStats {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.stats
}

func (c *Cache) ClearStats() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.stats = CacheStats{}
}

func (c *Cache) cleanupRoutine() {
	ticker := time.NewTicker(c.cleanupInterval)
	defer ticker.Stop()

	for range ticker.C {
		c.cleanup()
	}
}

func (c *Cache) cleanup() {
	now := time.Now()
	for _, shard := range c.shards {
		shard.mu.Lock()
		for infoHash, elem := range shard.torrents {
			entry := elem.Value.(*CacheEntry)
			if now.Sub(entry.LastAccessed) > c.expirationTime {
				shard.lru.Remove(elem)
				delete(shard.torrents, infoHash)
				c.mu.Lock()
				c.stats.Evictions++
				c.stats.Size--
				c.mu.Unlock()
			}
		}
		shard.mu.Unlock()
	}
}

func fnv32(key string) uint32 {
	hash := uint32(2166136261)
	const prime32 = uint32(16777619)
	for i := 0; i < len(key); i++ {
		hash *= prime32
		hash ^= uint32(key[i])
	}
	return hash
}
