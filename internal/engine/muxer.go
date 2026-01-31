package engine

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/mohaanymo/veld/config"
	"github.com/mohaanymo/veld/models"
)

// AutoMuxer automatically selects the best muxer based on availability.
type AutoMuxer struct {
	ffmpegPath string
	tempDir    string
	backend    string
	verbose    bool
}

// NewAutoMuxer creates a new auto-selecting muxer.
func NewAutoMuxer(cfg *config.Config) *AutoMuxer {
	m := &AutoMuxer{
		tempDir: os.TempDir(),
		backend: cfg.MuxerBackend,
		verbose: cfg.Verbose,
	}

	if path, err := exec.LookPath("ffmpeg"); err == nil {
		m.ffmpegPath = path
	}

	return m
}

// Mux combines tracks into the output file.
func (m *AutoMuxer) Mux(ctx context.Context, tracks []*models.Track, outputPath string, format ContainerFormat) error {
	if len(tracks) == 0 {
		return fmt.Errorf("no tracks to mux")
	}

	// Determine output path with proper extension
	if outputPath == "" {
		outputPath = "output"
	}

	ext := "." + string(format)
	if !strings.HasSuffix(strings.ToLower(outputPath), ext) {
		outputPath = outputPath + ext
	}

	if !filepath.IsAbs(outputPath) {
		cwd, _ := os.Getwd()
		outputPath = filepath.Join(cwd, outputPath)
	}

	outputDir := filepath.Dir(outputPath)
	if err := os.MkdirAll(outputDir, 0755); err != nil {
		return fmt.Errorf("create output dir: %w", err)
	}

	baseName := strings.TrimSuffix(filepath.Base(outputPath), ext)

	// Separate media tracks from subtitles
	var mediaTracks []*models.Track
	var subtitleTracks []*models.Track
	for _, t := range tracks {
		if t.IsSubtitle() {
			subtitleTracks = append(subtitleTracks, t)
		} else {
			mediaTracks = append(mediaTracks, t)
		}
	}

	// Save subtitle tracks as separate files
	for _, sub := range subtitleTracks {
		subPath := m.subtitlePath(outputDir, baseName, sub)
		if err := m.saveSubtitle(sub, subPath); err != nil {
			if m.verbose {
				fmt.Printf("Warning: failed to save subtitle %s: %v\n", sub.ID, err)
			}
		} else {
			fmt.Printf("✓ Subtitle saved: %s\n", subPath)
		}
	}

	if len(mediaTracks) == 0 {
		return nil
	}

	if m.verbose {
		fmt.Printf("Muxing %d media tracks to: %s\n", len(mediaTracks), outputPath)
	}

	// Concatenate segments for each track to temp files
	tempFiles := make([]string, 0, len(mediaTracks))
	defer func() {
		for _, f := range tempFiles {
			os.Remove(f)
		}
	}()

	for i, track := range mediaTracks {
		tempPath := filepath.Join(m.tempDir, fmt.Sprintf("veld_track_%d_%s.tmp", i, sanitizeID(track.ID)))
		tempFiles = append(tempFiles, tempPath)

		if err := m.concatSegments(track, tempPath); err != nil {
			return fmt.Errorf("concat track %s: %w", track.ID, err)
		}

		if m.verbose {
			info, _ := os.Stat(tempPath)
			if info != nil {
				fmt.Printf("Track %s (%s): %d bytes\n", track.ID, track.Type, info.Size())
			}
		}
	}

	// Use FFmpeg if available
	if m.ffmpegPath != "" && (m.backend == "auto" || m.backend == "ffmpeg") {
		return m.muxWithFFmpeg(ctx, tempFiles, mediaTracks, outputPath, format)
	}

	// Binary concat for single track or TS format
	if len(mediaTracks) == 1 || format == FormatTS {
		return m.binaryCopy(tempFiles[0], outputPath)
	}

	return fmt.Errorf("FFmpeg required for multi-track muxing to %s", format)
}

// subtitlePath generates a path for a subtitle file.
func (m *AutoMuxer) subtitlePath(dir, baseName string, sub *models.Track) string {
	ext := getSubtitleExt(sub.Codec)
	lang := sub.Language
	if lang == "" {
		lang = "sub"
	}
	return filepath.Join(dir, fmt.Sprintf("%s.%s%s", baseName, lang, ext))
}

