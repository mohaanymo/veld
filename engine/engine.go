// Package engine provides the high-performance download engine.
package engine

import (
	"context"
	"crypto/tls"
	"fmt"
	"io"
	"net"
	"net/http"
	"time"

	"github.com/mohaanymo/veld/config"
	"github.com/mohaanymo/veld/decryptor"
	"github.com/mohaanymo/veld/models"
	"github.com/mohaanymo/veld/parser"
)

// Engine is the main download orchestrator.
type Engine struct {
	cfg        *config.Config
	client     *http.Client
	pool       *WorkerPool
	progressCh chan ProgressUpdate

	// Selected tracks (set after selection)
	SelectedTracks []*models.Track

	// Pluggable interfaces
	decryptor *decryptor.Decryptor
	muxer     Muxer
}

// New creates a new Engine with optimized settings.
func New(cfg *config.Config) (*Engine, error) {
	client := newOptimizedClient()
	progressCh := make(chan ProgressUpdate, 100)

	var dec *decryptor.Decryptor
	if cfg.DecryptionKey != "" {
		d, err := decryptor.New(cfg.DecryptionKey)
		if err != nil {
			return nil, err
		}
		dec = d
	}

	e := &Engine{
		cfg:        cfg,
		client:     client,
		progressCh: progressCh,
		decryptor:  dec,
		muxer:      NewAutoMuxer(cfg),
	}

	e.pool = NewWorkerPool(cfg.Threads, client, progressCh)
	e.pool.SetVerbose(cfg.Verbose)

	return e, nil
}

// newOptimizedClient creates a heavily tuned HTTP client for maximum throughput.
func newOptimizedClient() *http.Client {
	dialer := &net.Dialer{
		Timeout:   30 * time.Second,
		KeepAlive: 30 * time.Second,
		DualStack: true,
	}

	transport := &http.Transport{
		// Connection pooling - aggressive settings for CDN downloads
		MaxIdleConns:        200,
		MaxIdleConnsPerHost: 100,
		MaxConnsPerHost:     100,
		IdleConnTimeout:     90 * time.Second,

		// Timeouts
		TLSHandshakeTimeout:   10 * time.Second,
		ResponseHeaderTimeout: 30 * time.Second,
		ExpectContinueTimeout: 1 * time.Second,

		// Disable compression - segments are already compressed
		DisableCompression: true,

		// Enable HTTP/2 for multiplexing
		ForceAttemptHTTP2: true,

		DialContext: dialer.DialContext,

		TLSClientConfig: &tls.Config{
			MinVersion: tls.VersionTLS12,
		},
	}

	return &http.Client{
		Transport: transport,
		Timeout:   0, // No overall timeout; handled per-request
	}
}

// SelectTracks selects tracks from manifest and stores them.
func (e *Engine) SelectTracks(manifest *models.Manifest) error {
	selected, err := SelectTracks(manifest.Tracks, e.cfg.TrackSelector)
	if err != nil {
		return err
	}
	e.SelectedTracks = selected
	return nil
}

// Download initiates the download process for selected tracks.
func (e *Engine) Download(ctx context.Context, manifest *models.Manifest) error {
	if e.SelectedTracks == nil {
		if err := e.SelectTracks(manifest); err != nil {
			return err
		}
	}

	// Lazy load segments for tracks with media playlist URL but no segments
	for _, track := range e.SelectedTracks {
		if e.cfg.Verbose {
			fmt.Printf("Track %s: Type=%s, MediaPlaylistURL=%q, Segments=%d\n",
				track.ID, track.Type, track.MediaPlaylistURL, len(track.Segments))
		}
		if track.MediaPlaylistURL != "" && len(track.Segments) == 0 {
			if err := e.LoadTrackSegments(ctx, track); err != nil {
				return fmt.Errorf("load segments for %s: %w", track.ID, err)
			}
		}
	}

	// Download init segments first (required for fMP4)
	for _, track := range e.SelectedTracks {
		if track.InitSegment != nil && track.InitSegment.URL != "" {
			if err := e.downloadInitSegment(ctx, track); err != nil {
				return fmt.Errorf("download init segment for %s: %w", track.ID, err)
			}
		}
	}

	// Start worker pool
	e.pool.Start(ctx)
	defer e.pool.Stop()

	// Queue all media segments
	for _, track := range e.SelectedTracks {
		for _, segment := range track.Segments {
			task := &SegmentTask{
				Segment: segment,
				Track:   track,
				Headers: e.cfg.Headers,
			}
			e.pool.Submit(task)
		}
	}

	// Wait for completion
	if err := e.pool.Wait(); err != nil {
		return err
	}

	// Decrypt segments if needed
	if e.decryptor != nil {
		for _, track := range e.SelectedTracks {
			if err := e.decryptTrack(track); err != nil {
				return err
			}
		}
	}

	// Mux tracks into final output
	return e.muxer.Mux(ctx, e.SelectedTracks, e.cfg.OutputPath, ContainerFormat(e.cfg.Format))
}

