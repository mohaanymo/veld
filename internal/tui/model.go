package tui

import (
	"fmt"
	"strings"
	"time"

	"github.com/mohaanymo/veld/internal/config"
	"github.com/mohaanymo/veld/internal/engine"
	"github.com/mohaanymo/veld/internal/models"

	tea "github.com/charmbracelet/bubbletea"
)

// Messages
type (
	progressMsg engine.ProgressUpdate
	tickMsg     time.Time
	DoneMsg     struct{}
	ErrorMsg    struct{ Err error }
)

// States
type appState int

const (
	stateStarting appState = iota
	stateDownloading
	stateMuxing
	stateDone
	stateError
)

type trackProgress struct {
	track         *models.Track
	totalSegments int
	doneSegments  int
	downloadBytes int64
}

// Model is the main TUI model.
type Model struct {
	state      appState
	width      int
	height     int
	frame      int
	engine     *engine.Engine
	manifest   *models.Manifest
	cfg        *config.Config
	progressCh <-chan engine.ProgressUpdate

	tracks        map[string]*trackProgress
	trackOrder    []string
	totalSegments int
	doneSegments  int
	downloaded    int64
	startTime     time.Time
	speed         float64
	eta           time.Duration
	err           error
}

// NewModel creates a new TUI model.
func NewModel(eng *engine.Engine, manifest *models.Manifest, cfg *config.Config) *Model {
	tracks := make(map[string]*trackProgress)
	trackOrder := make([]string, 0)
	totalSegments := 0

	selectedTracks := eng.SelectedTracks
	if selectedTracks == nil {
		selectedTracks = manifest.Tracks
	}

	for _, track := range selectedTracks {
		tp := &trackProgress{
			track:         track,
			totalSegments: len(track.Segments),
		}
		tracks[track.ID] = tp
		trackOrder = append(trackOrder, track.ID)
		totalSegments += len(track.Segments)
	}

	return &Model{
		engine:        eng,
		manifest:      manifest,
		cfg:           cfg,
		progressCh:    eng.Progress(),
		tracks:        tracks,
		trackOrder:    trackOrder,
		totalSegments: totalSegments,
		startTime:     time.Now(),
		state:         stateStarting,
		width:         80,
		height:        24,
	}
}

func (m *Model) Init() tea.Cmd {
	return tea.Batch(m.listenProgress(), tick())
}

func (m *Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "ctrl+c", "q":
			return m, tea.Quit
		}

	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height

	case progressMsg:
		m.handleProgress(engine.ProgressUpdate(msg))
		m.state = stateDownloading
		return m, m.listenProgress()

	case tickMsg:
		m.frame++
		m.updateSpeed()
		return m, tick()

	case DoneMsg:
		m.state = stateDone
		return m, tea.Quit

	case ErrorMsg:
		m.state = stateError
		m.err = msg.Err
		return m, tea.Quit
	}

	return m, nil
}

func (m *Model) View() string {
	w := clamp(m.width-4, 60, 100)

	var b strings.Builder
	b.WriteString(m.viewHeader(w))
	b.WriteString("\n\n")
	b.WriteString(m.viewContent(w))

	return b.String()
}

func (m *Model) viewHeader(w int) string {
	title := titleStyle.Render("⚡ veld")
	subtitle := dimStyle.Render(" - Video Element Downloader")

	typeLabel := labelStyle.Render("type:")
	typeValue := valueStyle.Render(m.manifest.Type.String())

	urlLabel := labelStyle.Render("url:")
	urlValue := dimStyle.Render(truncate(m.manifest.URL, w-30))

	line1 := title + subtitle
	line2 := fmt.Sprintf("%s %s  %s %s", typeLabel, typeValue, urlLabel, urlValue)

	return headerStyle.Width(w).Render(line1 + "\n" + line2)
}

func (m *Model) viewContent(w int) string {
	var b strings.Builder

	b.WriteString(subtitleStyle.Render("Tracks"))
	b.WriteString("\n\n")

	for _, trackID := range m.trackOrder {
		tp := m.tracks[trackID]
		b.WriteString(m.renderTrack(tp, w-6))
		b.WriteString("\n")
	}

	b.WriteString("\n")
	b.WriteString(subtitleStyle.Render("Progress"))
	b.WriteString("\n\n")
	b.WriteString(m.renderOverallProgress(w - 6))
	b.WriteString("\n\n")
	b.WriteString(m.renderStats())
	b.WriteString("\n\n")
	b.WriteString(m.renderStatus())
	b.WriteString("\n")
	b.WriteString(m.renderHelp())

	return contentStyle.Width(w).Render(b.String())
}

