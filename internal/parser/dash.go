package parser

import (
	"context"
	"encoding/xml"
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

// DASHParser parses DASH (mpd) manifests.
type DASHParser struct {
	client *http.Client
}

// NewDASHParser creates a new DASH parser.
func NewDASHParser() *DASHParser {
	return &DASHParser{
		client: &http.Client{Timeout: 30 * time.Second},
	}
}

// CanParse checks if URL is a DASH manifest.
func (p *DASHParser) CanParse(urlStr string) bool {
	lower := strings.ToLower(urlStr)
	return strings.Contains(lower, ".mpd") || strings.Contains(lower, "format=mpd")
}

// Parse parses a DASH manifest.
func (p *DASHParser) Parse(ctx context.Context, urlStr string, headers map[string]string) (*models.Manifest, error) {
	content, err := p.fetch(ctx, urlStr, headers)
	if err != nil {
		return nil, fmt.Errorf("fetch manifest: %w", err)
	}

	baseURL, _ := url.Parse(urlStr)

	var mpd MPD
	if err := xml.Unmarshal([]byte(content), &mpd); err != nil {
		return nil, fmt.Errorf("parse MPD: %w", err)
	}

	return p.convertMPD(&mpd, baseURL)
}

// DASH MPD XML structures

type MPD struct {
	XMLName                   xml.Name `xml:"MPD"`
	MediaPresentationDuration string   `xml:"mediaPresentationDuration,attr"`
	MinBufferTime             string   `xml:"minBufferTime,attr"`
	Periods                   []Period `xml:"Period"`
	BaseURL                   string   `xml:"BaseURL"`
}

type Period struct {
	ID             string          `xml:"id,attr"`
	Start          string          `xml:"start,attr"`
	Duration       string          `xml:"duration,attr"`
	AdaptationSets []AdaptationSet `xml:"AdaptationSet"`
	BaseURL        string          `xml:"BaseURL"`
}

type AdaptationSet struct {
	ID                 string              `xml:"id,attr"`
	MimeType           string              `xml:"mimeType,attr"`
	ContentType        string              `xml:"contentType,attr"`
	Lang               string              `xml:"lang,attr"`
	Codecs             string              `xml:"codecs,attr"`
	Width              int                 `xml:"width,attr"`
	Height             int                 `xml:"height,attr"`
	Representations    []Representation    `xml:"Representation"`
	ContentProtections []ContentProtection `xml:"ContentProtection"`
	SegmentTemplate    *SegmentTemplate    `xml:"SegmentTemplate"`
	BaseURL            string              `xml:"BaseURL"`
}

type Representation struct {
	ID              string           `xml:"id,attr"`
	Bandwidth       int64            `xml:"bandwidth,attr"`
	Width           int              `xml:"width,attr"`
	Height          int              `xml:"height,attr"`
	Codecs          string           `xml:"codecs,attr"`
	MimeType        string           `xml:"mimeType,attr"`
	SegmentTemplate *SegmentTemplate `xml:"SegmentTemplate"`
	SegmentList     *SegmentList     `xml:"SegmentList"`
	BaseURL         string           `xml:"BaseURL"`
}

type SegmentTemplate struct {
	Media          string    `xml:"media,attr"`
	Initialization string    `xml:"initialization,attr"`
	Timescale      int       `xml:"timescale,attr"`
	Duration       int       `xml:"duration,attr"`
	StartNumber    int       `xml:"startNumber,attr"`
	Timeline       *Timeline `xml:"SegmentTimeline"`
}

type Timeline struct {
	S []SegmentTime `xml:"S"`
}

type SegmentTime struct {
	T int `xml:"t,attr"` // Start time
	D int `xml:"d,attr"` // Duration
	R int `xml:"r,attr"` // Repeat count
}

type SegmentList struct {
	Initialization *URLType  `xml:"Initialization"`
	Segments       []URLType `xml:"SegmentURL"`
}

type URLType struct {
	SourceURL string `xml:"sourceURL,attr"`
	Media     string `xml:"media,attr"`
	Range     string `xml:"range,attr"`
}

type ContentProtection struct {
	SchemeIdUri string `xml:"schemeIdUri,attr"`
	Value       string `xml:"value,attr"`
	DefaultKID  string `xml:"default_KID,attr"`
	PSSH        string `xml:"pssh"`
}

// convertMPD converts parsed MPD to our manifest model.
func (p *DASHParser) convertMPD(mpd *MPD, baseURL *url.URL) (*models.Manifest, error) {
	manifest := &models.Manifest{
		URL:      baseURL.String(),
		Type:     models.ManifestDASH,
		Duration: parseDuration(mpd.MediaPresentationDuration),
	}

	for _, period := range mpd.Periods {
		periodBase := resolveBase(baseURL, mpd.BaseURL, period.BaseURL)

		for _, as := range period.AdaptationSets {
			asBase := resolveBase(periodBase, as.BaseURL, "")
			trackType := detectTrackType(as.MimeType, as.ContentType)

			// Check for encryption
			var keyID string
			encrypted := len(as.ContentProtections) > 0
			for _, cp := range as.ContentProtections {
				if cp.DefaultKID != "" {
					keyID = strings.ReplaceAll(cp.DefaultKID, "-", "")
				}
			}

			for _, rep := range as.Representations {
				repBase := resolveBase(asBase, rep.BaseURL, "")

				track := &models.Track{
					ID:        rep.ID,
					Type:      trackType,
					Bandwidth: rep.Bandwidth,
					Codec:     firstNonEmpty(rep.Codecs, as.Codecs),
					Language:  as.Lang,
					Resolution: models.Resolution{
						Width:  firstNonZero(rep.Width, as.Width),
						Height: firstNonZero(rep.Height, as.Height),
					},
					Encrypted: encrypted,
					KeyID:     keyID,
				}

				// Get segment template (from rep or adaptation set)
				tmpl := rep.SegmentTemplate
				if tmpl == nil {
					tmpl = as.SegmentTemplate
				}

				if tmpl != nil {
					track.Segments, track.InitSegment = p.buildSegmentsFromTemplate(tmpl, rep, repBase)
				} else if rep.SegmentList != nil {
					track.Segments, track.InitSegment = p.buildSegmentsFromList(rep.SegmentList, repBase)
				} else if rep.BaseURL != "" {
					// Non-segmented content (e.g., single VTT subtitle file)
					track.Segments = []*models.Segment{{
						Index: 0,
						URL:   repBase.String(),
					}}
				}

				manifest.Tracks = append(manifest.Tracks, track)
			}
		}
	}

	return manifest, nil
}

// buildSegmentsFromTemplate generates segments from a template.
func (p *DASHParser) buildSegmentsFromTemplate(tmpl *SegmentTemplate, rep Representation, base *url.URL) ([]*models.Segment, *models.Segment) {
	var segments []*models.Segment
	var initSeg *models.Segment

	if tmpl.Initialization != "" {
		initURL := expandTemplate(tmpl.Initialization, rep.ID, 0, 0)
		initSeg = &models.Segment{
			Index: -1,
			URL:   resolveURL(base, initURL),
		}
	}

	timescale := tmpl.Timescale
	if timescale == 0 {
		timescale = 1
	}

	if tmpl.Timeline != nil && len(tmpl.Timeline.S) > 0 {
		segNum := tmpl.StartNumber
		if segNum == 0 {
			segNum = 1
		}
		currentTime := 0

		for _, s := range tmpl.Timeline.S {
			if s.T > 0 {
				currentTime = s.T
			}
			repeatCount := s.R + 1
			if s.R < 0 {
				repeatCount = 1
			}

			for i := 0; i < repeatCount; i++ {
				mediaURL := expandTemplate(tmpl.Media, rep.ID, segNum, currentTime)
				seg := &models.Segment{
					Index:    segNum - 1,
					URL:      resolveURL(base, mediaURL),
					Duration: time.Duration(s.D) * time.Second / time.Duration(timescale),
				}
				segments = append(segments, seg)
				segNum++
				currentTime += s.D
			}
		}
	} else if tmpl.Duration > 0 {
		// Fixed duration segments - default to 100 segments
		numSegments := 100
		for i := 0; i < numSegments; i++ {
			segNum := tmpl.StartNumber + i
			mediaURL := expandTemplate(tmpl.Media, rep.ID, segNum, 0)
			seg := &models.Segment{
				Index:    i,
				URL:      resolveURL(base, mediaURL),
				Duration: time.Duration(tmpl.Duration) * time.Second / time.Duration(timescale),
			}
			segments = append(segments, seg)
		}
	}

	return segments, initSeg
}

// buildSegmentsFromList builds segments from explicit list.
func (p *DASHParser) buildSegmentsFromList(list *SegmentList, base *url.URL) ([]*models.Segment, *models.Segment) {
	var segments []*models.Segment
	var initSeg *models.Segment

	if list.Initialization != nil && list.Initialization.SourceURL != "" {
		initSeg = &models.Segment{
			Index: -1,
			URL:   resolveURL(base, list.Initialization.SourceURL),
		}
		if list.Initialization.Range != "" {
			initSeg.ByteRange = parseByteRange(list.Initialization.Range)
		}
	}

	for i, seg := range list.Segments {
		s := &models.Segment{
			Index: i,
			URL:   resolveURL(base, seg.Media),
		}
		if seg.Range != "" {
			s.ByteRange = parseByteRange(seg.Range)
		}
		segments = append(segments, s)
	}

	return segments, initSeg
}

// fetch downloads content from URL.
func (p *DASHParser) fetch(ctx context.Context, urlStr string, headers map[string]string) (string, error) {
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

// Helper functions

func detectTrackType(mimeType, contentType string) models.TrackType {
	check := strings.ToLower(mimeType + contentType)
	switch {
	case strings.Contains(check, "video"):
		return models.TrackVideo
	case strings.Contains(check, "audio"):
		return models.TrackAudio
	case strings.Contains(check, "text"), strings.Contains(check, "subtitle"):
		return models.TrackSubtitle
	default:
		return models.TrackVideo
	}
}

func resolveBase(parent *url.URL, paths ...string) *url.URL {
	result := parent
	for _, p := range paths {
		if p == "" {
			continue
		}
		if rel, err := url.Parse(p); err == nil {
			result = result.ResolveReference(rel)
		}
	}
	return result
}

func expandTemplate(template string, repID string, number int, t int) string {
	result := template
	result = strings.ReplaceAll(result, "$RepresentationID$", repID)
	result = strings.ReplaceAll(result, "$Number$", strconv.Itoa(number))
	result = strings.ReplaceAll(result, "$Time$", strconv.Itoa(t))

	// Handle $Number%05d$ style format
	re := regexp.MustCompile(`\$Number%(\d+)d\$`)
	result = re.ReplaceAllStringFunc(result, func(match string) string {
		width, _ := strconv.Atoi(re.FindStringSubmatch(match)[1])
		return fmt.Sprintf("%0*d", width, number)
	})

	return result
}

func parseDuration(s string) time.Duration {
	if s == "" {
		return 0
	}
	s = strings.TrimPrefix(s, "PT")
	s = strings.TrimPrefix(s, "P")

	var hours, minutes, seconds float64

	if idx := strings.Index(s, "H"); idx != -1 {
		hours, _ = strconv.ParseFloat(s[:idx], 64)
		s = s[idx+1:]
	}
	if idx := strings.Index(s, "M"); idx != -1 {
		minutes, _ = strconv.ParseFloat(s[:idx], 64)
		s = s[idx+1:]
	}
	if idx := strings.Index(s, "S"); idx != -1 {
		seconds, _ = strconv.ParseFloat(s[:idx], 64)
	}

	return time.Duration(hours)*time.Hour +
		time.Duration(minutes)*time.Minute +
		time.Duration(seconds*float64(time.Second))
}

func firstNonEmpty(a, b string) string {
	if a != "" {
		return a
	}
	return b
}

func firstNonZero(a, b int) int {
	if a != 0 {
		return a
	}
	return b
}
