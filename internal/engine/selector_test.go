package engine

import (
	"testing"

	"github.com/mohaanymo/veld/internal/models"
)

// createTestTracks creates a standard set of tracks for testing
func createTestTracks() []*models.Track {
	return []*models.Track{
		// Videos (sorted by bandwidth desc)
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
}

func TestNormalizeLanguage(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"en", "en"},
		{"eng", "en"},
		{"english", "en"},
		{"ar", "ar"},
		{"ara", "ar"},
		{"arb", "ar"},
		{"arabic", "ar"},
		{"ja", "ja"},
		{"jpn", "ja"},
		{"japanese", "ja"},
		{"unknown", "unknown"}, // passthrough
		{"", ""},
	}

	for _, tt := range tests {
		result := normalizeLanguage(tt.input)
		if result != tt.expected {
			t.Errorf("normalizeLanguage(%q) = %q, want %q", tt.input, result, tt.expected)
		}
	}
}

func TestLanguageMatches(t *testing.T) {
	tests := []struct {
		trackLang string
		wantLang  string
		expected  bool
	}{
		{"en", "en", true},
		{"eng", "en", true},
		{"en", "eng", true},
		{"english", "en", true},
		{"ar", "ara", true},
		{"arb", "ar", true},
		{"en", "ar", false},
		{"tr", "en", false},
	}

	for _, tt := range tests {
		result := languageMatches(tt.trackLang, tt.wantLang)
		if result != tt.expected {
			t.Errorf("languageMatches(%q, %q) = %v, want %v", tt.trackLang, tt.wantLang, result, tt.expected)
		}
	}
}

func TestParseResolution(t *testing.T) {
	tests := []struct {
		input    string
		expected int
	}{
		{"4k", 2160},
		{"2160p", 2160},
		{"1080p", 1080},
		{"fhd", 1080},
		{"720p", 720},
		{"hd", 720},
		{"480p", 480},
		{"sd", 480},
		{"360p", 360},
		{"690p", 690},
	}

	for _, tt := range tests {
		result := parseResolution(tt.input)
		if result != tt.expected {
			t.Errorf("parseResolution(%q) = %d, want %d", tt.input, result, tt.expected)
		}
	}
}

func TestParseBandwidth(t *testing.T) {
	tests := []struct {
		input    string
		expected int64
	}{
		{"128k", 128000},
		{"256k", 256000},
		{"2M", 2000000},
		{"5M", 5000000},
		{"1G", 1000000000},
		{"128000", 128000},
	}

	for _, tt := range tests {
		result := parseBandwidth(tt.input)
		if result != tt.expected {
			t.Errorf("parseBandwidth(%q) = %d, want %d", tt.input, result, tt.expected)
		}
	}
}

func TestParseBandwidthRange(t *testing.T) {
	tests := []struct {
		input       string
		expectedMin int64
		expectedMax int64
	}{
		{">128k", 128000, 0},
		{"<256k", 0, 256000},
		{"128k-256k", 128000, 256000},
		{"1M-5M", 1000000, 5000000},
	}

	for _, tt := range tests {
		min, max := parseBandwidthRange(tt.input)
		if min != tt.expectedMin || max != tt.expectedMax {
			t.Errorf("parseBandwidthRange(%q) = (%d, %d), want (%d, %d)",
				tt.input, min, max, tt.expectedMin, tt.expectedMax)
		}
	}
}

func TestSelectLegacySelectors(t *testing.T) {
	tracks := createTestTracks()
	ts := NewTrackSelector(tracks)

	tests := []struct {
		selector    string
		expectedIDs []string
	}{
		{"best", []string{"v1", "a1"}},
		{"bv", []string{"v1"}},
		{"ba", []string{"a1"}},
		{"bv+ba", []string{"v1", "a1"}},
	}

	for _, tt := range tests {
		selected, err := ts.Select(tt.selector)
		if err != nil {
			t.Errorf("Select(%q) error: %v", tt.selector, err)
			continue
		}

		ids := extractIDs(selected)
		if !equalSlices(ids, tt.expectedIDs) {
			t.Errorf("Select(%q) = %v, want %v", tt.selector, ids, tt.expectedIDs)
		}
	}
}

func TestSelectResolutionRange(t *testing.T) {
	tracks := createTestTracks()
	ts := NewTrackSelector(tracks)

	tests := []struct {
		selector   string
		expectedID string // expected video ID
	}{
		{"v:-720p", "v2"},  // 720p is best ≤720p
		{"v:-1080p", "v1"}, // 1080p is best ≤1080p
		{"v:-690p", "v3"},  // 690p exactly
		{"v:720p", "v2"},   // closest to 720p
		{"v:1080p", "v1"},  // closest to 1080p
	}

	for _, tt := range tests {
		selected, err := ts.Select(tt.selector)
		if err != nil {
			t.Errorf("Select(%q) error: %v", tt.selector, err)
			continue
		}

		// Find video in selected
		var videoID string
		for _, track := range selected {
			if track.IsVideo() {
				videoID = track.ID
				break
			}
		}

		if videoID != tt.expectedID {
			t.Errorf("Select(%q) video = %q, want %q", tt.selector, videoID, tt.expectedID)
		}
	}
}

