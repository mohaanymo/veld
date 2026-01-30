package parser

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/mohaanymo/veld/internal/models"
)

// HLSParser parses HLS (m3u8) manifests.
type HLSParser struct {
	client *http.Client
}

// NewHLSParser creates a new HLS parser.
func NewHLSParser() *HLSParser {
	return &HLSParser{
		client: &http.Client{Timeout: 30 * time.Second},
	}
}

// CanParse checks if URL is an HLS manifest.
func (p *HLSParser) CanParse(urlStr string) bool {
	lower := strings.ToLower(urlStr)
	return strings.Contains(lower, ".m3u8") || strings.Contains(lower, "format=m3u8")
}

// Parse parses an HLS manifest.
func (p *HLSParser) Parse(ctx context.Context, urlStr string, headers map[string]string) (*models.Manifest, error) {
	content, err := p.fetch(ctx, urlStr, headers)
	if err != nil {
		return nil, fmt.Errorf("fetch manifest: %w", err)
	}

	baseURL, _ := url.Parse(urlStr)

	// Check if master or media playlist
	if strings.Contains(content, "#EXT-X-STREAM-INF") {
		return p.parseMaster(ctx, content, baseURL, headers)
	}
	return p.parseMedia(content, baseURL)
}

// parseMaster parses a master playlist.
func (p *HLSParser) parseMaster(ctx context.Context, content string, baseURL *url.URL, headers map[string]string) (*models.Manifest, error) {
	manifest := &models.Manifest{
		URL:  baseURL.String(),
		Type: models.ManifestHLS,
	}

	lines := strings.Split(content, "\n")
	var currentAttrs map[string]string

	for _, line := range lines {
		line = strings.TrimSpace(line)

		switch {
		case strings.HasPrefix(line, "#EXT-X-STREAM-INF:"):
			currentAttrs = parseHLSAttributes(strings.TrimPrefix(line, "#EXT-X-STREAM-INF:"))

		case strings.HasPrefix(line, "#EXT-X-MEDIA:"):
			attrs := parseHLSAttributes(strings.TrimPrefix(line, "#EXT-X-MEDIA:"))
			track, mediaURL := p.parseMediaTrack(attrs, baseURL)
			// Only add tracks that have a URI - tracks without URI are muxed into video variants
			if track != nil && mediaURL != "" {
				track.MediaPlaylistURL = mediaURL
				manifest.Tracks = append(manifest.Tracks, track)
			}

		case !strings.HasPrefix(line, "#") && line != "" && currentAttrs != nil:
			// This is the URI for the previous STREAM-INF
			mediaURL := resolveURL(baseURL, line)
			track := p.parseStreamTrack(currentAttrs, mediaURL)

			// Parse media playlist to get segments
			mediaManifest, err := p.Parse(ctx, mediaURL, headers)
			if err == nil && len(mediaManifest.Tracks) > 0 {
				track.Segments = mediaManifest.Tracks[0].Segments
				track.InitSegment = mediaManifest.Tracks[0].InitSegment
			}

			manifest.Tracks = append(manifest.Tracks, track)
			currentAttrs = nil
		}
	}

	return manifest, nil
}

// parseMedia parses a media playlist.
func (p *HLSParser) parseMedia(content string, baseURL *url.URL) (*models.Manifest, error) {
	manifest := &models.Manifest{
		URL:  baseURL.String(),
		Type: models.ManifestHLS,
	}

	track := &models.Track{
		ID:   "0",
		Type: models.TrackVideo,
	}

	scanner := bufio.NewScanner(strings.NewReader(content))
	var segmentDuration time.Duration
	segmentIndex := 0

	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())

		switch {
		case strings.HasPrefix(line, "#EXTINF:"):
			durStr := strings.TrimPrefix(line, "#EXTINF:")
			durStr = strings.Split(durStr, ",")[0]
			if dur, err := strconv.ParseFloat(durStr, 64); err == nil {
				segmentDuration = time.Duration(dur * float64(time.Second))
			}

		case strings.HasPrefix(line, "#EXT-X-KEY:"):
			attrs := parseHLSAttributes(strings.TrimPrefix(line, "#EXT-X-KEY:"))
			if uri, ok := attrs["URI"]; ok {
				track.Encrypted = true
				track.EncryptionURI = resolveURL(baseURL, strings.Trim(uri, "\""))
			}
			if iv, ok := attrs["IV"]; ok {
				track.EncryptionIV = parseHexBytes(iv)
			}

		case strings.HasPrefix(line, "#EXT-X-MAP:"):
			attrs := parseHLSAttributes(strings.TrimPrefix(line, "#EXT-X-MAP:"))
			if uri, ok := attrs["URI"]; ok {
				track.InitSegment = &models.Segment{
					Index: -1,
					URL:   resolveURL(baseURL, strings.Trim(uri, "\"")),
				}
				if br, ok := attrs["BYTERANGE"]; ok {
					track.InitSegment.ByteRange = parseByteRange(br)
				}
			}

		case !strings.HasPrefix(line, "#") && line != "":
			segment := &models.Segment{
				Index:    segmentIndex,
				URL:      resolveURL(baseURL, line),
				Duration: segmentDuration,
			}
			track.Segments = append(track.Segments, segment)
			manifest.Duration += segmentDuration
			segmentIndex++
		}
	}

	manifest.Tracks = append(manifest.Tracks, track)
	return manifest, nil
}

