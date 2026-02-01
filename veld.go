// Package veld provides a high-performance HLS/DASH media downloader.
//
// Basic usage:
//
//	d, err := veld.New(
//		veld.WithURL("https://example.com/video.m3u8"),
//		veld.WithOutput("video.mp4"),
//	)
//	if err != nil {
//		log.Fatal(err)
//	}
//	defer d.Close()
//
//	if err := d.Parse(ctx); err != nil {
//		log.Fatal(err)
//	}
//	if err := d.SelectTracks(); err != nil {
//		log.Fatal(err)
//	}
//	if err := d.Download(ctx); err != nil {
//		log.Fatal(err)
//	}
//
// Or use the convenience function:
//
//	err := veld.DownloadURL(ctx, "https://example.com/video.m3u8", "video.mp4")
package veld

import (
	"context"
	"fmt"

	"github.com/mohaanymo/veld/internal/config"
	"github.com/mohaanymo/veld/internal/engine"
	"github.com/mohaanymo/veld/internal/models"
	"github.com/mohaanymo/veld/internal/parser"
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

// WithURL sets the stream URL (required).
func WithURL(url string) Option {
	return func(c *config.Config) {
		c.URL = url
	}
}

// WithOutput sets the output file path.
func WithFileName(filename string) Option {
	return func(c *config.Config) {
		c.FileName = filename
	}
}

// WithDir sets the directory path.
func WithDir(dir string) Option {
	return func(c *config.Config) {
		c.OutputDir = dir
	}
}

// WithThreads sets the number of concurrent download threads (default: 16, max: 128).
func WithThreads(n int) Option {
	return func(c *config.Config) {
		c.Threads = n
	}
}

// WithFormat sets the output format: "mp4", "mkv", or "ts" (default: "mp4").
func WithFormat(format string) Option {
	return func(c *config.Config) {
		c.Format = format
	}
}

// WithHeaders sets custom HTTP headers for requests.
func WithHeaders(headers map[string]string) Option {
	return func(c *config.Config) {
		for k, v := range headers {
			c.Headers[k] = v
		}
	}
}

// WithHeader adds a single HTTP header.
func WithHeader(key, value string) Option {
	return func(c *config.Config) {
		c.Headers[key] = value
	}
}

// WithCookies sets cookies for HTTP requests.
func WithCookies(cookies string) Option {
	return func(c *config.Config) {
		c.Cookies = cookies
	}
}

// WithTrackSelector sets the track selection string.
// Examples: "best", "1080p", "720p", "all", "video:0+audio:1"
func WithTrackSelector(selector string) Option {
	return func(c *config.Config) {
		c.TrackSelector = selector
	}
}

// WithDecryptionKey sets the decryption key in "KID:KEY" format (32 hex chars each).
func WithDecryptionKeys(keys []string) Option {
	return func(c *config.Config) {
		c.DecryptionKeys = keys
	}
}

// WithVerbose enables verbose logging.
func WithVerbose(verbose bool) Option {
	return func(c *config.Config) {
		c.Verbose = verbose
	}
}

// WithParallelTracks enables downloading all tracks concurrently.
func WithParallelTracks(parallel bool) Option {
	return func(c *config.Config) {
		c.ParallelTracks = parallel
	}
}

// WithMaxBandwidth sets maximum download speed in bytes per second.
// Set to 0 for unlimited (default).
func WithMaxBandwidth(bytesPerSec int64) Option {
	return func(c *config.Config) {
		c.MaxBandwidth = bytesPerSec
	}
}

// Parse fetches and parses the manifest from the configured URL.
// Must be called before Tracks(), SelectTracks(), or Download().
func (d *Downloader) Parse(ctx context.Context) error {
	registry := parser.NewRegistry()
	manifest, err := registry.Parse(ctx, d.cfg.URL, d.cfg.Headers)
	if err != nil {
		return fmt.Errorf("parse manifest: %w", err)
	}
	d.manifest = manifest
	return nil
}

// Tracks returns all available tracks after parsing.
// Returns nil if Parse() hasn't been called.
func (d *Downloader) Tracks() []*Track {
	if d.manifest == nil {
		return nil
	}
	// Convert internal tracks to public Track type
	tracks := make([]*Track, len(d.manifest.Tracks))
	for i, t := range d.manifest.Tracks {
		tracks[i] = &Track{internal: t}
	}
	return tracks
}

// SelectTracks selects tracks based on the configured selector.
// If no selector was configured, uses "best" (best video + best audio).
func (d *Downloader) SelectTracks() error {
	if d.manifest == nil {
		return fmt.Errorf("manifest not parsed, call Parse() first")
	}
	return d.eng.SelectTracks(d.manifest)
}

// SetSelectedTracks allows manual track selection.
// Pass tracks obtained from Tracks().
func (d *Downloader) SetSelectedTracks(tracks []*Track) {
	internal := make([]*models.Track, len(tracks))
	for i, t := range tracks {
		internal[i] = t.internal
	}
	d.eng.SelectedTracks = internal
}

// SelectedTracks returns the currently selected tracks.
func (d *Downloader) SelectedTracks() []*Track {
	if d.eng.SelectedTracks == nil {
		return nil
	}
	tracks := make([]*Track, len(d.eng.SelectedTracks))
	for i, t := range d.eng.SelectedTracks {
		tracks[i] = &Track{internal: t}
	}
	return tracks
}

// Download starts the download process.
// Blocks until complete or context is canceled.
func (d *Downloader) Download(ctx context.Context) error {
	if d.manifest == nil {
		return fmt.Errorf("manifest not parsed, call Parse() first")
	}
	return d.eng.Download(ctx, d.manifest)
}

// Progress returns a channel for receiving download progress updates.
// The channel is closed when the download completes.
func (d *Downloader) Progress() <-chan ProgressUpdate {
	ch := make(chan ProgressUpdate, 100)
	go func() {
		defer close(ch)
		for p := range d.eng.Progress() {
			ch <- ProgressUpdate{
				SegmentIndex: p.SegmentIndex,
				TrackID:      p.TrackID,
				BytesLoaded:  p.BytesLoaded,
				Completed:    p.Completed,
				Error:        p.Error,
			}
		}
	}()
	return ch
}

// Close releases all resources held by the downloader.
// Always call Close() when done, preferably with defer.
func (d *Downloader) Close() error {
	return d.eng.Close()
}

// getSelectedTracksInternal returns internal track models (for manager use).
func (d *Downloader) getSelectedTracksInternal() interface{} {
	return d.eng.SelectedTracks
}

// ManifestType returns the type of manifest ("HLS" or "DASH").
// Returns empty string if Parse() hasn't been called.
func (d *Downloader) ManifestType() string {
	if d.manifest == nil {
		return ""
	}
	return d.manifest.Type.String()
}

// DownloadURL is a convenience function for simple downloads.
// It parses the manifest, selects tracks (using "best" or configured selector),
// and downloads to the specified output path.
func DownloadURL(ctx context.Context, url, filename string, opts ...Option) error {
	allOpts := append([]Option{
		WithURL(url),
		WithFileName(filename),
		WithTrackSelector("best"),
	}, opts...)

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
