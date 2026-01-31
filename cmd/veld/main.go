package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"github.com/mohaanymo/veld/config"
	"github.com/mohaanymo/veld/engine"
	"github.com/mohaanymo/veld/parser"
	"github.com/mohaanymo/veld/tui"

	tea "github.com/charmbracelet/bubbletea"
)

var (
	version = "1.0.0"
	commit  = "dev"
)

func main() {
	cfg := parseFlags()

	if cfg.ShowVersion {
		fmt.Printf("veld %s (%s)\n", version, commit)
		os.Exit(0)
	}

	if cfg.URL == "" {
		fmt.Fprintln(os.Stderr, "Error: --url is required")
		flag.Usage()
		os.Exit(1)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Handle graceful shutdown
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sigCh
		cancel()
	}()

	if err := run(ctx, cfg); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}

func parseFlags() *config.Config {
	cfg := config.New()

	var headers headerFlags
	var threads int

	// Core options
	flag.StringVar(&cfg.URL, "url", "", "")
	flag.StringVar(&cfg.URL, "u", "", "")
	flag.StringVar(&cfg.OutputPath, "output", "", "")
	flag.StringVar(&cfg.OutputPath, "o", "", "")
	flag.IntVar(&threads, "threads", config.DefaultThreads, "")
	flag.IntVar(&threads, "n", config.DefaultThreads, "")
	flag.BoolVar(&cfg.ParallelTracks, "parallel-tracks", false, "")
	flag.BoolVar(&cfg.ParallelTracks, "P", false, "")
	flag.Var(&headers, "header", "")
	flag.Var(&headers, "H", "")
	flag.StringVar(&cfg.Cookies, "cookie", "", "")
	flag.StringVar(&cfg.DecryptionKey, "key", "", "")
	flag.StringVar(&cfg.TrackSelector, "select-track", "", "")
	flag.StringVar(&cfg.TrackSelector, "s", "", "")
	flag.StringVar(&cfg.Format, "format", config.DefaultFormat, "")
	flag.StringVar(&cfg.Format, "f", config.DefaultFormat, "")
	flag.StringVar(&cfg.MuxerBackend, "muxer", config.DefaultMuxerBackend, "")
	flag.BoolVar(&cfg.NoProgress, "no-progress", false, "")
	flag.BoolVar(&cfg.Verbose, "verbose", false, "")
	flag.BoolVar(&cfg.Verbose, "v", false, "")
	flag.BoolVar(&cfg.ShowVersion, "version", false, "")

	flag.Usage = printUsage
	flag.Parse()

	cfg.Threads = threads

	// If no track selector provided, show interactive picker
	if cfg.TrackSelector == "" {
		cfg.TrackSelector = "interactive"
	}

	// Parse headers
	for _, h := range headers {
		parts := strings.SplitN(h, ":", 2)
		if len(parts) == 2 {
			cfg.Headers[strings.TrimSpace(parts[0])] = strings.TrimSpace(parts[1])
		}
	}
	return cfg
}

func printUsage() {
	fmt.Fprintf(os.Stderr, `veld - Video Element Downloader: High-performance HLS/DASH media downloader

Usage: veld [options] -u <URL>

Options:
  -u, --url <URL>           Stream URL (m3u8 or mpd) [required]
  -o, --output <path>       Output file path (default: output.mp4)
  -n, --threads <num>       Concurrent downloads (default: 16)
  -s, --select-track <sel>  Track selection (omit for interactive picker)
  -P, --parallel-tracks     Download all tracks concurrently
  -f, --format <fmt>        Output format: mp4, mkv, ts (default: mp4)
  -H, --header <header>     Custom header (repeatable)
      --cookie <cookies>    Cookies for requests
      --key <KID:KEY>       Decryption key
      --muxer <backend>     Muxer: auto, ffmpeg, binary (default: auto)
      --no-progress         Disable TUI progress
  -v, --verbose             Verbose output
      --version             Show version

Track Selection (-s):
  If omitted, an interactive picker will be shown.
  Presets:
    best                Best video + best audio
    all                 All tracks
    1080p, 720p, etc    Video by resolution + best audio
    video:0+audio:1     By index

Examples:
  veld -u https://example.com/video.m3u8           # Interactive picker
  veld -u https://example.com/video.m3u8 -s best   # Auto-select best
  veld -u https://example.com/video.mpd -s 1080p   # 1080p video
`)
}

