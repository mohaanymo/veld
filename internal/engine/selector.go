package engine

import (
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/mohaanymo/veld/internal/models"
)

// Language aliases for normalization (ISO 639-1 ←→ ISO 639-2/B)
var langAliases = map[string]string{
	// English
	"eng": "en", "english": "en",
	// Arabic
	"ara": "ar", "arb": "ar", "arabic": "ar",
	// Japanese
	"jpn": "ja", "japanese": "ja",
	// Chinese
	"zho": "zh", "chi": "zh", "chinese": "zh", "cmn": "zh",
	// Spanish
	"spa": "es", "spanish": "es",
	// French
	"fra": "fr", "fre": "fr", "french": "fr",
	// German
	"deu": "de", "ger": "de", "german": "de",
	// Portuguese
	"por": "pt", "portuguese": "pt",
	// Russian
	"rus": "ru", "russian": "ru",
	// Korean
	"kor": "ko", "korean": "ko",
	// Italian
	"ita": "it", "italian": "it",
	// Turkish
	"tur": "tr", "turkish": "tr",
	// Hindi
	"hin": "hi", "hindi": "hi",
	// Dutch
	"nld": "nl", "dut": "nl", "dutch": "nl",
	// Polish
	"pol": "pl", "polish": "pl",
	// Vietnamese
	"vie": "vi", "vietnamese": "vi",
	// Thai
	"tha": "th", "thai": "th",
	// Indonesian
	"ind": "id", "indonesian": "id",
	// Hebrew
	"heb": "he", "hebrew": "he",
	// Greek
	"ell": "el", "gre": "el", "greek": "el",
	// Czech
	"ces": "cs", "cze": "cs", "czech": "cs",
	// Romanian
	"ron": "ro", "rum": "ro", "romanian": "ro",
	// Hungarian
	"hun": "hu", "hungarian": "hu",
	// Swedish
	"swe": "sv", "swedish": "sv",
	// Danish
	"dan": "da", "danish": "da",
	// Finnish
	"fin": "fi", "finnish": "fi",
	// Norwegian
	"nor": "no", "norwegian": "no", "nob": "no", "nno": "no",
	// Ukrainian
	"ukr": "uk", "ukrainian": "uk",
	// Malay
	"msa": "ms", "may": "ms", "malay": "ms",
	// Filipino/Tagalog
	"fil": "tl", "tgl": "tl", "tagalog": "tl", "filipino": "tl",
	// Persian/Farsi
	"fas": "fa", "per": "fa", "persian": "fa", "farsi": "fa",
}

// normalizeLanguage converts language codes to a normalized form.
func normalizeLanguage(lang string) string {
	lang = strings.ToLower(strings.TrimSpace(lang))
	if normalized, ok := langAliases[lang]; ok {
		return normalized
	}
	return lang
}

// languageMatches checks if two language codes are equivalent.
func languageMatches(trackLang, wantLang string) bool {
	return normalizeLanguage(trackLang) == normalizeLanguage(wantLang)
}

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

// trackExpr represents a parsed track expression like "a:en,ar!" or "v:-1080p[>2M]"
type trackExpr struct {
	trackType    string   // "v", "a", "s" or empty
	values       []string // languages, resolutions, etc.
	required     bool     // ! modifier - fail if not found
	includeUnd   bool     // ? modifier - include undefined tracks
	selectAll    bool     // * modifier - select all matching
	resMin       int      // resolution range min (0 = no min)
	resMax       int      // resolution range max (0 = no max)
	resUpTo      bool     // -720p syntax (select best up to)
	bwMin        int64    // bandwidth range min (0 = no min)
	bwMax        int64    // bandwidth range max (0 = no max)
}

