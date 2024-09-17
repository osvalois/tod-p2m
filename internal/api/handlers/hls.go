package handlers

// internal/internal/handlers/hls.go
import (
	"fmt"
	"io"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/render"

	"tod-p2m/internal/streaming/hls"
	"tod-p2m/internal/torrent"
)

func HLSPlaylist(tm *torrent.Manager) http.HandlerFunc {
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
		segmenter := hls.NewSegmenter(file, 10*1024*1024) // 10MB por segmento
		w.Header().Set("Content-Type", "application/x-mpegURL")
		w.WriteHeader(http.StatusOK)

		fmt.Fprintf(w, "#EXTM3U\n")
		fmt.Fprintf(w, "#EXT-X-VERSION:3\n")
		fmt.Fprintf(w, "#EXT-X-TARGETDURATION:10\n")
		fmt.Fprintf(w, "#EXT-X-MEDIA-SEQUENCE:0\n")

		for i := 0; i < segmenter.SegmentCount(); i++ {
			fmt.Fprintf(w, "#EXTINF:10.0,\n")
			fmt.Fprintf(w, "%d.ts\n", i)
		}

		fmt.Fprintf(w, "#EXT-X-ENDLIST\n")
	}
}

func HLSSegment(tm *torrent.Manager) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		infoHash := chi.URLParam(r, "infoHash")
		fileIDStr := chi.URLParam(r, "fileID")
		segmentIDStr := chi.URLParam(r, "segmentID")

		fileID, err := strconv.Atoi(fileIDStr)
		if err != nil {
			render.Status(r, http.StatusBadRequest)
			render.JSON(w, r, map[string]string{"error": "Invalid file ID"})
			return
		}

		segmentID, err := strconv.Atoi(segmentIDStr)
		if err != nil {
			render.Status(r, http.StatusBadRequest)
			render.JSON(w, r, map[string]string{"error": "Invalid segment ID"})
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
		segmenter := hls.NewSegmenter(file, 10*1024*1024) // 10MB por segmento

		segment, err := segmenter.GetSegment(segmentID)
		if err != nil {
			render.Status(r, http.StatusNotFound)
			render.JSON(w, r, map[string]string{"error": "Segment not found"})
			return
		}

		w.Header().Set("Content-Type", "video/MP2T")
		w.WriteHeader(http.StatusOK)

		_, err = io.Copy(w, segment)
		if err != nil {
			tm.Logger.Error().Err(err).Msg("Failed to stream segment")
		}
	}
}