// parseStreamTrack creates a track from STREAM-INF attributes.
func (p *HLSParser) parseStreamTrack(attrs map[string]string, mediaURL string) *models.Track {
	track := &models.Track{
		Type: models.TrackVideo,
	}

	if bw, ok := attrs["BANDWIDTH"]; ok {
		track.Bandwidth, _ = strconv.ParseInt(bw, 10, 64)
	}

	if res, ok := attrs["RESOLUTION"]; ok {
		parts := strings.Split(res, "x")
		if len(parts) == 2 {
			track.Resolution.Width, _ = strconv.Atoi(parts[0])
			track.Resolution.Height, _ = strconv.Atoi(parts[1])
		}
	}

	if codecs, ok := attrs["CODECS"]; ok {
		track.Codec = strings.Trim(codecs, "\"")
	}

	track.ID = fmt.Sprintf("video_%d_%d", track.Resolution.Height, track.Bandwidth)
	return track
}

// parseMediaTrack creates a track from EXT-X-MEDIA attributes.
func (p *HLSParser) parseMediaTrack(attrs map[string]string, baseURL *url.URL) (*models.Track, string) {
	track := &models.Track{}

	if typ, ok := attrs["TYPE"]; ok {
		switch strings.ToUpper(typ) {
		case "AUDIO":
			track.Type = models.TrackAudio
		case "SUBTITLES", "CLOSED-CAPTIONS":
			track.Type = models.TrackSubtitle
		default:
			track.Type = models.TrackVideo
		}
	}

	if name, ok := attrs["NAME"]; ok {
		track.Name = strings.Trim(name, "\"")
	}

	if lang, ok := attrs["LANGUAGE"]; ok {
		track.Language = strings.Trim(lang, "\"")
	}

	var mediaURL string
	if uri, ok := attrs["URI"]; ok {
		mediaURL = resolveURL(baseURL, strings.Trim(uri, "\""))
	}

	// Build unique ID
	groupID := ""
	if gid, ok := attrs["GROUP-ID"]; ok {
		groupID = strings.Trim(gid, "\"")
	}
	track.ID = fmt.Sprintf("%s_%s_%s", groupID, track.Language, track.Name)
	if track.ID == "__" {
		track.ID = fmt.Sprintf("media_%d", time.Now().UnixNano())
	}

	return track, mediaURL
}

// fetch downloads content from URL.
func (p *HLSParser) fetch(ctx context.Context, urlStr string, headers map[string]string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, urlStr, nil)
	if err != nil {
		return "", err
	}

	for k, v := range headers {
		req.Header.Set(k, v)
	}

	resp, err := p.client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("HTTP %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	return string(body), err
}

// parseHLSAttributes parses HLS attribute string.
func parseHLSAttributes(s string) map[string]string {
	attrs := make(map[string]string)
	re := regexp.MustCompile(`([A-Z0-9-]+)=("[^"]*"|[^,]*)`)
	matches := re.FindAllStringSubmatch(s, -1)
	for _, m := range matches {
		if len(m) >= 3 {
			attrs[m[1]] = m[2]
		}
	}
	return attrs
}

// ParseMediaPlaylist parses an HLS media playlist and returns segments and init segment.
// This is exported for use by the engine for lazy loading audio/subtitle tracks.
func ParseMediaPlaylist(content string, baseURLStr string) ([]*models.Segment, *models.Segment) {
	baseURL, _ := url.Parse(baseURLStr)
	var segments []*models.Segment
	var initSegment *models.Segment

	lines := strings.Split(content, "\n")
	var segmentDuration time.Duration
	segmentIndex := 0

	for _, line := range lines {
		line = strings.TrimSpace(line)

		switch {
		case strings.HasPrefix(line, "#EXTINF:"):
			durStr := strings.TrimPrefix(line, "#EXTINF:")
			durStr = strings.Split(durStr, ",")[0]
			if dur, err := strconv.ParseFloat(durStr, 64); err == nil {
				segmentDuration = time.Duration(dur * float64(time.Second))
			}

		case strings.HasPrefix(line, "#EXT-X-MAP:"):
			attrs := parseHLSAttributes(strings.TrimPrefix(line, "#EXT-X-MAP:"))
			if uri, ok := attrs["URI"]; ok {
				initSegment = &models.Segment{
					Index: -1,
					URL:   resolveURL(baseURL, strings.Trim(uri, "\"")),
				}
				if br, ok := attrs["BYTERANGE"]; ok {
					initSegment.ByteRange = parseByteRange(br)
				}
			}

		case !strings.HasPrefix(line, "#") && line != "":
			segment := &models.Segment{
				Index:    segmentIndex,
				URL:      resolveURL(baseURL, line),
				Duration: segmentDuration,
			}
			segments = append(segments, segment)
			segmentIndex++
		}
	}

	return segments, initSegment
}
