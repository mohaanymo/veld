package veld

import (
	"fmt"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// Color palette (Tokyonight theme)
var (
	colorBg        = lipgloss.Color("#1a1b26")
	colorBorder    = lipgloss.Color("#414868")
	colorMuted     = lipgloss.Color("#565f89")
	colorSubtle    = lipgloss.Color("#787c99")
	colorText      = lipgloss.Color("#a9b1d6")
	colorPrimary   = lipgloss.Color("#7aa2f7")
	colorSuccess   = lipgloss.Color("#9ece6a")
	colorWarning   = lipgloss.Color("#e0af68")
	colorSecondary = lipgloss.Color("#bb9af7")
	colorAccent    = lipgloss.Color("#7dcfff")
	colorRose      = lipgloss.Color("#f7768e")
)

// Styles
var (
	uiHeaderStyle = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(colorBorder).
			Padding(0, 2)

	uiTitleStyle = lipgloss.NewStyle().
			Foreground(colorPrimary).
			Bold(true)

	uiSubtitleStyle = lipgloss.NewStyle().
			Foreground(colorAccent).
			Bold(true)

	uiContentStyle = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(colorBorder).
			Padding(1, 2)

	uiNormalStyle = lipgloss.NewStyle().
			Foreground(colorText)

	uiDimStyle = lipgloss.NewStyle().
			Foreground(colorMuted)

	uiSuccessStyle = lipgloss.NewStyle().
			Foreground(colorSuccess).
			Bold(true)

	uiErrorStyle = lipgloss.NewStyle().
			Foreground(colorRose).
			Bold(true)

	uiWarningStyle = lipgloss.NewStyle().
			Foreground(colorWarning)

	uiHelpStyle = lipgloss.NewStyle().
			Foreground(colorMuted)

	uiKeyHelpStyle = lipgloss.NewStyle().
			Foreground(colorSubtle)

	uiSpinnerStyle = lipgloss.NewStyle().
			Foreground(colorPrimary)

	uiProgressActive = lipgloss.NewStyle().
			Foreground(colorPrimary)

	uiProgressDone = lipgloss.NewStyle().
			Foreground(colorSuccess)

	uiProgressWait = lipgloss.NewStyle().
			Foreground(colorMuted)

	uiVideoBadge = lipgloss.NewStyle().
			Foreground(colorBg).
			Background(colorPrimary).
			Padding(0, 1).
			Bold(true)

	uiAudioBadge = lipgloss.NewStyle().
			Foreground(colorBg).
			Background(colorSecondary).
			Padding(0, 1).
			Bold(true)

	uiStateLabelStyle = lipgloss.NewStyle().
				Foreground(colorSubtle)

	uiStateValueStyle = lipgloss.NewStyle().
				Foreground(colorAccent).
				Bold(true)

	uiSelectedStyle = lipgloss.NewStyle().
			Foreground(colorAccent).
			Bold(true)
)

var uiSpinner = []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}

// ManagerUI provides a beautiful terminal UI for the download manager.
type ManagerUI struct {
	manager  *Manager
	program  *tea.Program
	model    *managerModel
	running  bool
}

// NewManagerUI creates a new manager UI.
func NewManagerUI(manager *Manager) *ManagerUI {
	return &ManagerUI{
		manager: manager,
	}
}

// Run starts the TUI and blocks until it exits.
func (ui *ManagerUI) Run() error {
	ui.model = newManagerModel(ui.manager)
	ui.program = tea.NewProgram(ui.model, tea.WithAltScreen())
	ui.running = true

	// Set up callbacks to refresh UI
	ui.manager.onProgress = func(task *Task) {
		if ui.program != nil {
			ui.program.Send(refreshMsg{})
		}
	}
	ui.manager.onStateChange = func(task *Task) {
		if ui.program != nil {
			ui.program.Send(refreshMsg{})
		}
	}

	_, err := ui.program.Run()
	ui.running = false
	return err
}

// Refresh forces a UI refresh.
func (ui *ManagerUI) Refresh() {
	if ui.program != nil && ui.running {
		ui.program.Send(refreshMsg{})
	}
}

// Messages
type (
	refreshMsg  struct{}
	uiTickMsg   time.Time
)

type managerModel struct {
	manager      *Manager
	width        int
	height       int
	frame        int
	cursor       int
	scrollOffset int
}

func newManagerModel(m *Manager) *managerModel {
	return &managerModel{
		manager: m,
		width:   80,
		height:  24,
	}
}

func (m *managerModel) Init() tea.Cmd {
	return tea.Batch(m.tick(), tea.EnterAltScreen)
}

