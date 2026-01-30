// Package parser provides manifest parsing for HLS and DASH streams.
package parser

import (
	"context"
	"fmt"
	"net/url"
	"strconv"
	"strings"

	"veld/internal/models"
)

// Parser defines the interface for manifest parsers.
type Parser interface {
	Parse(ctx context.Context, url string, headers map[string]string) (*models.Manifest, error)
	CanParse(url string) bool
}

// Registry manages available parsers.
type Registry struct {
	parsers []Parser
}

// NewRegistry creates a new parser registry with default parsers.
func NewRegistry() *Registry {
	return &Registry{
		parsers: []Parser{
			NewHLSParser(),
			NewDASHParser(),
		},
	}
}

// Parse finds an appropriate parser and parses the manifest.
func (r *Registry) Parse(ctx context.Context, urlStr string, headers map[string]string) (*models.Manifest, error) {
	for _, p := range r.parsers {
		if p.CanParse(urlStr) {
			return p.Parse(ctx, urlStr, headers)
		}
	}
	return nil, fmt.Errorf("no parser found for URL: %s", urlStr)
}

// Common helper functions used by parsers

// resolveURL resolves a relative URL against a base URL.
func resolveURL(base *url.URL, relative string) string {
	if strings.HasPrefix(relative, "http://") || strings.HasPrefix(relative, "https://") {
		return relative
	}
	rel, err := url.Parse(relative)
	if err != nil {
		return relative
	}
	return base.ResolveReference(rel).String()
}

// parseByteRange parses a BYTERANGE attribute (format: "length@offset" or "start-end").
func parseByteRange(s string) *models.ByteRange {
	s = strings.Trim(s, "\"")

	// Handle "length@offset" format (HLS)
	if strings.Contains(s, "@") {
		parts := strings.Split(s, "@")
		if len(parts) < 1 {
			return nil
		}
		length, _ := strconv.ParseInt(parts[0], 10, 64)
		start := int64(0)
		if len(parts) > 1 {
			start, _ = strconv.ParseInt(parts[1], 10, 64)
		}
		return &models.ByteRange{Start: start, End: start + length - 1}
	}

	// Handle "start-end" format (DASH)
	parts := strings.Split(s, "-")
	if len(parts) != 2 {
		return nil
	}
	start, _ := strconv.ParseInt(parts[0], 10, 64)
	end, _ := strconv.ParseInt(parts[1], 10, 64)
	return &models.ByteRange{Start: start, End: end}
}

// parseHexBytes parses a hex string (0x...) to bytes.
func parseHexBytes(s string) []byte {
	s = strings.TrimPrefix(s, "0x")
	s = strings.TrimPrefix(s, "0X")
	var result []byte
	for i := 0; i < len(s)-1; i += 2 {
		b, _ := strconv.ParseUint(s[i:i+2], 16, 8)
		result = append(result, byte(b))
	}
	return result
}
