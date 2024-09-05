package utils

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/anacrolix/torrent"
)

type FileInfo struct {
	MimeType           string
	ContentDisposition string
}

func GetFileInfo(file *torrent.File) FileInfo {
	fileName := filepath.Base(file.DisplayPath())
	fileExt := strings.ToLower(filepath.Ext(fileName))
	mimeType := getMIMEType(fileExt)
	contentDisposition := fmt.Sprintf(`inline; filename="%s"`, fileName)

	return FileInfo{
		MimeType:           mimeType,
		ContentDisposition: contentDisposition,
	}
}

func getMIMEType(fileExt string) string {
	switch fileExt {
	case ".mp4", ".m4v":
		return "video/mp4"
	case ".webm":
		return "video/webm"
	case ".ogg":
		return "video/ogg"
	case ".mp3":
		return "audio/mpeg"
	case ".wav":
		return "audio/wav"
	case ".pdf":
		return "application/pdf"
	case ".jpg", ".jpeg":
		return "image/jpeg"
	case ".png":
		return "image/png"
	case ".gif":
		return "image/gif"
	case ".svg":
		return "image/svg+xml"
	default:
		return "application/octet-stream"
	}
}
