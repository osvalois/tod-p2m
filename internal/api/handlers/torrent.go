package handlers

import (
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/render"

	"tod-p2m/internal/torrent"
)

func GetTorrentInfo(tm *torrent.Manager) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		infoHash := chi.URLParam(r, "infoHash")

		t, err := tm.GetTorrent(infoHash)
		if err != nil {
			render.Status(r, http.StatusInternalServerError)
			render.JSON(w, r, map[string]string{"error": "Failed to get torrent"})
			return
		}

		info := torrent.TorrentInfo{
			InfoHash: t.InfoHash().String(),
			Name:     t.Name(),
			Files:    make([]torrent.FileInfo, 0, len(t.Files())),
		}

		for i, f := range t.Files() {
			info.Files = append(info.Files, torrent.FileInfo{
				ID:       i,
				Name:     f.DisplayPath(),
				Size:     f.Length(),
				Progress: float64(f.BytesCompleted()) / float64(f.Length()),
			})
		}

		render.JSON(w, r, info)
	}
}