// getSubtitleExt returns appropriate extension for subtitle codec.
func getSubtitleExt(codec string) string {
	codec = strings.ToLower(codec)
	switch {
	case strings.Contains(codec, "vtt"), strings.Contains(codec, "webvtt"), strings.Contains(codec, "wvtt"):
		return ".vtt"
	case strings.Contains(codec, "ttml"), strings.Contains(codec, "stpp"):
		return ".ttml"
	case strings.Contains(codec, "srt"):
		return ".srt"
	default:
		return ".vtt"
	}
}

// saveSubtitle writes subtitle data to a file.
func (m *AutoMuxer) saveSubtitle(track *models.Track, path string) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()

	for _, seg := range track.Segments {
		if len(seg.Data) > 0 {
			f.Write(seg.Data)
		}
	}
	return nil
}

// sanitizeID makes track ID safe for filenames.
func sanitizeID(id string) string {
	replacer := strings.NewReplacer("/", "_", "\\", "_", ":", "_", " ", "_")
	return replacer.Replace(id)
}

// concatSegments writes all segments of a track to a single file.
func (m *AutoMuxer) concatSegments(track *models.Track, outputPath string) error {
	f, err := os.Create(outputPath)
	if err != nil {
		return fmt.Errorf("create temp file: %w", err)
	}
	defer f.Close()

	bytesWritten := int64(0)

	// Write init segment first (required for fMP4)
	if track.InitSegment != nil && len(track.InitSegment.Data) > 0 {
		n, err := f.Write(track.InitSegment.Data)
		if err != nil {
			return fmt.Errorf("write init segment: %w", err)
		}
		bytesWritten += int64(n)
		if m.verbose {
			fmt.Printf("  Init segment for %s: %d bytes\n", track.ID, n)
		}
	}

	// Write media segments in order
	for i, seg := range track.Segments {
		if len(seg.Data) == 0 {
			if m.verbose {
				fmt.Printf("  Warning: segment %d has no data\n", i)
			}
			continue
		}
		n, err := f.Write(seg.Data)
		if err != nil {
			return fmt.Errorf("write segment %d: %w", i, err)
		}
		bytesWritten += int64(n)
	}

	if bytesWritten == 0 {
		return fmt.Errorf("no data written for track %s", track.ID)
	}

	return nil
}

// muxWithFFmpeg uses FFmpeg to mux tracks.
// FIXED: Use -map 0 -map 1 etc. to map ALL streams from each input, not just stream 0.
func (m *AutoMuxer) muxWithFFmpeg(ctx context.Context, inputFiles []string, tracks []*models.Track, output string, format ContainerFormat) error {
	args := []string{"-y", "-hide_banner"}

	if !m.verbose {
		args = append(args, "-loglevel", "error")
	} else {
		args = append(args, "-loglevel", "info")
	}

	// Add inputs
	for _, f := range inputFiles {
		args = append(args, "-i", f)
	}

	// Copy codecs (no re-encoding)
	args = append(args, "-c", "copy")

	// CRITICAL FIX: Map ALL streams from each input file
	// Using "-map N" maps all streams from input N, not just stream 0
	// This ensures audio tracks are properly included in the output
	for i := range inputFiles {
		args = append(args, "-map", fmt.Sprintf("%d", i))
	}

	// For MP4/MOV, use faststart for web playback
	if format == FormatMP4 {
		args = append(args, "-movflags", "+faststart")
	}

	args = append(args, output)

	if m.verbose {
		fmt.Printf("FFmpeg command: %s %s\n", m.ffmpegPath, strings.Join(args, " "))
	}

	cmd := exec.CommandContext(ctx, m.ffmpegPath, args...)
	var stderr strings.Builder
	cmd.Stderr = &stderr
	if m.verbose {
		cmd.Stderr = os.Stderr
	}

	if err := cmd.Run(); err != nil {
		if !m.verbose {
			return fmt.Errorf("%w: %s", err, stderr.String())
		}
		return err
	}

	return nil
}

// binaryCopy copies a file.
func (m *AutoMuxer) binaryCopy(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()

	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer out.Close()

	_, err = io.Copy(out, in)
	return err
}

// SupportedFormats returns supported output formats.
func (m *AutoMuxer) SupportedFormats() []ContainerFormat {
	if m.ffmpegPath != "" {
		return []ContainerFormat{FormatMP4, FormatMKV, FormatTS, FormatWebM}
	}
	return []ContainerFormat{FormatTS}
}
