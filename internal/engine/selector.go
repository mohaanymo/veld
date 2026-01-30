package engine

import (
	"fmt"
	"sort"
	"strings"

	"veld/internal/models"
)

// TrackSelector provides smart track selection from a list of tracks.
type TrackSelector struct {
	Videos    []*models.Track
	Audios    []*models.Track
	Subtitles []*models.Track
}

// NewTrackSelector categorizes tracks by type.
func NewTrackSelector(tracks []*models.Track) *TrackSelector {
	ts := &TrackSelector{}

	for _, t := range tracks {
		switch {
		case t.IsSubtitle():
			ts.Subtitles = append(ts.Subtitles, t)
		case t.IsAudio():
			ts.Audios = append(ts.Audios, t)
		case t.IsVideo():
			ts.Videos = append(ts.Videos, t)
		default:
			// Fallback based on TrackType field
			switch t.Type {
			case models.TrackAudio:
				ts.Audios = append(ts.Audios, t)
			case models.TrackSubtitle:
				ts.Subtitles = append(ts.Subtitles, t)
			default:
				ts.Videos = append(ts.Videos, t)
			}
		}
	}

	// Sort by bandwidth (highest first)
	sortByBandwidth := func(tracks []*models.Track) {
		sort.Slice(tracks, func(i, j int) bool {
			return tracks[i].Bandwidth > tracks[j].Bandwidth
		})
	}
	sortByBandwidth(ts.Videos)
	sortByBandwidth(ts.Audios)

	return ts
}

// Select selects tracks based on selector string.
func (ts *TrackSelector) Select(selector string) []*models.Track {
	selector = strings.ToLower(strings.TrimSpace(selector))
	var selected []*models.Track

	switch selector {
	case "all":
		selected = append(selected, ts.Videos...)
		selected = append(selected, ts.Audios...)
		selected = append(selected, ts.Subtitles...)
		return selected

	case "all-video":
		return ts.Videos

	case "all-audio":
		return ts.Audios

	case "all-subs", "all-subtitles":
		return ts.Subtitles

	case "best", "bv+ba", "best-video+best-audio":
		if len(ts.Videos) > 0 {
			selected = append(selected, ts.Videos[0])
		}
		if len(ts.Audios) > 0 {
			selected = append(selected, ts.Audios[0])
		}
		return selected

	case "best-video", "bv":
		if len(ts.Videos) > 0 {
			return []*models.Track{ts.Videos[0]}
		}
		return nil

	case "best-audio", "ba":
		if len(ts.Audios) > 0 {
			return []*models.Track{ts.Audios[0]}
		}
		return nil
	}

	// Parse complex selectors: "1080p+ba", "720p+aac", "video:0+audio:1"
	parts := strings.Split(selector, "+")
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}

		switch part {
		case "bv", "best-video":
			if len(ts.Videos) > 0 {
				selected = append(selected, ts.Videos[0])
			}
		case "ba", "best-audio":
			if len(ts.Audios) > 0 {
				selected = append(selected, ts.Audios[0])
			}
		default:
			// Handle index-based selection: "video:0", "audio:1"
			if strings.Contains(part, ":") {
				track := ts.selectByIndex(part)
				if track != nil {
					selected = append(selected, track)
				}
				continue
			}

			// Handle resolution-based selection: "1080p", "720p", "4k"
			if isResolutionSelector(part) {
				track := ts.findByResolution(part)
				if track != nil {
					selected = append(selected, track)
				}
				continue
			}

			// Handle language-based selection for audio: "en", "ar", "ja"
			if len(part) == 2 || len(part) == 3 {
				for _, t := range ts.Audios {
					if strings.EqualFold(t.Language, part) {
						selected = append(selected, t)
						break
					}
				}
				continue
			}

			// Handle codec-based selection: "aac", "h264", "hevc"
			track := ts.findByCodec(part)
			if track != nil {
				selected = append(selected, track)
			}
		}
	}

	// Fallback if nothing selected
	if len(selected) == 0 {
		if len(ts.Videos) > 0 {
			selected = append(selected, ts.Videos[0])
		}
		if len(ts.Audios) > 0 {
			selected = append(selected, ts.Audios[0])
		}
	}

	// Auto-add best audio if only video was selected
	hasVideo := false
	hasAudio := false
	for _, t := range selected {
		if t.IsVideo() {
			hasVideo = true
		}
		if t.IsAudio() {
			hasAudio = true
		}
	}
	if hasVideo && !hasAudio && len(ts.Audios) > 0 {
		selected = append(selected, ts.Audios[0])
	}

	return selected
}

func (ts *TrackSelector) selectByIndex(part string) *models.Track {
	kv := strings.SplitN(part, ":", 2)
	if len(kv) != 2 {
		return nil
	}

	typ, idxStr := kv[0], kv[1]
	var idx int
	fmt.Sscanf(idxStr, "%d", &idx)

	switch typ {
	case "video", "v":
		if idx < len(ts.Videos) {
			return ts.Videos[idx]
		}
	case "audio", "a":
		if idx < len(ts.Audios) {
			return ts.Audios[idx]
		}
	case "subtitle", "sub", "s":
		if idx < len(ts.Subtitles) {
			return ts.Subtitles[idx]
		}
	}
	return nil
}

func isResolutionSelector(s string) bool {
	s = strings.ToLower(s)
	return strings.HasSuffix(s, "p") || s == "4k" || s == "2k" ||
		s == "hd" || s == "fhd" || s == "sd"
}

func (ts *TrackSelector) findByResolution(res string) *models.Track {
	res = strings.ToLower(res)
	targetHeight := 0

	switch res {
	case "4k", "2160p":
		targetHeight = 2160
	case "1440p", "2k":
		targetHeight = 1440
	case "1080p", "fhd":
		targetHeight = 1080
	case "720p", "hd":
		targetHeight = 720
	case "480p", "sd":
		targetHeight = 480
	case "360p":
		targetHeight = 360
	case "240p":
		targetHeight = 240
	case "144p":
		targetHeight = 144
	default:
		fmt.Sscanf(res, "%dp", &targetHeight)
	}

	// Find closest match
	var best *models.Track
	bestDiff := int(^uint(0) >> 1) // Max int

	for _, t := range ts.Videos {
		diff := abs(t.Resolution.Height - targetHeight)
		if diff < bestDiff {
			bestDiff = diff
			best = t
		}
	}
	return best
}

func (ts *TrackSelector) findByCodec(codec string) *models.Track {
	codec = strings.ToLower(codec)

	// Check audio first
	for _, t := range ts.Audios {
		if strings.Contains(strings.ToLower(t.Codec), codec) {
			return t
		}
	}
	// Check video
	for _, t := range ts.Videos {
		if strings.Contains(strings.ToLower(t.Codec), codec) {
			return t
		}
	}
	return nil
}

// SelectTracks is the main entry point for track selection.
func SelectTracks(tracks []*models.Track, selector string) ([]*models.Track, error) {
	if len(tracks) == 0 {
		return nil, fmt.Errorf("no tracks available")
	}

	ts := NewTrackSelector(tracks)
	selected := ts.Select(selector)

	if len(selected) == 0 {
		return nil, fmt.Errorf("no tracks matched selector: %s", selector)
	}

	return selected, nil
}

func abs(x int) int {
	if x < 0 {
		return -x
	}
	return x
}
