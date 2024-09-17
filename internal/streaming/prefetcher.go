package streaming

import (
	"container/ring"
	"io"
	"sync"
	"time"

	"github.com/anacrolix/torrent"
)

type Prefetcher struct {
	file       *torrent.File
	reader     io.Reader
	bufferSize int
	readAhead  int
	mu         sync.Mutex
	buffer     *ring.Ring
}

func NewPrefetcher(file *torrent.File, bufferSize, readAhead int) *Prefetcher {
	p := &Prefetcher{
		file:       file,
		reader:     file.NewReader(),
		bufferSize: bufferSize,
		readAhead:  readAhead,
		buffer:     ring.New(readAhead),
	}
	go p.prefetchRoutine()
	return p
}

func (p *Prefetcher) prefetchRoutine() {
	for {
		p.mu.Lock()
		nextPiece := p.buffer.Next()
		p.mu.Unlock()

		if nextPiece.Value == nil {
			pieceIndex := p.file.Offset() / int64(p.file.Torrent().Info().PieceLength)
			p.file.Torrent().Piece(int(pieceIndex)).SetPriority(torrent.PiecePriorityNow)

			buffer := make([]byte, p.bufferSize)
			n, err := p.reader.Read(buffer)
			if err != nil && err != io.EOF {
				// Manejar el error aquí
				time.Sleep(100 * time.Millisecond)
				continue
			}

			p.mu.Lock()
			nextPiece.Value = buffer[:n]
			p.mu.Unlock()
		}

		time.Sleep(100 * time.Millisecond)
	}
}

func (p *Prefetcher) Read(b []byte) (int, error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.buffer.Value == nil {
		return p.reader.Read(b)
	}

	data := p.buffer.Value.([]byte)
	n := copy(b, data)

	if n < len(data) {
		p.buffer.Value = data[n:]
	} else {
		p.buffer.Value = nil
		p.buffer = p.buffer.Next()
	}

	return n, nil
}
