package adaptive

// internal/internal/streaming/adaptive/bitrate.go
import (
	"math"
	"sync"
	"time"
)

type BitrateAdapter struct {
	minBitrate     int
	maxBitrate     int
	bufferSize     time.Duration
	currentBitrate int
	networkSpeed   float64
	speedSamples   []float64
	maxSamples     int
	mu             sync.Mutex
}

func NewBitrateAdapter(minBitrate, maxBitrate int, bufferSize time.Duration) *BitrateAdapter {
	return &BitrateAdapter{
		minBitrate:     minBitrate,
		maxBitrate:     maxBitrate,
		bufferSize:     bufferSize,
		currentBitrate: minBitrate,
	}
}

func (ba *BitrateAdapter) AdaptBitrate(downloadSpeed float64, bufferLevel time.Duration) int {
	// Simple adaptation algorithm
	if bufferLevel < ba.bufferSize/2 {
		ba.currentBitrate = int(math.Max(float64(ba.minBitrate), float64(ba.currentBitrate)*0.8))
	} else if bufferLevel > ba.bufferSize*3/4 && downloadSpeed > float64(ba.currentBitrate)*1.5 {
		ba.currentBitrate = int(math.Min(float64(ba.maxBitrate), float64(ba.currentBitrate)*1.2))
	}

	return ba.currentBitrate
}
func (ba *BitrateAdapter) UpdateNetworkSpeed(bytesReceived int, duration time.Duration) {
	ba.mu.Lock()
	defer ba.mu.Unlock()

	speed := float64(bytesReceived) / duration.Seconds()
	ba.speedSamples = append(ba.speedSamples, speed)
	if len(ba.speedSamples) > ba.maxSamples {
		ba.speedSamples = ba.speedSamples[1:]
	}

	var total float64
	for _, s := range ba.speedSamples {
		total += s
	}
	ba.networkSpeed = total / float64(len(ba.speedSamples))
}

func (ba *BitrateAdapter) GetOptimalBitrate() int {
	ba.mu.Lock()
	defer ba.mu.Unlock()

	// Ajusta el bitrate basado en la velocidad de la red
	optimalBitrate := int(ba.networkSpeed * 0.8) // 80% de la velocidad de la red
	return clamp(optimalBitrate, ba.minBitrate, ba.maxBitrate)
}

func clamp(value, min, max int) int {
	if value < min {
		return min
	}
	if value > max {
		return max
	}
	return value
}