func run(ctx context.Context, cfg *config.Config) error {
	parserRegistry := parser.NewRegistry()

	if cfg.Verbose {
		fmt.Printf("Parsing manifest: %s\n", cfg.URL)
	}
	manifest, err := parserRegistry.Parse(ctx, cfg.URL, cfg.Headers)
	if err != nil {
		return fmt.Errorf("failed to parse manifest: %w", err)
	}

	fmt.Printf("Found %d tracks\n", len(manifest.Tracks))

	eng, err := engine.New(cfg)
	if err != nil {
		return fmt.Errorf("failed to create engine: %w", err)
	}
	defer eng.Close()

	// Handle track selection
	if cfg.TrackSelector == "interactive" {
		picker := tui.NewTrackPicker(manifest.Tracks)
		p := tea.NewProgram(picker, tea.WithAltScreen())
		if _, err := p.Run(); err != nil {
			return fmt.Errorf("track picker error: %w", err)
		}

		result := picker.Result()
		if result.Canceled {
			fmt.Println("Canceled")
			return nil
		}
		if len(result.Selected) == 0 {
			return fmt.Errorf("no tracks selected")
		}
		eng.SelectedTracks = result.Selected
	} else {
		if err := eng.SelectTracks(manifest); err != nil {
			return fmt.Errorf("failed to select tracks: %w", err)
		}
	}

	fmt.Printf("Selected %d tracks\n", len(eng.SelectedTracks))
	for _, t := range eng.SelectedTracks {
		fmt.Printf("  - %s: %s %s\n", t.Type, t.Resolution.QualityLabel(), t.Codec)
	}

	// Pre-load segments for lazy-loaded tracks before TUI
	for _, track := range eng.SelectedTracks {
		if track.MediaPlaylistURL != "" && len(track.Segments) == 0 {
			if err := eng.LoadTrackSegments(ctx, track); err != nil {
				return fmt.Errorf("load segments for %s: %w", track.ID, err)
			}
		}
	}

	if cfg.NoProgress {
		err := eng.Download(ctx, manifest)
		if err != nil {
			return err
		}
		printOutputPath(cfg)
		return nil
	}

	// Run with TUI
	model := tui.NewModel(eng, manifest, cfg)
	p := tea.NewProgram(model, tea.WithAltScreen())

	var downloadErr error
	go func() {
		if err := eng.Download(ctx, manifest); err != nil {
			downloadErr = err
			p.Send(tui.ErrorMsg{Err: err})
		} else {
			p.Send(tui.DoneMsg{})
		}
	}()

	if _, err := p.Run(); err != nil {
		return fmt.Errorf("TUI error: %w", err)
	}

	if downloadErr != nil {
		return downloadErr
	}

	printOutputPath(cfg)
	return nil
}

func printOutputPath(cfg *config.Config) {
	output := cfg.OutputPath
	if output == "" {
		output = "output"
	}
	if !strings.HasSuffix(strings.ToLower(output), "."+cfg.Format) {
		output = output + "." + cfg.Format
	}
	fmt.Printf("\n✓ Saved to: %s\n", output)
}

// headerFlags implements flag.Value for repeatable header flags
type headerFlags []string

func (h *headerFlags) String() string {
	return strings.Join(*h, ", ")
}

func (h *headerFlags) Set(value string) error {
	*h = append(*h, value)
	return nil
}
