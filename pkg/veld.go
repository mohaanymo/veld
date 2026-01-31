// Package veld provides a high-performance HLS/DASH media downloader.
package veld

import (
	"context"
	"fmt"

	"github.com/mohaanymo/veld/config"
	"github.com/mohaanymo/veld/engine"
	"github.com/mohaanymo/veld/models"
	"github.com/mohaanymo/veld/parser"
)

// Downloader is the main API for downloading media streams.
type Downloader struct {
	cfg      *config.Config
	eng      *engine.Engine
	manifest *models.Manifest
}

// Option configures the downloader.
type Option func(*config.Config)

// New creates a new Downloader with the given options.
func New(opts ...Option) (*Downloader, error) {
	cfg := config.New()
	for _, opt := range opts {
		opt(cfg)
	}
	
	if err := cfg.Validate(); err != nil {
		return nil, err
	}

	eng, err := engine.New(cfg)
	if err != nil {
		return nil, err
	}

	return &Downloader{
		cfg: cfg,
		eng: eng,
	}, nil
}

// WithURL sets the stream URL.
func WithURL(url string) Option {
	return func(c *config.Config) {
		c.URL = url
	}
}

// WithOutput sets the output file path.
func WithOutput(path string) Option {
	return func(c *config.Config) {
		c.OutputPath = path
	}
}

// WithThreads sets the number of concurrent download threads.
func WithThreads(n int) Option {
	return func(c *config.Config) {
		c.Threads = n
	}
}

// WithFormat sets the output format (mp4, mkv, ts).
func WithFormat(format string) Option {
	return func(c *config.Config) {
		c.Format = format
	}
}

// WithHeaders sets custom HTTP headers.
func WithHeaders(headers map[string]string) Option {
	return func(c *config.Config) {
		for k, v := range headers {
			c.Headers[k] = v
		}
	}
}

// WithTrackSelector sets the track selection string.
func WithTrackSelector(selector string) Option {
	return func(c *config.Config) {
		c.TrackSelector = selector
	}
}

// WithDecryptionKey sets the decryption key (KID:KEY format).
func WithDecryptionKey(key string) Option {
	return func(c *config.Config) {
		c.DecryptionKey = key
	}
}

// Parse fetches and parses the manifest.
func (d *Downloader) Parse(ctx context.Context) error {
	registry := parser.NewRegistry()
	manifest, err := registry.Parse(ctx, d.cfg.URL, d.cfg.Headers)
	if err != nil {
		return fmt.Errorf("parse manifest: %w", err)
	}
	d.manifest = manifest
	return nil
}

// Tracks returns available tracks after parsing.
func (d *Downloader) Tracks() []*models.Track {
	if d.manifest == nil {
		return nil
	}
	return d.manifest.Tracks
}

// SelectTracks selects tracks based on the configured selector.
func (d *Downloader) SelectTracks() error {
	if d.manifest == nil {
		return fmt.Errorf("manifest not parsed, call Parse() first")
	}
	return d.eng.SelectTracks(d.manifest)
}

// SetSelectedTracks allows manual track selection.
func (d *Downloader) SetSelectedTracks(tracks []*models.Track) {
	d.eng.SelectedTracks = tracks
}

// Download starts the download process.
func (d *Downloader) Download(ctx context.Context) error {
	if d.manifest == nil {
		return fmt.Errorf("manifest not parsed, call Parse() first")
	}
	return d.eng.Download(ctx, d.manifest)
}

// Progress returns a channel for progress updates.
func (d *Downloader) Progress() <-chan engine.ProgressUpdate {
	return d.eng.Progress()
}

// Close releases resources.
func (d *Downloader) Close() error {
	return d.eng.Close()
}

// DownloadURL is a convenience function for simple downloads.
func DownloadURL(ctx context.Context, url, output string, opts ...Option) error {
	allOpts := append([]Option{WithURL(url), WithOutput(output)}, opts...)
	d, err := New(allOpts...)
	if err != nil {
		return err
	}
	defer d.Close()

	if err := d.Parse(ctx); err != nil {
		return err
	}

	if err := d.SelectTracks(); err != nil {
		return err
	}

	return d.Download(ctx)
}