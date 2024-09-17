package torrent

import (
	"context"
	"sync"
	"time"
	"tod-p2m/internal/config"
	"tod-p2m/internal/storage"

	"github.com/anacrolix/torrent"
	"github.com/rs/zerolog"
	"golang.org/x/time/rate"
)

// TorrentWrapper extends torrent.Torrent with additional metadata
type TorrentWrapper struct {
	*torrent.Torrent
	infoReady    chan struct{}
	lastAccessed time.Time
	pieceStats   []pieceStats
	mu           sync.RWMutex
}

type pieceStats struct {
	priority    torrent.PiecePriority
	lastRequest time.Time
}

type TorrentInfo struct {
	InfoHash string
	Name     string
	Files    []FileInfo
}

type FileInfo struct {
	ID       int
	Name     string
	Size     int64
	Progress float64
}

const (
	// Límites y tamaños
	maxConcurrentTorrents = 50               // Reducido para evitar sobrecarga
	maxPendingRequests    = 200              // Límite de solicitudes pendientes por torrent
	pieceSelectionWindow  = 50               // Aumentado para mejorar la selección de piezas
	bufferSize            = 256 * 1024       // Aumentado a 256KB para operaciones de archivo más eficientes
	maxMetadataSize       = 10 * 1024 * 1024 // 10MB límite para metadatos de torrent

	// Intervalos de tiempo
	pollInterval    = 100 * time.Millisecond // Aumentado para reducir la carga de CPU
	cleanupInterval = 10 * time.Minute       // Aumentado para reducir la sobrecarga de limpieza
	statsInterval   = 5 * time.Minute        // Aumentado para reducir la frecuencia de actualización de estadísticas
	torrentTimeout  = 5 * time.Minute        // Aumentado para dar más tiempo a los torrents lentos
	metadataTimeout = 1 * time.Minute        // Tiempo de espera para obtener metadatos

	// Reintentos y backoff
	maxRetries         = 5 // Aumentado para mayor resiliencia
	initialRetryDelay  = 1 * time.Second
	maxRetryDelay      = 1 * time.Minute
	retryBackoffFactor = 2.0

	// Límites de velocidad y conexiones
	maxDownloadSpeed = 20 * 1024 * 1024 // 20MB/s
	maxUploadSpeed   = 5 * 1024 * 1024  // 5MB/s
	maxConnections   = 200              // Conexiones máximas por torrent
	maxPeers         = 50               // Número máximo de peers por torrent

	// Cache
	pieceReadAhead = 5                // Número de piezas para leer por adelantado
	readCacheSize  = 64 * 1024 * 1024 // 64MB de caché de lectura
	writeCacheSize = 32 * 1024 * 1024 // 32MB de caché de escritura
)

type Manager struct {
	client       *Client
	cache        *storage.FileStore // Cambiado de fileStore a cache
	config       *config.Config
	Logger       zerolog.Logger
	mu           sync.RWMutex
	torrents     map[string]*TorrentWrapper
	lastAccessed sync.Map
	limiter      *rate.Limiter
	semaphore    chan struct{}
	ctx          context.Context
	cancel       context.CancelFunc
	downloadDir  string
}