func (m *managerModel) tick() tea.Cmd {
	return tea.Tick(100*time.Millisecond, func(t time.Time) tea.Msg {
		return uiTickMsg(t)
	})
}

func (m *managerModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "ctrl+c", "q":
			return m, tea.Quit
		case "up", "k":
			if m.cursor > 0 {
				m.cursor--
				m.adjustScroll()
			}
		case "down", "j":
			tasks := m.manager.GetAllTasks()
			if m.cursor < len(tasks)-1 {
				m.cursor++
				m.adjustScroll()
			}
		case "c":
			// Cancel selected task
			tasks := m.manager.GetAllTasks()
			if m.cursor < len(tasks) {
				m.manager.CancelTask(tasks[m.cursor].ID)
			}
		case "r":
			// Remove completed/failed/canceled task
			tasks := m.manager.GetAllTasks()
			if m.cursor < len(tasks) {
				m.manager.RemoveTask(tasks[m.cursor].ID)
				if m.cursor >= len(m.manager.GetAllTasks()) && m.cursor > 0 {
					m.cursor--
				}
			}
		}

	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height

	case uiTickMsg:
		m.frame++
		return m, m.tick()

	case refreshMsg:
		// Just trigger a re-render
	}

	return m, nil
}

func (m *managerModel) adjustScroll() {
	visibleRows := m.height - 15
	if visibleRows < 5 {
		visibleRows = 5
	}

	if m.cursor < m.scrollOffset {
		m.scrollOffset = m.cursor
	}
	if m.cursor >= m.scrollOffset+visibleRows {
		m.scrollOffset = m.cursor - visibleRows + 1
	}
}

func (m *managerModel) View() string {
	w := m.width - 4
	if w < 60 {
		w = 60
	}
	if w > 100 {
		w = 100
	}

	var b strings.Builder
	b.WriteString(m.viewHeader(w))
	b.WriteString("\n\n")
	b.WriteString(m.viewTasks(w))

	return b.String()
}

func (m *managerModel) viewHeader(w int) string {
	title := uiTitleStyle.Render("⚡ veld")
	subtitle := uiDimStyle.Render(" - Download Manager")

	stats := m.manager.Stats()

	line1 := title + subtitle
	line2 := fmt.Sprintf("%s %s  %s %s  %s %s  %s %s",
		uiStateLabelStyle.Render("active:"),
		uiStateValueStyle.Render(fmt.Sprintf("%d", stats.Active)),
		uiStateLabelStyle.Render("pending:"),
		uiNormalStyle.Render(fmt.Sprintf("%d", stats.Pending)),
		uiStateLabelStyle.Render("done:"),
		uiSuccessStyle.Render(fmt.Sprintf("%d", stats.Completed)),
		uiStateLabelStyle.Render("failed:"),
		uiErrorStyle.Render(fmt.Sprintf("%d", stats.Failed)),
	)

	return uiHeaderStyle.Width(w).Render(line1 + "\n" + line2)
}

func (m *managerModel) viewTasks(w int) string {
	var b strings.Builder

	b.WriteString(uiSubtitleStyle.Render("Downloads"))
	b.WriteString("\n\n")

	tasks := m.manager.GetAllTasks()

	if len(tasks) == 0 {
		b.WriteString(uiDimStyle.Render("  No downloads queued"))
		b.WriteString("\n\n")
		b.WriteString(uiDimStyle.Render("  Add tasks using manager.AddTask()"))
		b.WriteString("\n")
	} else {
		visibleRows := m.height - 15
		if visibleRows < 5 {
			visibleRows = 5
		}

		for i := m.scrollOffset; i < len(tasks) && i < m.scrollOffset+visibleRows; i++ {
			b.WriteString(m.renderTask(tasks[i], i == m.cursor, w-6))
			b.WriteString("\n")
		}

		if len(tasks) > visibleRows {
			b.WriteString(uiDimStyle.Render(fmt.Sprintf("\n  %d/%d tasks", min(m.scrollOffset+visibleRows, len(tasks)), len(tasks))))
		}
	}

	b.WriteString("\n\n")
	b.WriteString(uiHelpStyle.Render(
		uiKeyHelpStyle.Render("↑/↓") + " navigate  " +
			uiKeyHelpStyle.Render("c") + " cancel  " +
			uiKeyHelpStyle.Render("r") + " remove  " +
			uiKeyHelpStyle.Render("q") + " quit",
	))

	return uiContentStyle.Width(w).Render(b.String())
}