// parseExpression parses a track expression like "a:en,ar!" or "v:-1080p[128k-256k]"
func parseExpression(expr string) *trackExpr {
	te := &trackExpr{}
	expr = strings.TrimSpace(expr)

	// Check for track type prefix
	if len(expr) >= 2 && expr[1] == ':' {
		te.trackType = strings.ToLower(string(expr[0]))
		expr = expr[2:]
	}

	// Check for bandwidth range [...]
	if idx := strings.Index(expr, "["); idx != -1 {
		endIdx := strings.Index(expr, "]")
		if endIdx > idx {
			bwPart := expr[idx+1 : endIdx]
			te.bwMin, te.bwMax = parseBandwidthRange(bwPart)
			expr = expr[:idx] + expr[endIdx+1:]
		}
	}

	// Check for modifiers at end
	for len(expr) > 0 {
		lastChar := expr[len(expr)-1]
		switch lastChar {
		case '!':
			te.required = true
			expr = expr[:len(expr)-1]
		case '?':
			te.includeUnd = true
			expr = expr[:len(expr)-1]
		case '*':
			te.selectAll = true
			expr = expr[:len(expr)-1]
		default:
			goto doneModifiers
		}
	}
doneModifiers:

	// Check for resolution range
	if strings.HasPrefix(expr, "-") && len(expr) > 1 {
		// -1080p syntax
		te.resUpTo = true
		te.resMax = parseResolution(expr[1:])
		return te
	}

	if strings.Contains(expr, "-") && !strings.HasPrefix(expr, "-") {
		// 720p-1080p syntax
		parts := strings.SplitN(expr, "-", 2)
		if len(parts) == 2 {
			te.resMin = parseResolution(parts[0])
			te.resMax = parseResolution(parts[1])
			return te
		}
	}

	// Parse comma-separated values
	if expr != "" {
		te.values = strings.Split(expr, ",")
		for i := range te.values {
			te.values[i] = strings.TrimSpace(te.values[i])
		}
	}

	return te
}

// parseResolution converts resolution strings to height in pixels
func parseResolution(s string) int {
	s = strings.ToLower(strings.TrimSpace(s))
	switch s {
	case "4k", "2160p", "uhd":
		return 2160
	case "1440p", "2k", "qhd":
		return 1440
	case "1080p", "fhd":
		return 1080
	case "720p", "hd":
		return 720
	case "480p", "sd":
		return 480
	case "360p":
		return 360
	case "240p":
		return 240
	case "144p":
		return 144
	default:
		var height int
		fmt.Sscanf(s, "%dp", &height)
		return height
	}
}

// parseBandwidthRange parses bandwidth expressions like "128k", ">128k", "128k-256k", "<5M"
func parseBandwidthRange(s string) (min, max int64) {
	s = strings.TrimSpace(s)

	// Handle comparison operators
	if strings.HasPrefix(s, ">") {
		min = parseBandwidth(s[1:])
		return min, 0
	}
	if strings.HasPrefix(s, "<") {
		max = parseBandwidth(s[1:])
		return 0, max
	}
	if strings.HasPrefix(s, ">=") {
		min = parseBandwidth(s[2:])
		return min, 0
	}
	if strings.HasPrefix(s, "<=") {
		max = parseBandwidth(s[2:])
		return 0, max
	}

	// Handle range
	if strings.Contains(s, "-") {
		parts := strings.SplitN(s, "-", 2)
		if len(parts) == 2 {
			return parseBandwidth(parts[0]), parseBandwidth(parts[1])
		}
	}

	// Single value - match closest
	bw := parseBandwidth(s)
	return bw, bw
}

// parseBandwidth converts bandwidth strings like "128k", "2M", "5000000" to bits per second
func parseBandwidth(s string) int64 {
	s = strings.ToLower(strings.TrimSpace(s))
	if s == "" {
		return 0
	}

	multiplier := int64(1)
	if strings.HasSuffix(s, "k") {
		multiplier = 1000
		s = s[:len(s)-1]
	} else if strings.HasSuffix(s, "m") {
		multiplier = 1000000
		s = s[:len(s)-1]
	} else if strings.HasSuffix(s, "g") {
		multiplier = 1000000000
		s = s[:len(s)-1]
	}

	val, _ := strconv.ParseInt(s, 10, 64)
	return val * multiplier
}

// matchesBandwidth checks if a track's bandwidth matches the range
func matchesBandwidth(bandwidth, min, max int64) bool {
	if min == 0 && max == 0 {
		return true // no bandwidth filter
	}
	if min > 0 && bandwidth < min {
		return false
	}
	if max > 0 && bandwidth > max {
		return false
	}
	return true
}

