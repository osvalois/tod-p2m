package adaptive

import (
	"math"
	"time"
)

type BitrateAdapter struct {
	minBitrate     int
	maxBitrate     int
	bufferSize     time.Duration
	currentBitrate int
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
