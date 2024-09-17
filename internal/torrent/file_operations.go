package torrent

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

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
		err := m.retryFileOperation(func() error {
			return os.Remove(path)
		}, 3)

		if err != nil {
			if os.IsNotExist(err) {
				m.Logger.Debug().Str("path", path).Msg("File already deleted or doesn't exist")
			} else if strings.Contains(err.Error(), "device or resource busy") {
				if handleErr := m.handleBusyResource(path); handleErr != nil {
					m.Logger.Error().Err(handleErr).Str("path", path).Msg("Failed to handle busy resource")
				}
			} else {
				m.Logger.Error().Err(err).Str("path", path).Msg("Failed to delete file")
			}
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
		return fmt.Errorf("failed to read directory %s: %w", dir, err)
	}

	for _, entry := range entries {
		if entry.IsDir() {
			subdir := filepath.Join(dir, entry.Name())
			if err := m.removeEmptyDirs(subdir); err != nil {
				m.Logger.Warn().Err(err).Str("path", subdir).Msg("Failed to remove subdirectory")
			}
		}
	}

	// Check if the directory is empty after processing subdirectories
	entries, err = os.ReadDir(dir)
	if err != nil {
		return fmt.Errorf("failed to re-read directory %s: %w", dir, err)
	}

	if len(entries) == 0 && dir != m.downloadDir {
		if err := os.Remove(dir); err != nil {
			if !os.IsNotExist(err) {
				return fmt.Errorf("failed to remove empty directory %s: %w", dir, err)
			}
			m.Logger.Debug().Str("path", dir).Msg("Directory already removed")
		} else {
			m.Logger.Info().Str("path", dir).Msg("Removed empty directory")
		}
	}

	return nil
}