// Select selects tracks based on selector string.
func (ts *TrackSelector) Select(selector string) ([]*models.Track, error) {
	selector = strings.TrimSpace(selector)
	if selector == "" {
		selector = "best"
	}

	// Handle legacy simple selectors
	selectorLower := strings.ToLower(selector)
	switch selectorLower {
	case "all":
		var all []*models.Track
		all = append(all, ts.Videos...)
		all = append(all, ts.Audios...)
		all = append(all, ts.Subtitles...)
		return all, nil

	case "all-video":
		return ts.Videos, nil

	case "all-audio":
		return ts.Audios, nil

	case "all-subs", "all-subtitles":
		return ts.Subtitles, nil

	case "best", "bv+ba", "best-video+best-audio":
		var selected []*models.Track
		if len(ts.Videos) > 0 {
			selected = append(selected, ts.Videos[0])
		}
		if len(ts.Audios) > 0 {
			selected = append(selected, ts.Audios[0])
		}
		return selected, nil

	case "best-video", "bv":
		if len(ts.Videos) > 0 {
			return []*models.Track{ts.Videos[0]}, nil
		}
		return nil, nil

	case "best-audio", "ba":
		if len(ts.Audios) > 0 {
			return []*models.Track{ts.Audios[0]}, nil
		}
		return nil, nil
	}

	// Parse complex selectors: "v:-720p + a:en,ar + s:ar"
	var selected []*models.Track
	var errors []string

	parts := splitExpressions(selector)
	for _, part := range parts {
		expr := parseExpression(part)
		tracks, err := ts.selectByExpression(expr)
		if err != nil {
			if expr.required {
				errors = append(errors, err.Error())
			}
			// Non-required: silently skip
		} else {
			selected = append(selected, tracks...)
		}
	}

	if len(errors) > 0 {
		return selected, fmt.Errorf("required tracks not found: %s", strings.Join(errors, "; "))
	}

	// If nothing selected, fall back to best
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

	return selected, nil
}

// splitExpressions splits "v:720p + a:en,ar" into ["v:720p", "a:en,ar"]
func splitExpressions(selector string) []string {
	// Handle + separator while being careful about bandwidth ranges
	var parts []string
	var current strings.Builder
	inBracket := false

	for _, ch := range selector {
		switch ch {
		case '[':
			inBracket = true
			current.WriteRune(ch)
		case ']':
			inBracket = false
			current.WriteRune(ch)
		case '+':
			if !inBracket {
				if s := strings.TrimSpace(current.String()); s != "" {
					parts = append(parts, s)
				}
				current.Reset()
			} else {
				current.WriteRune(ch)
			}
		default:
			current.WriteRune(ch)
		}
	}
	if s := strings.TrimSpace(current.String()); s != "" {
		parts = append(parts, s)
	}

	return parts
}

// selectByExpression selects tracks based on a parsed expression
func (ts *TrackSelector) selectByExpression(expr *trackExpr) ([]*models.Track, error) {
	var pool []*models.Track

	// Determine track pool based on type
	switch expr.trackType {
	case "v", "video":
		pool = ts.Videos
	case "a", "audio":
		pool = ts.Audios
	case "s", "sub", "subtitle":
		pool = ts.Subtitles
	default:
		// Auto-detect based on expression content
		if expr.resMax > 0 || expr.resMin > 0 || expr.resUpTo {
			pool = ts.Videos
		} else if len(expr.values) > 0 {
			// Check if it's a resolution
			if isResolutionSelector(expr.values[0]) {
				pool = ts.Videos
			} else {
				// Assume audio for language codes
				pool = ts.Audios
			}
		} else {
			pool = ts.Videos
		}
	}

	if len(pool) == 0 {
		return nil, fmt.Errorf("no %s tracks available", expr.trackType)
	}

	var selected []*models.Track

	// Handle resolution range selection
	if expr.resUpTo || expr.resMax > 0 || expr.resMin > 0 {
		selected = ts.selectByResolutionRange(pool, expr)
		if len(selected) == 0 && len(pool) > 0 {
			// Fallback to best available
			selected = []*models.Track{pool[0]}
		}
	} else if len(expr.values) > 0 {
		// Handle value-based selection (languages, resolutions, codecs)
		selected = ts.selectByValues(pool, expr)
	} else {
		// No specific filter, apply bandwidth filter if any
		for _, t := range pool {
			if matchesBandwidth(t.Bandwidth, expr.bwMin, expr.bwMax) {
				selected = append(selected, t)
				if !expr.selectAll {
					break
				}
			}
		}
		if len(selected) == 0 && len(pool) > 0 {
			selected = []*models.Track{pool[0]}
		}
	}

	// Include undefined tracks if ? modifier
	if expr.includeUnd {
		for _, t := range pool {
			lang := normalizeLanguage(t.Language)
			if lang == "" || lang == "und" || lang == "undefined" || lang == "unknown" {
				if !containsTrack(selected, t) {
					selected = append(selected, t)
				}
			}
		}
	}

	if len(selected) == 0 {
		return nil, fmt.Errorf("no matching tracks for %v", expr.values)
	}

	return selected, nil
}

