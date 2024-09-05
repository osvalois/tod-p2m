package hls

import (
	"fmt"
	"io"
)

type PlaylistGenerator struct {
	SegmentDuration int
}

func NewPlaylistGenerator(segmentDuration int) *PlaylistGenerator {
	return &PlaylistGenerator{
		SegmentDuration: segmentDuration,
	}
}

func (pg *PlaylistGenerator) Generate(w io.Writer, segmentCount int) error {
	_, err := fmt.Fprintf(w, "#EXTM3U\n")
	if err != nil {
		return err
	}

	_, err = fmt.Fprintf(w, "#EXT-X-VERSION:3\n")
	if err != nil {
		return err
	}

	_, err = fmt.Fprintf(w, "#EXT-X-TARGETDURATION:%d\n", pg.SegmentDuration)
	if err != nil {
		return err
	}

	_, err = fmt.Fprintf(w, "#EXT-X-MEDIA-SEQUENCE:0\n")
	if err != nil {
		return err
	}

	for i := 0; i < segmentCount; i++ {
		_, err = fmt.Fprintf(w, "#EXTINF:%d.0,\n", pg.SegmentDuration)
		if err != nil {
			return err
		}

		_, err = fmt.Fprintf(w, "%d.ts\n", i)
		if err != nil {
			return err
		}
	}

	_, err = fmt.Fprintf(w, "#EXT-X-ENDLIST\n")
	return err
}
