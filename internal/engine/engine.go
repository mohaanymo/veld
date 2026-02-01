// Package engine provides the high-performance download engine.
package engine

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/mohaanymo/veld/internal/config"
	"github.com/mohaanymo/veld/internal/decryptor"
	"github.com/mohaanymo/veld/internal/httpclient"
	"github.com/mohaanymo/veld/internal/models"
	"github.com/mohaanymo/veld/internal/parser"
)

// Engine is the main download orchestrator.
type Engine struct {
	cfg        *config.Config
	client     *http.Client
	pool       *WorkerPool
	progressCh chan ProgressUpdate

	// Selected tracks (set after selection)
	SelectedTracks []*models.Track

	// Resume support
	checkpoint     *Checkpoint
	checkpointPath string

	// Pluggable interfaces
	muxer Muxer
}

// New creates a new Engine with optimized settings.
func New(cfg *config.Config) (*Engine, error) {
	// Use shared HTTP client with optional rate limiting
	var client *http.Client
	if cfg.MaxBandwidth > 0 {
		client = httpclient.NewWithRateLimit(httpclient.DefaultConfig(), cfg.MaxBandwidth)
	} else {
		client = httpclient.New(httpclient.DefaultConfig())
	}

	progressCh := make(chan ProgressUpdate, 100)

	e := &Engine{
		cfg:        cfg,
		client:     client,
		progressCh: progressCh,
		muxer:      NewAutoMuxer(cfg),
	}

	e.pool = NewWorkerPool(cfg.Threads, client, progressCh)
	e.pool.SetVerbose(cfg.Verbose)

	return e, nil
}

// SelectTracks selects tracks from manifest and stores them.
func (e *Engine) SelectTracks(manifest *models.Manifest) error {
	selected, err := SelectTracks(manifest.Tracks, e.cfg.TrackSelector)
	if err != nil {
		return err
	}

	// Set up decryptors for encrypted tracks
	for _, track := range selected {
		// CENC decryptor (DASH) - uses provided key
		if len(e.cfg.DecryptionKeys) != 0 {
			for _, kidkey := range e.cfg.DecryptionKeys {
				if strings.Contains(kidkey, track.KeyID) {
					dec, _ := decryptor.New(kidkey)
					track.Decryptor = dec
				}
			}
		}

		// HLS AES-128 decryptor - key fetched from URI
		if track.EncryptionURI != "" && track.Decryptor == nil {
			track.HLSDecryptor = decryptor.NewHLSDecryptor(e.client, e.cfg.Headers)
		}
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

	// Set up temp directory and checkpoint for resume support
	outputPath := filepath.Join(e.cfg.OutputDir, e.cfg.FileName)
	e.checkpointPath = CheckpointPath(outputPath)
	tempDir := filepath.Join(os.TempDir(), fmt.Sprintf("veld_%d", os.Getpid()))

	// Try to load existing checkpoint for resume
	existingCP, _ := LoadCheckpoint(e.checkpointPath)
	if existingCP != nil && existingCP.Matches(e.cfg.URL) {
		// Resume from existing checkpoint
		tempDir = existingCP.TempDir
		e.checkpoint = existingCP
		if e.cfg.Verbose {
			fmt.Printf("Resuming download from checkpoint\n")
		}
	} else {
		// Create new checkpoint
		if err := os.MkdirAll(tempDir, 0755); err != nil {
			return fmt.Errorf("create temp dir: %w", err)
		}
		e.checkpoint = NewCheckpoint(e.cfg.URL, tempDir)
	}

	e.pool.SetTempDir(tempDir)

	// Set up checkpoint callback
	e.pool.SetOnSegmentDone(func(trackID string, index int) {
		e.checkpoint.MarkDone(trackID, index)
	})

	// Start worker pool
	e.pool.Start(ctx)
	defer e.pool.Stop()

	// CENC decryption function (for DASH)
	cencDecFunc := func(track *models.Track, segment *models.Segment) error {
		// Combine init + segment
		combined := make([]byte, len(track.InitSegment.Data)+len(segment.Data))
		copy(combined, track.InitSegment.Data)
		copy(combined[len(track.InitSegment.Data):], segment.Data)
		decrypted, err := track.Decryptor.Decrypt(combined)
		if err != nil {
			return err
		}
		segment.Data = decrypted
		return nil
	}

	// HLS AES-128 decryption function
	hlsDecFunc := func(track *models.Track, segment *models.Segment) error {
		// Fetch key (cached after first fetch)
		key, err := track.HLSDecryptor.FetchKey(ctx, track.EncryptionURI)
		if err != nil {
			return fmt.Errorf("fetch key: %w", err)
		}

		// Use segment index as IV if none specified
		iv := track.EncryptionIV
		if len(iv) == 0 {
			iv = decryptor.SegmentIV(segment.Index)
		}

		decrypted, err := track.HLSDecryptor.Decrypt(segment.Data, key, iv)
		if err != nil {
			return fmt.Errorf("decrypt: %w", err)
		}
		segment.Data = decrypted
		return nil
	}

	// Queue media segments (skip already completed ones for resume)
	totalSegments := 0
	skippedSegments := 0
	for _, track := range e.SelectedTracks {
		for _, segment := range track.Segments {
			totalSegments++

			// Skip if already downloaded (resume)
			if e.checkpoint.IsSegmentDone(track.ID, segment.Index) {
				segment.FilePath = e.checkpoint.SegmentPath(track.ID, segment.Index)
				skippedSegments++
				continue
			}

			task := &SegmentTask{
				Segment: segment,
				Track:   track,
				Headers: e.cfg.Headers,
			}
			// Set appropriate decryption function
			if track.Decryptor != nil {
				task.DecFunc = cencDecFunc
			} else if track.HLSDecryptor != nil {
				task.DecFunc = hlsDecFunc
			}
			e.pool.Submit(task)
		}
	}

	if e.cfg.Verbose && skippedSegments > 0 {
		fmt.Printf("Resuming: skipped %d/%d segments\n", skippedSegments, totalSegments)
	}

	// Wait for completion
	if err := e.pool.Wait(); err != nil {
		// Save checkpoint for future resume
		e.checkpoint.Save(e.checkpointPath)
		return err
	}

	// Success: clean up checkpoint and temp files after muxing
	defer func() {
		os.Remove(e.checkpointPath)
		os.RemoveAll(tempDir)
	}()

	if _, err := os.Stat(e.cfg.OutputDir); os.IsNotExist(err) {
		os.MkdirAll(e.cfg.OutputDir, 0644)
	}

	// Mux tracks into final output
	return e.muxer.Mux(ctx, e.SelectedTracks, filepath.Join(e.cfg.OutputDir, e.cfg.FileName), ContainerFormat(e.cfg.Format))
}

// decryptTrack decrypts all segments in a track.
func (e *Engine) decryptTrack(track *models.Track) error {
	if track.Decryptor == nil {
		return nil
	}

	for _, segment := range track.Segments {
		// Combine init + segment
		combined := make([]byte, len(track.InitSegment.Data)+len(segment.Data))
		copy(combined, track.InitSegment.Data)
		copy(combined[len(track.InitSegment.Data):], segment.Data)
		decrypted, err := track.Decryptor.Decrypt(combined)
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
