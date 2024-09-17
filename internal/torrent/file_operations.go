package torrent

// internal/torrent/file_operations.go
import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

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
func (m *Manager) handleBusyResource(path string) error {
	m.Logger.Warn().Str("path", path).Msg("Resource busy, attempting to release")

	infoHash := getInfoHashFromPath(path)
	if infoHash != "" {
		if err := m.cache.CloseFile(infoHash); err != nil {
			m.Logger.Error().Err(err).Str("infoHash", infoHash).Msg("Failed to close file in cache")
		}
	}

	// Espera un poco antes de intentar de nuevo
	time.Sleep(1 * time.Second)

	return m.retryFileOperation(func() error {
		err := os.Remove(path)
		if err != nil {
			if os.IsPermission(err) {
				// Intenta cambiar los permisos si es un error de permisos
				chmodErr := os.Chmod(path, 0666)
				if chmodErr != nil {
					m.Logger.Error().Err(chmodErr).Str("path", path).Msg("Failed to change file permissions")
					return chmodErr
				}
				// Intenta eliminar de nuevo después de cambiar los permisos
				return os.Remove(path)
			}
		}
		return err
	}, maxRetries)
}

func (m *Manager) retryFileOperation(operation func() error, maxRetries int) error {
	var err error
	for attempt := 0; attempt < maxRetries; attempt++ {
		err = operation()
		if err == nil {
			return nil
		}

		if os.IsNotExist(err) {
			// Si el archivo no existe, no hay necesidad de reintentar
			return nil
		}

		if strings.Contains(err.Error(), "device or resource busy") {
			// Si el recurso está ocupado, intenta manejarlo específicamente
			busyErr := m.handleBusyResource(getPathFromError(err))
			if busyErr == nil {
				// Si se manejó correctamente, intenta la operación original de nuevo
				continue
			}
		}

		// Calcula el tiempo de espera con retroceso exponencial
		waitTime := initialRetryWait * time.Duration(1<<uint(attempt))
		if waitTime > maxRetryWait {
			waitTime = maxRetryWait
		}

		m.Logger.Warn().Err(err).Int("attempt", attempt+1).Dur("wait", waitTime).Msg("File operation failed, retrying")
		time.Sleep(waitTime)
	}

	return fmt.Errorf("file operation failed after %d attempts: %w", maxRetries, err)
}

func getInfoHashFromPath(path string) string {
	base := filepath.Base(path)
	parts := strings.Split(base, "_")
	if len(parts) > 0 {
		return parts[0]
	}
	return ""
}

func getPathFromError(err error) string {
	// Esta función es una implementación de ejemplo. Puede que necesites ajustarla
	// dependiendo de cómo se formateen los mensajes de error en tu sistema.
	errMsg := err.Error()
	parts := strings.Split(errMsg, "\"")
	if len(parts) >= 3 {
		return parts[1]
	}
	return ""
}