func (m *Model) renderTrack(tp *trackProgress, w int) string {
	var b strings.Builder

	// Badge
	if tp.track.IsSubtitle() {
		b.WriteString(subtitleBadge.Render("SUB"))
	} else if tp.track.IsAudio() {
		b.WriteString(audioBadge.Render("AUDIO"))
	} else {
		b.WriteString(videoBadge.Render("VIDEO"))
	}
	b.WriteString(" ")

	// Track info
	info := tp.track.ID
	if tp.track.Resolution.Height > 0 {
		info = tp.track.Resolution.QualityLabel()
	}
	if tp.track.Codec != "" {
		info += " " + dimStyle.Render("•") + " " + tp.track.Codec
	}
	if tp.track.Language != "" {
		info += " " + dimStyle.Render("•") + " " + tp.track.Language
	}
	b.WriteString(normalStyle.Render(fmt.Sprintf("%-20s", info)))
	b.WriteString(" ")

	// Progress bar
	barWidth := 30
	pct := 0.0
	if tp.totalSegments > 0 {
		pct = float64(tp.doneSegments) / float64(tp.totalSegments)
	}
	filled := int(pct * float64(barWidth))
	empty := barWidth - filled

	bar := progressActive.Render(strings.Repeat("█", filled)) +
		progressWait.Render(strings.Repeat("░", empty))
	b.WriteString(bar)
	b.WriteString(" ")

	b.WriteString(statValueStyle.Render(fmt.Sprintf("%3.0f%%", pct*100)))
	b.WriteString(dimStyle.Render(fmt.Sprintf(" (%d/%d)", tp.doneSegments, tp.totalSegments)))

	return b.String()
}

func (m *Model) renderOverallProgress(w int) string {
	var b strings.Builder

	pct := 0.0
	if m.totalSegments > 0 {
		pct = float64(m.doneSegments) / float64(m.totalSegments)
	}

	barWidth := clamp(w-20, 20, 80)
	filled := clamp(int(pct*float64(barWidth)), 0, barWidth)
	empty := barWidth - filled

	bar := progressActive.Render(strings.Repeat("█", filled)) +
		progressWait.Render(strings.Repeat("░", empty))
	b.WriteString(bar)
	b.WriteString(" ")
	b.WriteString(statValueStyle.Render(fmt.Sprintf("%.1f%%", pct*100)))

	return b.String()
}

func (m *Model) renderStats() string {
	stats := []struct {
		label string
		value string
	}{
		{"Speed", fmt.Sprintf("%.2f MB/s", m.speed/1024/1024)},
		{"Downloaded", formatBytes(m.downloaded)},
		{"Elapsed", formatDuration(time.Since(m.startTime))},
		{"ETA", formatDuration(m.eta)},
	}

	var parts []string
	for _, s := range stats {
		part := statLabelStyle.Render(s.label+": ") + statValueStyle.Render(s.value)
		parts = append(parts, part)
	}

	return strings.Join(parts, "  ")
}

func (m *Model) renderStatus() string {
	switch m.state {
	case stateStarting:
		return spinnerStyle.Render(spinner[m.frame%len(spinner)]) + dimStyle.Render(" starting...")
	case stateDownloading:
		return spinnerStyle.Render(spinner[m.frame%len(spinner)]) + dimStyle.Render(" downloading segments...")
	case stateMuxing:
		return spinnerStyle.Render(spinner[m.frame%len(spinner)]) + warningStyle.Render(" muxing tracks...")
	case stateDone:
		return successStyle.Render("✓ download complete!")
	case stateError:
		return errorStyle.Render(fmt.Sprintf("✗ error: %v", m.err))
	}
	return ""
}

func (m *Model) renderHelp() string {
	return helpStyle.Render(
		keyHelpStyle.Render("q") + " quit  " +
			keyHelpStyle.Render("ctrl+c") + " cancel",
	)
}

func (m *Model) handleProgress(p engine.ProgressUpdate) {
	if tp, ok := m.tracks[p.TrackID]; ok {
		if p.Completed {
			tp.doneSegments++
			m.doneSegments++
		}
		tp.downloadBytes += p.BytesLoaded
		m.downloaded += p.BytesLoaded
	}
}

func (m *Model) updateSpeed() {
	elapsed := time.Since(m.startTime).Seconds()
	if elapsed > 0 {
		m.speed = float64(m.downloaded) / elapsed
	}

	remaining := m.totalSegments - m.doneSegments
	if m.speed > 0 && remaining > 0 && m.doneSegments > 0 {
		avgSegSize := float64(m.downloaded) / float64(m.doneSegments)
		m.eta = time.Duration(float64(remaining) * avgSegSize / m.speed * float64(time.Second))
	}
}

func (m *Model) listenProgress() tea.Cmd {
	return func() tea.Msg {
		p, ok := <-m.progressCh
		if !ok {
			return DoneMsg{}
		}
		return progressMsg(p)
	}
}

func tick() tea.Cmd {
	return tea.Tick(100*time.Millisecond, func(t time.Time) tea.Msg {
		return tickMsg(t)
	})
}

// Helpers

func clamp(v, min, max int) int {
	if v < min {
		return min
	}
	if v > max {
		return max
	}
	return v
}

func truncate(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max-3] + "..."
}

func formatBytes(b int64) string {
	const unit = 1024
	if b < unit {
		return fmt.Sprintf("%d B", b)
	}
	div, exp := int64(unit), 0
	for n := b / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.2f %cB", float64(b)/float64(div), "KMGTPE"[exp])
}

func formatDuration(d time.Duration) string {
	if d < time.Second {
		return "0s"
	}
	d = d.Round(time.Second)
	h := d / time.Hour
	d -= h * time.Hour
	m := d / time.Minute
	d -= m * time.Minute
	s := d / time.Second

	if h > 0 {
		return fmt.Sprintf("%dh%02dm%02ds", h, m, s)
	}
	if m > 0 {
		return fmt.Sprintf("%dm%02ds", m, s)
	}
	return fmt.Sprintf("%ds", s)
}
