package main

import (
	"fmt"
	"strings"

	"github.com/mohaanymo/veld/internal/engine"
	"github.com/mohaanymo/veld/internal/models"
)

func main() {
	// Create mock tracks
	tracks := []*models.Track{
		// Videos
		{ID: "v1", Type: models.TrackVideo, Codec: "avc1", Bandwidth: 5000000, Resolution: models.Resolution{Width: 1920, Height: 1080}},
		{ID: "v2", Type: models.TrackVideo, Codec: "avc1", Bandwidth: 2500000, Resolution: models.Resolution{Width: 1280, Height: 720}},
		{ID: "v3", Type: models.TrackVideo, Codec: "avc1", Bandwidth: 1500000, Resolution: models.Resolution{Width: 1200, Height: 690}},
		{ID: "v4", Type: models.TrackVideo, Codec: "hevc", Bandwidth: 3000000, Resolution: models.Resolution{Width: 3840, Height: 2160}},

		// Audios
		{ID: "a1", Type: models.TrackAudio, Codec: "mp4a", Bandwidth: 256000, Language: "en", Name: "English"},
		{ID: "a2", Type: models.TrackAudio, Codec: "mp4a", Bandwidth: 128000, Language: "eng", Name: "English (alt)"},
		{ID: "a3", Type: models.TrackAudio, Codec: "aac", Bandwidth: 192000, Language: "tr", Name: "Turkish"},
		{ID: "a4", Type: models.TrackAudio, Codec: "ac-3", Bandwidth: 64000, Language: "und", Name: "Unknown"},

		// Subtitles
		{ID: "s1", Type: models.TrackSubtitle, Codec: "wvtt", Language: "ar", Name: "Arabic (explanation)"},
		{ID: "s2", Type: models.TrackSubtitle, Codec: "wvtt", Language: "ara", Name: "Arabic (plain)"},
		{ID: "s3", Type: models.TrackSubtitle, Codec: "wvtt", Language: "en", Name: "English"},
		{ID: "s4", Type: models.TrackSubtitle, Codec: "wvtt", Language: "und", Name: "Unknown"},
	}

	tests := []struct {
		selector string
		desc     string
	}{
		// Legacy selectors
		{"best", "Best video + audio"},
		{"all-video", "All videos"},
		{"all-audio", "All audios"},

		// Resolution ranges
		{"v:-720p", "Video up to 720p (should get 690p)"},
		{"v:-1080p", "Video up to 1080p (should get 1080p)"},
		{"v:720p-1080p", "Video between 720p-1080p"},

		// Language selection
		{"a:en", "English audio (matches 'en')"},
		{"a:eng", "English audio (matches 'eng' → normalized to 'en')"},
		{"a:en,ar", "English + Arabic audio (ar not found → just en)"},
		{"a:en,tr", "English + Turkish audio"},

		// Language with fallback
		{"a:ar,ja", "Arabic + Japanese (neither exists → fallback to best)"},
		{"a:ar!,ja!", "Arabic + Japanese REQUIRED (should error)"},

		// Include undefined
		{"a:en,?", "English + undefined tracks"},
		{"s:ar,?", "Arabic subs + undefined subs"},

		// Select all matching
		{"s:ar*", "All Arabic subtitles"},
		{"a:en*", "All English audio (both en and eng tracks)"},

		// Bandwidth filtering
		{"a:en[>100k]", "English audio > 100kbps"},
		{"a:en[<200k]", "English audio < 200kbps"},
		{"a:[128k-256k]", "Audio between 128k-256k"},

		// Combined
		{"v:-720p + a:en,ar + s:ar", "Your example: 720p video, en/ar audio, ar subs"},
		{"v:-1080p[>2M] + a:en[>100k]", "1080p video >2Mbps, en audio >100kbps"},
	}

	fmt.Println("=== Enhanced Track Selector Tests ===")
	fmt.Println()

	for _, test := range tests {
		fmt.Printf("Selector: %s\n", test.selector)
		fmt.Printf("Purpose:  %s\n", test.desc)

		selected, err := engine.SelectTracks(tracks, test.selector)

		if err != nil {
			fmt.Printf("Error:    %v\n", err)
		}

		if len(selected) > 0 {
			fmt.Print("Selected: ")
			var parts []string
			for _, t := range selected {
				info := t.ID
				if t.Resolution.Height > 0 {
					info += fmt.Sprintf(" (%dp)", t.Resolution.Height)
				}
				if t.Language != "" {
					info += fmt.Sprintf(" [%s]", t.Language)
				}
				if t.Bandwidth > 0 {
					info += fmt.Sprintf(" @%dk", t.Bandwidth/1000)
				}
				parts = append(parts, info)
			}
			fmt.Println(strings.Join(parts, ", "))
		}
		fmt.Println(strings.Repeat("-", 60))
	}
}
