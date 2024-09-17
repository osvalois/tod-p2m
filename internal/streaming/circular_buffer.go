package streaming

import (
	"io"
	"sync"
)

type CircularBuffer struct {
	buffer []byte
	size   int
	read   int
	write  int
	mu     sync.Mutex
}

func NewCircularBuffer(size int) *CircularBuffer {
	return &CircularBuffer{
		buffer: make([]byte, size),
		size:   size,
	}
}

func (cb *CircularBuffer) Write(p []byte) (n int, err error) {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	n = len(p)
	if n > cb.size {
		p = p[n-cb.size:]
		n = cb.size
	}

	firstPart := cb.size - cb.write
	if firstPart > n {
		firstPart = n
	}

	copy(cb.buffer[cb.write:], p[:firstPart])
	copy(cb.buffer, p[firstPart:])

	cb.write = (cb.write + n) % cb.size
	if cb.write == cb.read {
		cb.read = (cb.read + 1) % cb.size
	}

	return n, nil
}

func (cb *CircularBuffer) Read(p []byte) (n int, err error) {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	if cb.read == cb.write {
		return 0, io.EOF
	}

	n = len(p)
	if n > cb.Available() {
		n = cb.Available()
	}

	firstPart := cb.size - cb.read
	if firstPart > n {
		firstPart = n
	}

	copy(p, cb.buffer[cb.read:cb.read+firstPart])
	copy(p[firstPart:], cb.buffer[:n-firstPart])

	cb.read = (cb.read + n) % cb.size

	return n, nil
}

func (cb *CircularBuffer) Available() int {
	if cb.write >= cb.read {
		return cb.write - cb.read
	}
	return cb.size - cb.read + cb.write
}
