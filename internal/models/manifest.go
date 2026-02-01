// Package models defines core data structures for media streams.
package models

import (
	"fmt"
	"strings"
	"time"

	"github.com/mohaanymo/veld/internal/decryptor"
)

// ManifestType represents the type of streaming manifest.
type ManifestType int

const (
	ManifestHLS ManifestType = iota
	ManifestDASH
)

func (t ManifestType) String() string {
	switch t {
	case ManifestHLS:
		return "HLS"
	case ManifestDASH:
		return "DASH"
	default:
		return "Unknown"
	}
}

// Manifest represents a parsed streaming manifest.
type Manifest struct {
	URL      string
	Type     ManifestType
	Tracks   []*Track
	Duration time.Duration
}

// TrackType represents the type of media track.
type TrackType int

const (
	TrackVideo TrackType = iota
	TrackAudio
	TrackSubtitle
)

func (t TrackType) String() string {
	switch t {
	case TrackVideo:
		return "video"
	case TrackAudio:
		return "audio"
	case TrackSubtitle:
		return "subtitle"
	default:
		return "unknown"
	}
}

// Track represents a media track (video, audio, or subtitle).
type Track struct {
	ID          string
	Type        TrackType
	Codec       string
	Bandwidth   int64
	Resolution  Resolution
	Language    string
	Name        string
	Segments    []*Segment
	InitSegment *Segment

	// Media playlist URL for lazy loading (HLS audio/subtitle tracks)
	MediaPlaylistURL string

	// Encryption info
	Decryptor     *decryptor.Decryptor    // For CENC (DASH)
	HLSDecryptor  *decryptor.HLSDecryptor // For AES-128 (HLS)
	Encrypted     bool
	EncryptionURI string
	EncryptionIV  []byte
	KeyID         string
}

// IsVideo returns true if track is a video track.
func (t *Track) IsVideo() bool {
	if t.Type == TrackVideo {
		return true
	}
	if t.Resolution.Height > 0 {
		return true
	}
	return hasVideoCodec(t.Codec)
}

// IsAudio returns true if track is an audio track.
func (t *Track) IsAudio() bool {
	if t.Type == TrackAudio {
		return true
	}
	return hasAudioCodec(t.Codec)
}

// IsSubtitle returns true if track is a subtitle track.
func (t *Track) IsSubtitle() bool {
	if t.Type == TrackSubtitle {
		return true
	}
	return hasSubtitleCodec(t.Codec)
}

// Resolution represents video dimensions.
type Resolution struct {
	Width  int
	Height int
}

func (r Resolution) String() string {
	if r.Width == 0 && r.Height == 0 {
		return ""
	}
	return fmt.Sprintf("%dx%d", r.Width, r.Height)
}

// QualityLabel returns a human-readable quality label (e.g., "1080p").
func (r Resolution) QualityLabel() string {
	switch {
	case r.Height >= 2160:
		return "4K"
	case r.Height >= 1440:
		return "1440p"
	case r.Height >= 1080:
		return "1080p"
	case r.Height >= 720:
		return "720p"
	case r.Height >= 480:
		return "480p"
	case r.Height >= 360:
		return "360p"
	case r.Height > 0:
		return fmt.Sprintf("%dp", r.Height)
	default:
		return ""
	}
}

// Segment represents a media segment.
type Segment struct {
	Index     int
	URL       string
	Duration  time.Duration
	Size      int64
	ByteRange *ByteRange
	Data      []byte // In-memory data (deprecated, use FilePath)
	FilePath  string // Path to segment file on disk
}

// ByteRange represents HTTP Range request parameters.
type ByteRange struct {
	Start int64
	End   int64
}

// Codec detection helpers (centralized to avoid duplication)
var (
	audioCodecs    = []string{"mp4a", "aac", "ac-3", "ec-3", "opus", "vorbis", "flac", "mp3"}
	videoCodecs    = []string{"avc", "h264", "hevc", "h265", "hvc1", "hev1", "vp9", "vp8", "av01", "av1"}
	subtitleCodecs = []string{"stpp", "wvtt", "ttml", "webvtt", "vtt", "srt"}
)

func hasAudioCodec(codec string) bool {
	codec = strings.ToLower(codec)
	for _, ac := range audioCodecs {
		if strings.Contains(codec, ac) {
			return true
		}
	}
	return false
}

func hasVideoCodec(codec string) bool {
	codec = strings.ToLower(codec)
	for _, vc := range videoCodecs {
		if strings.Contains(codec, vc) {
			return true
		}
	}
	return false
}

func hasSubtitleCodec(codec string) bool {
	codec = strings.ToLower(codec)
	for _, sc := range subtitleCodecs {
		if strings.Contains(codec, sc) {
			return true
		}
	}
	return false
}

// HasAudioCodec is exported for use by other packages.
func HasAudioCodec(codec string) bool { return hasAudioCodec(codec) }

// HasVideoCodec is exported for use by other packages.
func HasVideoCodec(codec string) bool { return hasVideoCodec(codec) }

// HasSubtitleCodec is exported for use by other packages.
func HasSubtitleCodec(codec string) bool { return hasSubtitleCodec(codec) }
