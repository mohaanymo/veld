// Package config provides configuration types for the downloader.
package config

import (
	"errors"
	"time"
)

// Common errors.
var (
	ErrMissingURL      = errors.New("URL is required")
	ErrInvalidFormat   = errors.New("invalid output format")
	ErrInvalidSelector = errors.New("invalid track selector")
)

// Config holds all application configuration.
type Config struct {
	// Input
	URL string

	// Output
	FileName  string
	OutputDir string
	Format    string // mp4, mkv, ts

	// Download settings
	Threads        int
	ParallelTracks bool
	RetryAttempts  int
	RetryDelay     time.Duration
	Timeout        time.Duration
	MaxBandwidth   int64 // bytes per second, 0 = unlimited

	// HTTP settings
	Headers map[string]string
	Cookies string

	// Encryption
	DecryptionKeys []string

	// Track selection
	TrackSelector string

	// Muxer backend
	MuxerBackend string // ffmpeg, binary, auto

	// UI/Logging
	NoProgress  bool
	Verbose     bool
	ShowVersion bool
}

// Default configuration values.
const (
	DefaultThreads       = 16
	DefaultFormat        = "mp4"
	DefaultMuxerBackend  = "auto"
	DefaultRetryAttempts = 3
	DefaultRetryDelay    = time.Second
	DefaultTimeout       = 30 * time.Second
	DefaultTrackSelector = "best"

	MaxThreads = 128
	MinThreads = 1
)

// New returns a Config with sensible defaults.
func New() *Config {
	return &Config{
		Threads:       DefaultThreads,
		Format:        DefaultFormat,
		MuxerBackend:  DefaultMuxerBackend,
		RetryAttempts: DefaultRetryAttempts,
		RetryDelay:    DefaultRetryDelay,
		Timeout:       DefaultTimeout,
		TrackSelector: DefaultTrackSelector,
		Headers:       make(map[string]string),
	}
}

// Validate checks if the configuration is valid and normalizes values.
func (c *Config) Validate() error {
	if c.URL == "" {
		return ErrMissingURL
	}

	// Clamp threads to valid range
	if c.Threads < MinThreads {
		c.Threads = MinThreads
	}
	if c.Threads > MaxThreads {
		c.Threads = MaxThreads
	}

	// Initialize headers map if nil
	if c.Headers == nil {
		c.Headers = make(map[string]string)
	}

	return nil
}
