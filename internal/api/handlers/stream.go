package handlers

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/anacrolix/torrent"
	"github.com/go-chi/chi/v5"
	"github.com/go-chi/render"

	"tod-p2m/internal/config"
	"tod-p2m/internal/streaming"
	torrentManager "tod-p2m/internal/torrent"
	"tod-p2m/internal/utils"
)

type StreamSession struct {
	Buffer *streaming.Buffer
	File   *torrent.File
}

var (
	sessions     = make(map[string]*StreamSession)
	sessionMutex sync.RWMutex
)

func StreamFile(tm *torrentManager.Manager, cfg *config.Config) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), cfg.RequestTimeout)
		defer cancel()

		infoHash := chi.URLParam(r, "infoHash")
		fileIDStr := chi.URLParam(r, "fileID")

		fileID, err := strconv.Atoi(fileIDStr)
		if err != nil {
			renderError(w, r, http.StatusBadRequest, "Invalid file ID")
			return
		}

		t, err := tm.GetTorrent(infoHash)
		if err != nil {
			renderError(w, r, http.StatusInternalServerError, "Failed to get torrent")
			return
		}

		select {
		case <-t.GotInfo():
		case <-ctx.Done():
			renderError(w, r, http.StatusRequestTimeout, "Timeout waiting for torrent info")
			return
		}

		if fileID < 0 || fileID >= len(t.Files()) {
			renderError(w, r, http.StatusNotFound, "File not found")
			return
		}

		file := t.Files()[fileID]
		fileSize := file.Length()

		sessionID := fmt.Sprintf("%s-%d", infoHash, fileID)
		session, err := getOrCreateSession(sessionID, file, cfg)
		if err != nil {
			renderError(w, r, http.StatusInternalServerError, "Failed to create streaming session")
			return
		}

		fileInfo := utils.GetFileInfo(file)
		w.Header().Set("Content-Type", fileInfo.MimeType)
		w.Header().Set("Accept-Ranges", "bytes")

		rangeHeader := r.Header.Get("Range")
		if rangeHeader != "" {
			if err := handleRangeRequest(w, r, session, fileSize, cfg); err != nil {
				tm.Logger.Error().Err(err).Msg("Failed to handle range request")
				http.Error(w, "Internal Server Error", http.StatusInternalServerError)
				return
			}
		} else {
			w.Header().Set("Content-Length", strconv.FormatInt(fileSize, 10))
			w.WriteHeader(http.StatusOK)
			if err := streamWithBuffer(w, session, cfg); err != nil {
				tm.Logger.Error().Err(err).Msg("Failed to stream file")
				http.Error(w, "Internal Server Error", http.StatusInternalServerError)
				return
			}
		}

		metrics := session.Buffer.GetMetrics()
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

func getOrCreateSession(sessionID string, file *torrent.File, cfg *config.Config) (*StreamSession, error) {
	sessionMutex.RLock()
	session, exists := sessions[sessionID]
	sessionMutex.RUnlock()

	if !exists {
		sessionMutex.Lock()
		defer sessionMutex.Unlock()

		// Check again in case another goroutine created the session
		if session, exists = sessions[sessionID]; !exists {
			buffer := streaming.NewBuffer(file, cfg.CacheSize, cfg.MaxPieceHandlers)
			session = &StreamSession{
				Buffer: buffer,
				File:   file,
			}
			sessions[sessionID] = session

			go func() {
				<-time.After(cfg.SessionTimeout)
				sessionMutex.Lock()
				delete(sessions, sessionID)
				sessionMutex.Unlock()
				buffer.Close()
			}()
		}
	}

	return session, nil
}

func handleRangeRequest(w http.ResponseWriter, r *http.Request, session *StreamSession, fileSize int64, cfg *config.Config) error {
	rangeHeader := r.Header.Get("Range")
	start, end, err := parseRange(rangeHeader, fileSize)
	if err != nil {
		return fmt.Errorf("invalid range: %w", err)
	}

	w.Header().Set("Content-Range", fmt.Sprintf("bytes %d-%d/%d", start, end, fileSize))
	w.Header().Set("Content-Length", strconv.FormatInt(end-start+1, 10))
	w.WriteHeader(http.StatusPartialContent)

	return streamRange(w, session, start, end, cfg)
}

func streamWithBuffer(w http.ResponseWriter, session *StreamSession, cfg *config.Config) error {
	chunk := make([]byte, cfg.WriteBufferSize)
	for {
		n, err := session.Buffer.Read(chunk)
		if err != nil {
			if err == io.EOF {
				return nil
			}
			return fmt.Errorf("buffer read error: %w", err)
		}
		if n == 0 {
			break
		}

		if _, err := w.Write(chunk[:n]); err != nil {
			return fmt.Errorf("write error: %w", err)
		}
		if f, ok := w.(http.Flusher); ok {
			f.Flush()
		}

		session.File.Download()
	}
	return nil
}

func streamRange(w http.ResponseWriter, session *StreamSession, start, end int64, cfg *config.Config) error {
	if _, err := session.Buffer.ReadAt(make([]byte, 1), start); err != nil {
		return fmt.Errorf("failed to seek to start position: %w", err)
	}

	chunk := make([]byte, cfg.WriteBufferSize)
	for offset := start; offset <= end; {
		remainingBytes := end - offset + 1
		if remainingBytes < int64(len(chunk)) {
			chunk = chunk[:remainingBytes]
		}

		n, err := session.Buffer.ReadAt(chunk, offset)
		if err != nil && err != io.EOF {
			return fmt.Errorf("buffer read error: %w", err)
		}
		if n == 0 {
			break
		}

		if _, err := w.Write(chunk[:n]); err != nil {
			return fmt.Errorf("write error: %w", err)
		}
		if f, ok := w.(http.Flusher); ok {
			f.Flush()
		}

		offset += int64(n)
		session.File.Download()
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
		return 0, 0, fmt.Errorf("invalid start range: %w", err)
	}

	var end int64
	if parts[1] == "" {
		end = fileSize - 1
	} else {
		end, err = strconv.ParseInt(parts[1], 10, 64)
		if err != nil {
			return 0, 0, fmt.Errorf("invalid end range: %w", err)
		}
	}

	if start > end || end >= fileSize {
		return 0, 0, fmt.Errorf("invalid range")
	}

	return start, end, nil
}

func renderError(w http.ResponseWriter, r *http.Request, status int, message string) {
	render.Status(r, status)
	render.JSON(w, r, map[string]string{"error": message})
}
