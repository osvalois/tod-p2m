package handlers

import (
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"

	"github.com/anacrolix/torrent/metainfo"
	"github.com/go-chi/chi/v5"
	"github.com/go-chi/render"

	"tod-p2m/internal/torrent"
	"tod-p2m/internal/utils"
)

const (
	bufferSize = 256 * 1024 // 256KB
)

func StreamFile(tm *torrent.Manager) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		infoHashStr := chi.URLParam(r, "infoHash")
		fileIDStr := chi.URLParam(r, "fileID")

		infoHash := metainfo.NewHashFromHex(infoHashStr)
		if infoHash.IsZero() {
			render.Status(r, http.StatusBadRequest)
			render.JSON(w, r, map[string]string{"error": "Invalid info hash"})
			return
		}

		fileID, err := strconv.Atoi(fileIDStr)
		if err != nil {
			render.Status(r, http.StatusBadRequest)
			render.JSON(w, r, map[string]string{"error": "Invalid file ID"})
			return
		}

		t, err := tm.GetTorrent(infoHash.HexString())
		if err != nil {
			render.Status(r, http.StatusInternalServerError)
			render.JSON(w, r, map[string]string{"error": "Failed to get torrent"})
			return
		}

		if fileID < 0 || fileID >= len(t.Files()) {
			render.Status(r, http.StatusNotFound)
			render.JSON(w, r, map[string]string{"error": "File not found"})
			return
		}

		file := t.Files()[fileID]
		reader := file.NewReader()
		defer reader.Close()

		fileInfo := utils.GetFileInfo(file)
		w.Header().Set("Content-Type", fileInfo.MimeType)
		w.Header().Set("Content-Disposition", fileInfo.ContentDisposition)
		w.Header().Set("Accept-Ranges", "bytes")
		w.Header().Set("Cache-Control", "public, max-age=3600")

		rangeHeader := r.Header.Get("Range")
		if rangeHeader != "" {
			if err := handleRangeRequest(w, r, reader, file.Length()); err != nil {
				http.Error(w, "Internal Server Error", http.StatusInternalServerError)
				return
			}
		} else {
			w.Header().Set("Content-Length", strconv.FormatInt(file.Length(), 10))
			w.WriteHeader(http.StatusOK)
			if err := streamFile(w, reader); err != nil {
				http.Error(w, "Internal Server Error", http.StatusInternalServerError)
			}
		}
	}
}

func handleRangeRequest(w http.ResponseWriter, r *http.Request, reader io.Reader, totalSize int64) error {
	rangeHeader := r.Header.Get("Range")
	start, end, err := parseRange(rangeHeader, totalSize)
	if err != nil {
		http.Error(w, err.Error(), http.StatusRequestedRangeNotSatisfiable)
		return err
	}

	w.Header().Set("Content-Range", fmt.Sprintf("bytes %d-%d/%d", start, end, totalSize))
	w.Header().Set("Content-Length", strconv.FormatInt(end-start+1, 10))
	w.WriteHeader(http.StatusPartialContent)

	_, err = io.CopyN(w, reader, end-start+1)
	return err
}

func streamFile(w http.ResponseWriter, reader io.Reader) error {
	buf := make([]byte, bufferSize)
	for {
		n, err := reader.Read(buf)
		if err != nil && err != io.EOF {
			return err
		}
		if n == 0 {
			break
		}
		if _, err := w.Write(buf[:n]); err != nil {
			return err
		}
		if f, ok := w.(http.Flusher); ok {
			f.Flush()
		}
	}
	return nil
}

func parseRange(rangeHeader string, totalSize int64) (int64, int64, error) {
	if rangeHeader == "" {
		return 0, totalSize - 1, nil
	}

	const prefix = "bytes="
	if !strings.HasPrefix(rangeHeader, prefix) {
		return 0, 0, errors.New("invalid range header")
	}

	rangePart := strings.TrimPrefix(rangeHeader, prefix)
	parts := strings.Split(rangePart, "-")
	if len(parts) != 2 {
		return 0, 0, errors.New("invalid range format")
	}

	start, err := strconv.ParseInt(parts[0], 10, 64)
	if err != nil {
		return 0, 0, err
	}

	var end int64
	if parts[1] == "" {
		end = totalSize - 1
	} else {
		end, err = strconv.ParseInt(parts[1], 10, 64)
		if err != nil {
			return 0, 0, err
		}
	}

	if start > end || end >= totalSize {
		return 0, 0, errors.New("invalid range")
	}

	return start, end, nil
}
