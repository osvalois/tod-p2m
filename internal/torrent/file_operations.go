// Package torrent provides functionality for managing torrent downloads and associated file operations.
package torrent

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/anacrolix/torrent"
)

// GetFile retrieves a file from a torrent.
// It returns an io.ReadSeeker for the requested file and any error encountered.
//
// Parameters:
//   - infoHash: The info hash of the torrent containing the file.
//   - fileIndex: The index of the file within the torrent.
//
// Returns:
//   - io.ReadSeeker: A reader for the requested file.
//   - error: Any error encountered during the operation.
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

// GetFileInfo retrieves information about a file in a torrent.
// It returns a FileInfo struct containing details about the file and any error encountered.
//
// Parameters:
//   - infoHash: The info hash of the torrent containing the file.
//   - fileIndex: The index of the file within the torrent.
//
// Returns:
//   - *FileInfo: A struct containing file information.
//   - error: Any error encountered during the operation.
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

// deleteFiles removes all files associated with a torrent.
// It attempts to delete each file and handles any errors encountered during the process.
//
// Parameters:
//   - t: The torrent whose files are to be deleted.
func (m *Manager) deleteFiles(t *torrent.Torrent) {
	for _, file := range t.Files() {
		path := filepath.Join(m.downloadDir, file.Path())
		err := m.retryFileOperation(func() error {
			return os.Remove(path)
		}, m.config.MaxRetries)

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

// removeEmptyDirs recursively removes empty directories within the given directory.
// It returns any error encountered during the operation.
//
// Parameters:
//   - dir: The directory to check and potentially remove.
//
// Returns:
//   - error: Any error encountered during the operation.
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

// handleBusyResource attempts to handle a resource that is reported as busy.
// It tries to close any open file handles and retry the deletion operation.
//
// Parameters:
//   - path: The path of the busy resource.
//
// Returns:
//   - error: Any error encountered during the operation.
func (m *Manager) handleBusyResource(path string) error {
	m.Logger.Warn().Str("path", path).Msg("Resource busy, attempting to release")

	infoHash := getInfoHashFromPath(path)
	if infoHash != "" {
		if err := m.cache.CloseFile(infoHash); err != nil {
			m.Logger.Error().Err(err).Str("infoHash", infoHash).Msg("Failed to close file in cache")
		}
	}

	// Wait a bit before trying again
	time.Sleep(m.config.RetryDelay)

	return m.retryFileOperation(func() error {
		err := os.Remove(path)
		if err != nil {
			if os.IsPermission(err) {
				// Try to change permissions if it's a permission error
				chmodErr := os.Chmod(path, 0666)
				if chmodErr != nil {
					m.Logger.Error().Err(chmodErr).Str("path", path).Msg("Failed to change file permissions")
					return chmodErr
				}
				// Try to delete again after changing permissions
				return os.Remove(path)
			}
		}
		return err
	}, m.config.MaxRetries)
}

// retryFileOperation attempts to perform a file operation multiple times with exponential backoff.
// It returns any error encountered during the final attempt.
//
// Parameters:
//   - operation: The file operation to perform.
//   - maxRetries: The maximum number of retry attempts.
//
// Returns:
//   - error: Any error encountered during the operation.
func (m *Manager) retryFileOperation(operation func() error, maxRetries int) error {
	var err error
	for attempt := 0; attempt < maxRetries; attempt++ {
		err = operation()
		if err == nil {
			return nil
		}

		if os.IsNotExist(err) {
			// If the file doesn't exist, there's no need to retry
			return nil
		}

		if strings.Contains(err.Error(), "device or resource busy") {
			// If the resource is busy, try to handle it specifically
			busyErr := m.handleBusyResource(getPathFromError(err))
			if busyErr == nil {
				// If handled successfully, try the original operation again
				continue
			}
		}

		// Calculate wait time with exponential backoff
		waitTime := m.config.InitialRetryWait * time.Duration(1<<uint(attempt))
		if waitTime > m.config.MaxRetryWait {
			waitTime = m.config.MaxRetryWait
		}

		m.Logger.Warn().Err(err).Int("attempt", attempt+1).Dur("wait", waitTime).Msg("File operation failed, retrying")
		time.Sleep(waitTime)
	}

	return fmt.Errorf("file operation failed after %d attempts: %w", maxRetries, err)
}

// getInfoHashFromPath extracts the info hash from a file path.
// It assumes the info hash is the first part of the file name, separated by an underscore.
//
// Parameters:
//   - path: The file path to extract the info hash from.
//
// Returns:
//   - string: The extracted info hash, or an empty string if not found.
func getInfoHashFromPath(path string) string {
	base := filepath.Base(path)
	parts := strings.Split(base, "_")
	if len(parts) > 0 {
		return parts[0]
	}
	return ""
}

// getPathFromError attempts to extract a file path from an error message.
// This is a simple implementation and may need to be adjusted based on the specific error format.
//
// Parameters:
//   - err: The error to extract the path from.
//
// Returns:
//   - string: The extracted path, or an empty string if not found.
func getPathFromError(err error) string {
	errMsg := err.Error()
	parts := strings.Split(errMsg, "\"")
	if len(parts) >= 3 {
		return parts[1]
	}
	return ""
}
