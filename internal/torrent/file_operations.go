package torrent

import (
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/anacrolix/torrent"
)

// GetFile retrieves a file from a torrent
func (m *Manager) GetFile(infoHash string, fileIndex int) (io.ReadSeeker, error) {
	t, err := m.GetTorrent(infoHash)
	if err != nil {
		return nil, fmt.Errorf("failed to get torrent: %w", err)
	}

	files := t.Files()
	if fileIndex < 0 || fileIndex >= len(files) {
		return nil, ErrInvalidFileIndex
	}

	file := files[fileIndex]
	filePath := filepath.Join(m.downloadDir, file.Path())

	if m.config.KeepFiles {
		if _, err := os.Stat(filePath); err == nil {
			return os.Open(filePath)
		}
	}

	return file.NewReader(), nil
}

// GetFileInfo retrieves information about a file in a torrent
func (m *Manager) GetFileInfo(infoHash string, fileIndex int) (*FileInfo, error) {
	t, err := m.GetTorrent(infoHash)
	if err != nil {
		return nil, fmt.Errorf("failed to get torrent: %w", err)
	}

	files := t.Files()
	if fileIndex < 0 || fileIndex >= len(files) {
		return nil, ErrInvalidFileIndex
	}

	file := files[fileIndex]
	return &FileInfo{
		Name:     file.DisplayPath(),
		Size:     file.Length(),
		Progress: float64(file.BytesCompleted()) / float64(file.Length()),
	}, nil
}

func (m *Manager) deleteFiles(t *torrent.Torrent) {
	for _, file := range t.Files() {
		path := filepath.Join(m.downloadDir, file.Path())
		if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
			m.Logger.Error().Err(err).Str("path", path).Msg("Failed to delete file")
		} else {
			m.Logger.Info().Str("path", path).Msg("File deleted successfully")
		}
	}

	if err := m.removeEmptyDirs(m.downloadDir); err != nil {
		m.Logger.Error().Err(err).Str("path", m.downloadDir).Msg("Failed to remove empty directories")
	}
}

func (m *Manager) removeEmptyDirs(dir string) error {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return err
	}

	for _, entry := range entries {
		if entry.IsDir() {
			subdir := filepath.Join(dir, entry.Name())
			if err := m.removeEmptyDirs(subdir); err != nil {
				return err
			}
		}
	}

	entries, err = os.ReadDir(dir)
	if err != nil {
		return err
	}

	if len(entries) == 0 && dir != m.downloadDir {
		if err := os.Remove(dir); err != nil {
			return err
		}
		m.Logger.Info().Str("path", dir).Msg("Removed empty directory")
	}

	return nil
}
