// Package httpclient provides a shared, optimized HTTP client for veld.
package httpclient

import (
	"context"
	"crypto/tls"
	"io"
	"net"
	"net/http"
	"time"

	"golang.org/x/time/rate"
)

// Config holds HTTP client configuration.
type Config struct {
	Timeout         time.Duration
	MaxConnsPerHost int
	DisableHTTP2    bool
	Headers         map[string]string
}

// DefaultConfig returns sensible defaults for media downloads.
func DefaultConfig() Config {
	return Config{
		Timeout:         0, // No overall timeout, handled per-request
		MaxConnsPerHost: 100,
		DisableHTTP2:    false,
	}
}

// New creates an optimized HTTP client for high-throughput downloads.
func New(cfg Config) *http.Client {
	if cfg.MaxConnsPerHost == 0 {
		cfg.MaxConnsPerHost = 100
	}

	dialer := &net.Dialer{
		Timeout:   30 * time.Second,
		KeepAlive: 30 * time.Second,
		DualStack: true,
	}

	transport := &http.Transport{
		MaxIdleConns:        200,
		MaxIdleConnsPerHost: cfg.MaxConnsPerHost,
		MaxConnsPerHost:     cfg.MaxConnsPerHost,
		IdleConnTimeout:     90 * time.Second,

		TLSHandshakeTimeout:   10 * time.Second,
		ResponseHeaderTimeout: 30 * time.Second,
		ExpectContinueTimeout: 1 * time.Second,

		DisableCompression: true, // Segments are already compressed
		ForceAttemptHTTP2:  !cfg.DisableHTTP2,
		DialContext:        dialer.DialContext,

		TLSClientConfig: &tls.Config{
			MinVersion: tls.VersionTLS12,
		},
	}

	return &http.Client{
		Transport: transport,
		Timeout:   cfg.Timeout,
	}
}

// NewWithRateLimit creates a client with bandwidth limiting.
// bytesPerSec is the maximum download speed in bytes per second.
// Set to 0 for unlimited.
func NewWithRateLimit(cfg Config, bytesPerSec int64) *http.Client {
	client := New(cfg)

	if bytesPerSec > 0 {
		// Create rate limiter: allow bursts of 64KB
		limiter := rate.NewLimiter(rate.Limit(bytesPerSec), 64*1024)
		client.Transport = &rateLimitedTransport{
			base:    client.Transport,
			limiter: limiter,
		}
	}

	return client
}

// rateLimitedTransport wraps a transport with rate limiting.
type rateLimitedTransport struct {
	base    http.RoundTripper
	limiter *rate.Limiter
}

func (t *rateLimitedTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	resp, err := t.base.RoundTrip(req)
	if err != nil {
		return nil, err
	}

	resp.Body = &rateLimitedReader{
		r:       resp.Body,
		limiter: t.limiter,
		ctx:     req.Context(),
	}
	return resp, nil
}

// rateLimitedReader wraps an io.ReadCloser with rate limiting.
type rateLimitedReader struct {
	r       io.ReadCloser
	limiter *rate.Limiter
	ctx     context.Context
}

func (r *rateLimitedReader) Read(p []byte) (int, error) {
	// Wait for rate limiter before reading
	if err := r.limiter.WaitN(r.ctx, len(p)); err != nil {
		return 0, err
	}
	return r.r.Read(p)
}

func (r *rateLimitedReader) Close() error {
	return r.r.Close()
}
