package tui

import (
	"fmt"
	"strings"

	"github.com/mohaanymo/veld/internal/models"

	tea "github.com/charmbracelet/bubbletea"
)

// TrackPickerResult is returned when track selection is complete.
type TrackPickerResult struct {
	Selected []*models.Track
	Canceled bool
}

// TrackPicker is a TUI for interactive track selection.
type TrackPicker struct {
	tracks       []*models.Track
	videos       []*models.Track
	audios       []*models.Track
	subtitles    []*models.Track
	selected     map[string]bool
	cursor       int
	scrollOffset int
	visibleRows  int
	width        int
	height       int
	done         bool
	canceled     bool
}

// NewTrackPicker creates a new track picker TUI.
func NewTrackPicker(tracks []*models.Track) *TrackPicker {
	tp := &TrackPicker{
		tracks:      tracks,
		selected:    make(map[string]bool),
		width:       80,
		height:      24,
		visibleRows: 15,
	}

	// Categorize tracks
	for _, t := range tracks {
		switch {
		case t.IsSubtitle():
			tp.subtitles = append(tp.subtitles, t)
		case t.IsAudio():
			tp.audios = append(tp.audios, t)
		default:
			tp.videos = append(tp.videos, t)
		}
	}

	// Pre-select best video and audio
	if len(tp.videos) > 0 {
		best := tp.videos[0]
		for _, v := range tp.videos {
			if v.Bandwidth > best.Bandwidth {
				best = v
			}
		}
		tp.selected[best.ID] = true
	}
	if len(tp.audios) > 0 {
		best := tp.audios[0]
		for _, a := range tp.audios {
			if a.Bandwidth > best.Bandwidth {
				best = a
			}
		}
		tp.selected[best.ID] = true
	}

	return tp
}

func (tp *TrackPicker) Init() tea.Cmd {
	return nil
}

func (tp *TrackPicker) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "ctrl+c", "q":
			tp.canceled = true
			tp.done = true
			return tp, tea.Quit

		case "enter":
			tp.done = true
			return tp, tea.Quit

		case "up", "k":
			if tp.cursor > 0 {
				tp.cursor--
				tp.adjustScroll()
			}

		case "down", "j":
			total := len(tp.videos) + len(tp.audios) + len(tp.subtitles)
			if tp.cursor < total-1 {
				tp.cursor++
				tp.adjustScroll()
			}

		case " ", "x":
			track := tp.getTrackAtCursor()
			if track != nil {
				tp.selected[track.ID] = !tp.selected[track.ID]
			}

		case "a":
			for _, t := range tp.audios {
				tp.selected[t.ID] = true
			}

		case "v":
			for _, t := range tp.videos {
				tp.selected[t.ID] = true
			}

		case "s":
			for _, t := range tp.subtitles {
				tp.selected[t.ID] = true
			}

		case "n":
			for k := range tp.selected {
				delete(tp.selected, k)
			}
		}

	case tea.WindowSizeMsg:
		tp.width = msg.Width
		tp.height = msg.Height
	}

	return tp, nil
}

func (tp *TrackPicker) getTrackAtCursor() *models.Track {
	if tp.cursor < len(tp.videos) {
		return tp.videos[tp.cursor]
	}
	audioIdx := tp.cursor - len(tp.videos)
	if audioIdx < len(tp.audios) {
		return tp.audios[audioIdx]
	}
	subIdx := tp.cursor - len(tp.videos) - len(tp.audios)
	if subIdx < len(tp.subtitles) {
		return tp.subtitles[subIdx]
	}
	return nil
}

func (tp *TrackPicker) adjustScroll() {
	if tp.cursor < tp.scrollOffset {
		tp.scrollOffset = tp.cursor
	}
	if tp.cursor >= tp.scrollOffset+tp.visibleRows {
		tp.scrollOffset = tp.cursor - tp.visibleRows + 1
	}
}