func TestSelectLanguage(t *testing.T) {
	tracks := createTestTracks()
	ts := NewTrackSelector(tracks)

	tests := []struct {
		selector    string
		expectedIDs []string
	}{
		{"a:en", []string{"a1"}},
		{"a:eng", []string{"a1"}},         // normalized to en, matches a1
		{"a:en,tr", []string{"a1", "a3"}}, // both exist
		{"a:en,ar", []string{"a1"}},       // ar doesn't exist, just en
		{"a:ar,ja", []string{"a1"}},       // neither exists, fallback to best
	}

	for _, tt := range tests {
		selected, err := ts.Select(tt.selector)
		if err != nil {
			t.Errorf("Select(%q) error: %v", tt.selector, err)
			continue
		}

		ids := extractIDs(selected)
		if !equalSlices(ids, tt.expectedIDs) {
			t.Errorf("Select(%q) = %v, want %v", tt.selector, ids, tt.expectedIDs)
		}
	}
}

func TestSelectRequiredModifier(t *testing.T) {
	tracks := createTestTracks()
	ts := NewTrackSelector(tracks)

	// Required language that doesn't exist should error
	_, err := ts.Select("a:ar!,ja!")
	if err == nil {
		t.Error("Select(a:ar!,ja!) should error when required languages don't exist")
	}

	// Required language that exists should work
	selected, err := ts.Select("a:en!")
	if err != nil {
		t.Errorf("Select(a:en!) error: %v", err)
	}
	if len(selected) == 0 || selected[0].ID != "a1" {
		t.Errorf("Select(a:en!) should select a1")
	}
}

func TestSelectUndefinedModifier(t *testing.T) {
	tracks := createTestTracks()
	ts := NewTrackSelector(tracks)

	// Include undefined tracks with ?
	selected, err := ts.Select("a:en,?")
	if err != nil {
		t.Errorf("Select(a:en,?) error: %v", err)
	}

	ids := extractIDs(selected)
	if !contains(ids, "a1") || !contains(ids, "a4") {
		t.Errorf("Select(a:en,?) should include en and und tracks, got %v", ids)
	}
}

func TestSelectAllModifier(t *testing.T) {
	tracks := createTestTracks()
	ts := NewTrackSelector(tracks)

	// Select all Arabic subtitles
	selected, err := ts.Select("s:ar*")
	if err != nil {
		t.Errorf("Select(s:ar*) error: %v", err)
	}

	ids := extractIDs(selected)
	if !contains(ids, "s1") || !contains(ids, "s2") {
		t.Errorf("Select(s:ar*) should include both ar subs, got %v", ids)
	}

	// Select all English audio (matches both en and eng)
	selected, err = ts.Select("a:en*")
	if err != nil {
		t.Errorf("Select(a:en*) error: %v", err)
	}

	ids = extractIDs(selected)
	if !contains(ids, "a1") || !contains(ids, "a2") {
		t.Errorf("Select(a:en*) should include both en tracks, got %v", ids)
	}
}

func TestSelectBandwidthFilter(t *testing.T) {
	tracks := createTestTracks()
	ts := NewTrackSelector(tracks)

	tests := []struct {
		selector   string
		expectedID string
	}{
		{"a:en[>100k]", "a1"}, // 256k > 100k
		{"a:en[<200k]", "a2"}, // 128k < 200k (eng normalized to en)
	}

	for _, tt := range tests {
		selected, err := ts.Select(tt.selector)
		if err != nil {
			t.Errorf("Select(%q) error: %v", tt.selector, err)
			continue
		}

		// Find audio in selected
		var audioID string
		for _, track := range selected {
			if track.IsAudio() {
				audioID = track.ID
				break
			}
		}

		if audioID != tt.expectedID {
			t.Errorf("Select(%q) audio = %q, want %q", tt.selector, audioID, tt.expectedID)
		}
	}
}

func TestSelectCombined(t *testing.T) {
	tracks := createTestTracks()
	ts := NewTrackSelector(tracks)

	// Combined selector: video + audio + subtitle
	selected, err := ts.Select("v:-720p + a:en,ar + s:ar")
	if err != nil {
		t.Errorf("Select combined error: %v", err)
	}

	ids := extractIDs(selected)

	// Should have: v2 (720p), a1 (en), s1 (ar)
	if !contains(ids, "v2") {
		t.Errorf("Combined should include v2 (720p video), got %v", ids)
	}
	if !contains(ids, "a1") {
		t.Errorf("Combined should include a1 (en audio), got %v", ids)
	}
	if !contains(ids, "s1") {
		t.Errorf("Combined should include s1 (ar subtitle), got %v", ids)
	}
}

func TestSelectTracksEntrypoint(t *testing.T) {
	tracks := createTestTracks()

	// Test main entrypoint
	selected, err := SelectTracks(tracks, "best")
	if err != nil {
		t.Errorf("SelectTracks error: %v", err)
	}
	if len(selected) < 2 {
		t.Errorf("SelectTracks(best) should return at least 2 tracks")
	}

	// Empty tracks should error
	_, err = SelectTracks(nil, "best")
	if err == nil {
		t.Error("SelectTracks with nil tracks should error")
	}
}

// Helper functions

func extractIDs(tracks []*models.Track) []string {
	ids := make([]string, len(tracks))
	for i, t := range tracks {
		ids[i] = t.ID
	}
	return ids
}

func equalSlices(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func contains(slice []string, item string) bool {
	for _, s := range slice {
		if s == item {
			return true
		}
	}
	return false
}