// selectByResolutionRange selects videos within a resolution range
func (ts *TrackSelector) selectByResolutionRange(pool []*models.Track, expr *trackExpr) []*models.Track {
	var candidates []*models.Track

	for _, t := range pool {
		height := t.Resolution.Height

		// Check resolution constraints
		if expr.resUpTo && height > expr.resMax {
			continue
		}
		if expr.resMin > 0 && height < expr.resMin {
			continue
		}
		if expr.resMax > 0 && !expr.resUpTo && height > expr.resMax {
			continue
		}

		// Check bandwidth constraints
		if !matchesBandwidth(t.Bandwidth, expr.bwMin, expr.bwMax) {
			continue
		}

		candidates = append(candidates, t)
	}

	if len(candidates) == 0 {
		return nil
	}

	// Sort by height descending (best quality first within range)
	sort.Slice(candidates, func(i, j int) bool {
		return candidates[i].Resolution.Height > candidates[j].Resolution.Height
	})

	if expr.selectAll {
		return candidates
	}
	return []*models.Track{candidates[0]}
}

// selectByValues selects tracks by language, resolution, or codec values
func (ts *TrackSelector) selectByValues(pool []*models.Track, expr *trackExpr) []*models.Track {
	var selected []*models.Track
	usedTracks := make(map[*models.Track]bool)

	for _, val := range expr.values {
		val = strings.TrimSpace(val)
		if val == "" {
			continue
		}

		// Check if it's a resolution
		if isResolutionSelector(val) {
			track := ts.findClosestResolution(pool, parseResolution(val), expr.bwMin, expr.bwMax)
			if track != nil && !usedTracks[track] {
				selected = append(selected, track)
				usedTracks[track] = true
				if !expr.selectAll {
					continue
				}
			}
			continue
		}

		// Check if it's a codec
		if isCodecSelector(val) {
			for _, t := range pool {
				if strings.Contains(strings.ToLower(t.Codec), strings.ToLower(val)) {
					if matchesBandwidth(t.Bandwidth, expr.bwMin, expr.bwMax) && !usedTracks[t] {
						selected = append(selected, t)
						usedTracks[t] = true
						if !expr.selectAll {
							break
						}
					}
				}
			}
			continue
		}

		// Assume it's a language code
		for _, t := range pool {
			if languageMatches(t.Language, val) {
				if matchesBandwidth(t.Bandwidth, expr.bwMin, expr.bwMax) && !usedTracks[t] {
					selected = append(selected, t)
					usedTracks[t] = true
					if !expr.selectAll {
						break // Only first match per language
					}
				}
			}
		}
	}

	// Fallback: if nothing matched and not required, pick best available
	if len(selected) == 0 && !expr.required && len(pool) > 0 {
		for _, t := range pool {
			if matchesBandwidth(t.Bandwidth, expr.bwMin, expr.bwMax) {
				return []*models.Track{t}
			}
		}
		return []*models.Track{pool[0]}
	}

	return selected
}

// findClosestResolution finds the track closest to target resolution
func (ts *TrackSelector) findClosestResolution(pool []*models.Track, target int, bwMin, bwMax int64) *models.Track {
	var best *models.Track
	bestDiff := int(^uint(0) >> 1)

	for _, t := range pool {
		if !matchesBandwidth(t.Bandwidth, bwMin, bwMax) {
			continue
		}
		diff := abs(t.Resolution.Height - target)
		if diff < bestDiff {
			bestDiff = diff
			best = t
		}
	}
	return best
}

// containsTrack checks if a track is already in the slice
func containsTrack(tracks []*models.Track, target *models.Track) bool {
	for _, t := range tracks {
		if t == target {
			return true
		}
	}
	return false
}

// isResolutionSelector checks if string is a resolution like "720p"
func isResolutionSelector(s string) bool {
	s = strings.ToLower(s)
	if match, _ := regexp.MatchString(`^\d+p$`, s); match {
		return true
	}
	return s == "4k" || s == "2k" || s == "hd" || s == "fhd" || s == "sd" || s == "uhd" || s == "qhd"
}

// isCodecSelector checks if string is a codec like "aac", "hevc"
func isCodecSelector(s string) bool {
	codecs := []string{"aac", "mp4a", "ac3", "ec3", "opus", "vorbis", "flac", "mp3",
		"h264", "avc", "hevc", "h265", "hvc1", "vp9", "vp8", "av1"}
	s = strings.ToLower(s)
	for _, c := range codecs {
		if s == c {
			return true
		}
	}
	return false
}

// SelectTracks is the main entry point for track selection.
func SelectTracks(tracks []*models.Track, selector string) ([]*models.Track, error) {
	if len(tracks) == 0 {
		return nil, fmt.Errorf("no tracks available")
	}

	ts := NewTrackSelector(tracks)
	selected, err := ts.Select(selector)

	if len(selected) == 0 && err == nil {
		return nil, fmt.Errorf("no tracks matched selector: %s", selector)
	}

	return selected, err
}

func abs(x int) int {
	if x < 0 {
		return -x
	}
	return x
}
