// internal/api/handlers/stream.go

package handlers

import (
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/anacrolix/torrent"
	"github.com/go-chi/chi/v5"
	"github.com/go-chi/render"

	"tod-p2m/internal/streaming"
	torrentManager "tod-p2m/internal/torrent"
	"tod-p2m/internal/utils"
)

const (
	bufferSize      = 20 * 1024 * 1024 // 20MB
	prefetchSize    = 10
	streamChunkSize = 1024 * 1024 // 1MB
	waitTimeout     = 30 * time.Second
)

func StreamFile(tm *torrentManager.Manager) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		infoHash := chi.URLParam(r, "infoHash")
		fileIDStr := chi.URLParam(r, "fileID")

		fileID, err := strconv.Atoi(fileIDStr)
		if err != nil {
			render.Status(r, http.StatusBadRequest)
			render.JSON(w, r, map[string]string{"error": "Invalid file ID"})
			return
		}

		t, err := tm.GetTorrent(infoHash)
		if err != nil {
			render.Status(r, http.StatusInternalServerError)
			render.JSON(w, r, map[string]string{"error": "Failed to get torrent"})
			return
		}

		// Esperar a que el torrent obtenga la información necesaria
		select {
		case <-t.GotInfo():
		case <-time.After(waitTimeout):
			render.Status(r, http.StatusRequestTimeout)
			render.JSON(w, r, map[string]string{"error": "Timeout waiting for torrent info"})
			return
		}

		if fileID < 0 || fileID >= len(t.Files()) {
			render.Status(r, http.StatusNotFound)
			render.JSON(w, r, map[string]string{"error": "File not found"})
			return
		}

		file := t.Files()[fileID]
		fileSize := file.Length()

		// Iniciar la descarga del archivo
		file.Download()

		buffer := streaming.NewBuffer(file, bufferSize, prefetchSize)
		defer buffer.Close()

		fileInfo := utils.GetFileInfo(file)
		w.Header().Set("Content-Type", fileInfo.MimeType)
		w.Header().Set("Accept-Ranges", "bytes")

		rangeHeader := r.Header.Get("Range")
		if rangeHeader != "" {
			if err := handleRangeRequest(w, r, buffer, fileSize); err != nil {
				http.Error(w, "Internal Server Error", http.StatusInternalServerError)
				return
			}
		} else {
			w.Header().Set("Content-Length", strconv.FormatInt(fileSize, 10))
			w.WriteHeader(http.StatusOK)
			if err := streamWithBuffer(w, buffer, file); err != nil {
				http.Error(w, "Internal Server Error", http.StatusInternalServerError)
				return
			}
		}

		// Log streaming metrics
		metrics := buffer.GetMetrics()
		tm.Logger.Info().
			Str("infoHash", infoHash).
			Int("fileID", fileID).
			Int64("bytesRead", metrics.BytesRead).
			Int64("cacheHits", metrics.CacheHits).
			Int64("cacheMisses", metrics.CacheMisses).
			Int64("prefetchCount", metrics.PrefetchCount).
			Msg("Streaming completed")
	}
}

func handleRangeRequest(w http.ResponseWriter, r *http.Request, buffer *streaming.Buffer, fileSize int64) error {
	rangeHeader := r.Header.Get("Range")
	start, end, err := parseRange(rangeHeader, fileSize)
	if err != nil {
		http.Error(w, err.Error(), http.StatusRequestedRangeNotSatisfiable)
		return err
	}

	w.Header().Set("Content-Range", fmt.Sprintf("bytes %d-%d/%d", start, end, fileSize))
	w.Header().Set("Content-Length", strconv.FormatInt(end-start+1, 10))
	w.WriteHeader(http.StatusPartialContent)

	return streamRange(w, buffer, start, end)
}

func streamWithBuffer(w http.ResponseWriter, buffer *streaming.Buffer, file *torrent.File) error {
	chunk := make([]byte, streamChunkSize)
	for {
		n, err := buffer.Read(chunk)
		if err != nil && err != io.EOF {
			return err
		}
		if n == 0 {
			break
		}

		if _, err := w.Write(chunk[:n]); err != nil {
			return err
		}
		if f, ok := w.(http.Flusher); ok {
			f.Flush()
		}

		// Asegurar que la descarga continúe
		file.Download()
	}
	return nil
}

func streamRange(w http.ResponseWriter, buffer *streaming.Buffer, start, end int64) error {
	_, err := buffer.ReadAt(make([]byte, 1), start) // Trigger prefetch
	if err != nil {
		return err
	}

	chunk := make([]byte, streamChunkSize)
	for offset := start; offset <= end; {
		remainingBytes := end - offset + 1
		if remainingBytes < int64(len(chunk)) {
			chunk = chunk[:remainingBytes]
		}

		n, err := buffer.ReadAt(chunk, offset)
		if err != nil && err != io.EOF {
			return err
		}
		if n == 0 {
			break
		}

		if _, err := w.Write(chunk[:n]); err != nil {
			return err
		}
		if f, ok := w.(http.Flusher); ok {
			f.Flush()
		}

		offset += int64(n)
	}
	return nil
}

func parseRange(rangeHeader string, fileSize int64) (int64, int64, error) {
	if rangeHeader == "" {
		return 0, fileSize - 1, nil
	}

	const prefix = "bytes="
	if !strings.HasPrefix(rangeHeader, prefix) {
		return 0, 0, fmt.Errorf("invalid range header")
	}

	rangePart := strings.TrimPrefix(rangeHeader, prefix)
	parts := strings.Split(rangePart, "-")
	if len(parts) != 2 {
		return 0, 0, fmt.Errorf("invalid range format")
	}

	start, err := strconv.ParseInt(parts[0], 10, 64)
	if err != nil {
		return 0, 0, err
	}

	var end int64
	if parts[1] == "" {
		end = fileSize - 1
	} else {
		end, err = strconv.ParseInt(parts[1], 10, 64)
		if err != nil {
			return 0, 0, err
		}
	}

	if start > end || end >= fileSize {
		return 0, 0, fmt.Errorf("invalid range")
	}

	return start, end, nil
}
