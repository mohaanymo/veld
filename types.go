package veld

import (
	"github.com/mohaanymo/veld/internal/models"
)

// TrackType represents the type of media track.
type TrackType int

const (
	TrackVideo    TrackType = TrackType(models.TrackVideo)
	TrackAudio    TrackType = TrackType(models.TrackAudio)
	TrackSubtitle TrackType = TrackType(models.TrackSubtitle)
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
	internal *models.Track
}

// ID returns the track's unique identifier.
func (t *Track) ID() string {
	return t.internal.ID
}

// Type returns the track type (video, audio, or subtitle).
func (t *Track) Type() TrackType {
	return TrackType(t.internal.Type)
}

// Codec returns the track's codec string (e.g., "avc1.64001f", "mp4a.40.2").
func (t *Track) Codec() string {
	return t.internal.Codec
}

// Bandwidth returns the track's bandwidth in bits per second.
func (t *Track) Bandwidth() int64 {
	return t.internal.Bandwidth
}

// Width returns the video width in pixels (0 for non-video tracks).
func (t *Track) Width() int {
	return t.internal.Resolution.Width
}

// Height returns the video height in pixels (0 for non-video tracks).
func (t *Track) Height() int {
	return t.internal.Resolution.Height
}

// Resolution returns the resolution as "WxH" string (empty for non-video).
func (t *Track) Resolution() string {
	return t.internal.Resolution.String()
}

// QualityLabel returns a human-readable quality label (e.g., "1080p", "720p", "4K").
func (t *Track) QualityLabel() string {
	return t.internal.Resolution.QualityLabel()
}

// Language returns the track's language code (e.g., "en", "es").
func (t *Track) Language() string {
	return t.internal.Language
}

// Name returns the track's name/label.
func (t *Track) Name() string {
	return t.internal.Name
}

// IsVideo returns true if this is a video track.
func (t *Track) IsVideo() bool {
	return t.internal.IsVideo()
}

// IsAudio returns true if this is an audio track.
func (t *Track) IsAudio() bool {
	return t.internal.IsAudio()
}

// IsSubtitle returns true if this is a subtitle track.
func (t *Track) IsSubtitle() bool {
	return t.internal.IsSubtitle()
}

// IsEncrypted returns true if the track is encrypted.
func (t *Track) IsEncrypted() bool {
	return t.internal.Encrypted
}

// SegmentCount returns the number of segments in this track.
func (t *Track) SegmentCount() int {
	return len(t.internal.Segments)
}

// ProgressUpdate represents a download progress update.
type ProgressUpdate struct {
	// SegmentIndex is the index of the segment that was processed.
	SegmentIndex int

	// TrackID is the ID of the track this segment belongs to.
	TrackID string

	// BytesLoaded is the number of bytes downloaded for this segment.
	BytesLoaded int64

	// Completed is true if the segment was successfully downloaded.
	Completed bool

	// Error is non-nil if the segment download failed.
	Error error
}