func (m *managerModel) renderTask(task *Task, isCursor bool, w int) string {
	task.mu.RLock()
	defer task.mu.RUnlock()

	var b strings.Builder

	// Cursor indicator
	if isCursor {
		b.WriteString(uiSelectedStyle.Render("▸ "))
	} else {
		b.WriteString("  ")
	}

	// State indicator
	switch task.State {
	case TaskPending:
		b.WriteString(uiDimStyle.Render("◯ "))
	case TaskParsing:
		b.WriteString(uiSpinnerStyle.Render(uiSpinner[m.frame%len(uiSpinner)] + " "))
	case TaskDownloading:
		b.WriteString(uiSpinnerStyle.Render(uiSpinner[m.frame%len(uiSpinner)] + " "))
	case TaskMuxing:
		b.WriteString(uiWarningStyle.Render("⚙ "))
	case TaskCompleted:
		b.WriteString(uiSuccessStyle.Render("✓ "))
	case TaskFailed:
		b.WriteString(uiErrorStyle.Render("✗ "))
	case TaskCanceled:
		b.WriteString(uiDimStyle.Render("⊘ "))
	}

	// Task ID/Name
	name := task.ID
	if len(name) > 25 {
		name = name[:22] + "..."
	}
	if isCursor {
		b.WriteString(uiSelectedStyle.Render(fmt.Sprintf("%-25s", name)))
	} else {
		b.WriteString(uiNormalStyle.Render(fmt.Sprintf("%-25s", name)))
	}
	b.WriteString(" ")

	// Progress bar (for active downloads)
	if task.State == TaskDownloading || task.State == TaskMuxing {
		barWidth := 20
		pct := task.Progress.Percent()
		filled := int(pct / 100 * float64(barWidth))
		if filled > barWidth {
			filled = barWidth
		}
		empty := barWidth - filled

		bar := uiProgressActive.Render(strings.Repeat("█", filled)) +
			uiProgressWait.Render(strings.Repeat("░", empty))
		b.WriteString(bar)
		b.WriteString(" ")
		b.WriteString(uiStateValueStyle.Render(fmt.Sprintf("%5.1f%%", pct)))
		b.WriteString(" ")

		// Speed and ETA
		if task.Progress.Speed > 0 {
			b.WriteString(uiDimStyle.Render(fmt.Sprintf("%s/s", formatBytes(int64(task.Progress.Speed)))))
		}
		if task.Progress.ETA > 0 {
			b.WriteString(uiDimStyle.Render(fmt.Sprintf(" ETA: %s", formatDuration(task.Progress.ETA))))
		}
	} else if task.State == TaskPending {
		b.WriteString(uiDimStyle.Render("waiting..."))
	} else if task.State == TaskParsing {
		b.WriteString(uiDimStyle.Render("parsing manifest..."))
	} else if task.State == TaskCompleted {
		duration := task.CompletedAt.Sub(task.StartedAt)
		b.WriteString(uiSuccessStyle.Render("completed"))
		b.WriteString(uiDimStyle.Render(fmt.Sprintf(" in %s", formatDuration(duration))))
	} else if task.State == TaskFailed {
		errMsg := "unknown error"
		if task.Error != nil {
			errMsg = task.Error.Error()
			if len(errMsg) > 30 {
				errMsg = errMsg[:27] + "..."
			}
		}
		b.WriteString(uiErrorStyle.Render(errMsg))
	} else if task.State == TaskCanceled {
		b.WriteString(uiDimStyle.Render("canceled"))
	}

	// Track info (on second line for active downloads)
	if (task.State == TaskDownloading || task.State == TaskMuxing) && len(task.SelectedTracks) > 0 {
		b.WriteString("\n      ")
		for i, t := range task.SelectedTracks {
			if i > 0 {
				b.WriteString(" ")
			}
			if t.IsVideo() {
				b.WriteString(uiVideoBadge.Render(t.QualityLabel()))
			} else if t.IsAudio() {
				label := "AUDIO"
				if t.Language() != "" {
					label = t.Language()
				}
				b.WriteString(uiAudioBadge.Render(label))
			}
		}
		b.WriteString(uiDimStyle.Render(fmt.Sprintf("  %d/%d segs",
			task.Progress.CompletedSegments, task.Progress.TotalSegments)))
	}

	return b.String()
}

// Helper functions
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
	return fmt.Sprintf("%.1f %cB", float64(b)/float64(div), "KMGTPE"[exp])
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
		return fmt.Sprintf("%dh%02dm", h, m)
	}
	if m > 0 {
		return fmt.Sprintf("%dm%02ds", m, s)
	}
	return fmt.Sprintf("%ds", s)
}

func uiMin(a, b int) int {
	if a < b {
		return a
	}
	return b
}