package handlers

import (
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/render"

	"tod-p2m/internal/torrent"
	"tod-p2m/internal/utils"
)

func StreamFile(tm *torrent.Manager) http.HandlerFunc {
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
			if err := utils.HandleRangeRequest(w, r, reader, file.Length()); err != nil {
				tm.Logger.Error().Err(err).Msg("Failed to handle range request")
				http.Error(w, "Internal Server Error", http.StatusInternalServerError)
				return
			}
		} else {
			w.Header().Set("Content-Length", strconv.FormatInt(file.Length(), 10))
			w.WriteHeader(http.StatusOK)
			if _, err := utils.CopyBuffer(w, reader); err != nil {
				tm.Logger.Error().Err(err).Msg("Failed to stream file")
			}
		}
	}
}