// decryptTrack decrypts all segments in a track.
func (e *Engine) decryptTrack(track *models.Track) error {
	if e.decryptor == nil {
		return nil
	}

	for _, segment := range track.Segments {
		// Combine init + segment
		combined := make([]byte, len(track.InitSegment.Data)+len(segment.Data))
		copy(combined, track.InitSegment.Data)
		copy(combined[len(track.InitSegment.Data):], segment.Data)
		decrypted, err := e.decryptor.Decrypt(combined)
		if err != nil {
			return err
		}
		segment.Data = decrypted
	}

	return nil
}

// Progress returns the progress update channel.
func (e *Engine) Progress() <-chan ProgressUpdate {
	return e.progressCh
}

// Close releases engine resources.
func (e *Engine) Close() error {
	close(e.progressCh)
	return nil
}

// SetMuxer sets a custom muxer implementation.
func (e *Engine) SetMuxer(m Muxer) {
	e.muxer = m
}

// downloadInitSegment downloads the initialization segment for a track.
func (e *Engine) downloadInitSegment(ctx context.Context, track *models.Track) error {
	if track.InitSegment == nil || track.InitSegment.URL == "" {
		return nil
	}

	req, err := http.NewRequestWithContext(ctx, "GET", track.InitSegment.URL, nil)
	if err != nil {
		return fmt.Errorf("create request: %w", err)
	}

	for k, v := range e.cfg.Headers {
		req.Header.Set(k, v)
	}

	if track.InitSegment.ByteRange != nil {
		req.Header.Set("Range", fmt.Sprintf("bytes=%d-%d",
			track.InitSegment.ByteRange.Start,
			track.InitSegment.ByteRange.End))
	}

	resp, err := e.client.Do(req)
	if err != nil {
		return fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusPartialContent {
		return fmt.Errorf("unexpected status: %d", resp.StatusCode)
	}

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("read body: %w", err)
	}

	track.InitSegment.Data = data

	if e.cfg.Verbose {
		fmt.Printf("Downloaded init segment for %s: %d bytes\n", track.ID, len(data))
	}

	return nil
}

// LoadTrackSegments fetches the media playlist and populates track segments.
// Used for lazy loading of audio/subtitle tracks in HLS.
func (e *Engine) LoadTrackSegments(ctx context.Context, track *models.Track) error {
	if track.MediaPlaylistURL == "" {
		return nil
	}

	req, err := http.NewRequestWithContext(ctx, "GET", track.MediaPlaylistURL, nil)
	if err != nil {
		return fmt.Errorf("create request: %w", err)
	}

	for k, v := range e.cfg.Headers {
		req.Header.Set(k, v)
	}

	resp, err := e.client.Do(req)
	if err != nil {
		return fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("unexpected status: %d", resp.StatusCode)
	}

	content, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("read body: %w", err)
	}

	segments, initSeg := parser.ParseMediaPlaylist(string(content), track.MediaPlaylistURL)
	track.Segments = segments
	if initSeg != nil {
		track.InitSegment = initSeg
	}

	if e.cfg.Verbose {
		fmt.Printf("Loaded %d segments for %s (init: %v)\n", len(segments), track.ID, initSeg != nil)
	}

	return nil
}
