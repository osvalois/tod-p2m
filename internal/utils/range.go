package utils

// internal/internal/streaming/utils/range.go
import (
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
)

func HandleRangeRequest(w http.ResponseWriter, r *http.Request, reader io.ReadSeeker, fileSize int64) error {
	rangeHeader := r.Header.Get("Range")
	if rangeHeader == "" {
		return fmt.Errorf("no range header")
	}

	rangeParts := strings.Split(strings.TrimPrefix(rangeHeader, "bytes="), "-")
	if len(rangeParts) != 2 {
		return fmt.Errorf("invalid range header")
	}

	start, err := strconv.ParseInt(rangeParts[0], 10, 64)
	if err != nil {
		return fmt.Errorf("invalid range start: %w", err)
	}

	var end int64
	if rangeParts[1] != "" {
		end, err = strconv.ParseInt(rangeParts[1], 10, 64)
		if err != nil {
			return fmt.Errorf("invalid range end: %w", err)
		}
	} else {
		end = fileSize - 1
	}

	if start >= fileSize || end >= fileSize || start > end {
		w.Header().Set("Content-Range", fmt.Sprintf("bytes */%d", fileSize))
		w.WriteHeader(http.StatusRequestedRangeNotSatisfiable)
		return nil
	}

	w.Header().Set("Content-Range", fmt.Sprintf("bytes %d-%d/%d", start, end, fileSize))
	w.Header().Set("Content-Length", strconv.FormatInt(end-start+1, 10))
	w.WriteHeader(http.StatusPartialContent)

	_, err = reader.Seek(start, io.SeekStart)
	if err != nil {
		return fmt.Errorf("failed to seek: %w", err)
	}

	_, err = io.CopyN(w, reader, end-start+1)
	if err != nil {
		return fmt.Errorf("failed to copy range: %w", err)
	}

	return nil
}

func CopyBuffer(dst io.Writer, src io.Reader) (int64, error) {
	buf := make([]byte, 32*1024)
	return io.CopyBuffer(dst, src, buf)
}