func (tp *TrackPicker) View() string {
	w := clamp(tp.width-4, 60, 100)

	var b strings.Builder

	title := titleStyle.Render("⚡ veld")
	subtitle := dimStyle.Render(" - Select Tracks")
	b.WriteString(headerStyle.Width(w).Render(title + subtitle))
	b.WriteString("\n\n")

	type trackItem struct {
		track   *models.Track
		badge   string
		section string
		idx     int
	}

	var allTracks []trackItem
	globalIdx := 0

	for _, v := range tp.videos {
		allTracks = append(allTracks, trackItem{v, "VIDEO", "Video Tracks", globalIdx})
		globalIdx++
	}
	for _, a := range tp.audios {
		allTracks = append(allTracks, trackItem{a, "AUDIO", "Audio Tracks", globalIdx})
		globalIdx++
	}
	for _, s := range tp.subtitles {
		allTracks = append(allTracks, trackItem{s, "SUB", "Subtitle Tracks", globalIdx})
		globalIdx++
	}

	total := len(allTracks)

	if tp.scrollOffset > 0 {
		b.WriteString(dimStyle.Render("  ↑ more tracks above"))
		b.WriteString("\n")
	}

	lastSection := ""
	visibleCount := 0
	for i := tp.scrollOffset; i < total && visibleCount < tp.visibleRows; i++ {
		item := allTracks[i]

		if item.section != lastSection {
			if lastSection != "" {
				b.WriteString("\n")
			}
			b.WriteString(subtitleStyle.Render(item.section))
			b.WriteString("\n\n")
			lastSection = item.section
		}

		isCursor := item.idx == tp.cursor
		isSelected := tp.selected[item.track.ID]
		b.WriteString(tp.renderTrackRow(item.track, isCursor, isSelected, item.badge))
		b.WriteString("\n")
		visibleCount++
	}
	b.WriteString("\n")

	if tp.scrollOffset+tp.visibleRows < total {
		b.WriteString(dimStyle.Render("  ↓ more tracks below"))
		b.WriteString("\n")
	}

	count := 0
	for _, v := range tp.selected {
		if v {
			count++
		}
	}
	b.WriteString(dimStyle.Render(fmt.Sprintf("Selected: %d tracks", count)))
	b.WriteString("\n\n")

	b.WriteString(helpStyle.Render(
		keyHelpStyle.Render("↑/↓") + " navigate  " +
			keyHelpStyle.Render("space") + " toggle  " +
			keyHelpStyle.Render("enter") + " confirm  " +
			keyHelpStyle.Render("q") + " cancel",
	))
	b.WriteString("\n")
	b.WriteString(helpStyle.Render(
		keyHelpStyle.Render("v") + " all video  " +
			keyHelpStyle.Render("a") + " all audio  " +
			keyHelpStyle.Render("s") + " all subs  " +
			keyHelpStyle.Render("n") + " none",
	))

	return contentStyle.Width(w).Render(b.String())
}

func (tp *TrackPicker) renderTrackRow(t *models.Track, cursor, selected bool, badge string) string {
	var b strings.Builder

	if cursor {
		b.WriteString(selectedStyle.Render("▸ "))
	} else {
		b.WriteString("  ")
	}

	if selected {
		b.WriteString(successStyle.Render("[✓] "))
	} else {
		b.WriteString(dimStyle.Render("[ ] "))
	}

	switch badge {
	case "VIDEO":
		b.WriteString(videoBadge.Render("VIDEO"))
	case "AUDIO":
		b.WriteString(audioBadge.Render("AUDIO"))
	case "SUB":
		b.WriteString(subtitleBadge.Render("SUB"))
	}
	b.WriteString(" ")

	if t.Resolution.Height > 0 {
		b.WriteString(valueStyle.Render(fmt.Sprintf("%-6s", t.Resolution.QualityLabel())))
	} else {
		b.WriteString(valueStyle.Render(fmt.Sprintf("%-6s", "")))
	}
	b.WriteString(" ")

	b.WriteString(normalStyle.Render(fmt.Sprintf("%-15s", t.Codec)))

	if t.Language != "" {
		b.WriteString(dimStyle.Render(" • "))
		b.WriteString(normalStyle.Render(t.Language))
	}

	if t.Bandwidth > 0 {
		b.WriteString(dimStyle.Render(" • "))
		b.WriteString(dimStyle.Render(formatBandwidth(t.Bandwidth)))
	}

	return b.String()
}

// Result returns the selected tracks.
func (tp *TrackPicker) Result() TrackPickerResult {
	if tp.canceled {
		return TrackPickerResult{Canceled: true}
	}

	var selected []*models.Track
	for _, t := range tp.tracks {
		if tp.selected[t.ID] {
			selected = append(selected, t)
		}
	}
	return TrackPickerResult{Selected: selected}
}

func formatBandwidth(bw int64) string {
	if bw >= 1000000 {
		return fmt.Sprintf("%.1f Mbps", float64(bw)/1000000)
	}
	if bw >= 1000 {
		return fmt.Sprintf("%.0f kbps", float64(bw)/1000)
	}
	return fmt.Sprintf("%d bps", bw)
}
