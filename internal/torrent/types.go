package torrent

type TorrentInfo struct {
	InfoHash string
	Name     string
	Files    []FileInfo
}

type FileInfo struct {
	ID       int
	Name     string
	Size     int64
	Progress float64
}
