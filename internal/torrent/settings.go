package torrent

// SetMaxConnections sets the maximum number of connections for the torrent client
func (m *Manager) SetMaxConnections(maxConnections int) {
	m.client.SetMaxConnections(maxConnections)
}

// SetDownloadLimit sets the download speed limit for the torrent client
func (m *Manager) SetDownloadLimit(downloadLimit int64) {
	m.client.SetDownloadLimit(downloadLimit)
}

// SetUploadLimit sets the upload speed limit for the torrent client
func (m *Manager) SetUploadLimit(uploadLimit int64) {
	m.client.SetUploadLimit(uploadLimit)
}
