package tui

import "github.com/charmbracelet/lipgloss"

// Color palette (Tokyonight theme)
var (
	colorBg      = lipgloss.Color("#1a1b26")
	colorBorder  = lipgloss.Color("#414868")
	colorMuted   = lipgloss.Color("#565f89")
	colorSubtle  = lipgloss.Color("#787c99")
	colorText    = lipgloss.Color("#a9b1d6")

	colorPrimary   = lipgloss.Color("#7aa2f7")
	colorSuccess   = lipgloss.Color("#9ece6a")
	colorWarning   = lipgloss.Color("#e0af68")
	colorSecondary = lipgloss.Color("#bb9af7")
	colorAccent    = lipgloss.Color("#7dcfff")
	colorRose      = lipgloss.Color("#f7768e")
)

// Styles
var (
	headerStyle = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(colorBorder).
			Padding(0, 2)

	titleStyle = lipgloss.NewStyle().
			Foreground(colorPrimary).
			Bold(true)

	subtitleStyle = lipgloss.NewStyle().
			Foreground(colorAccent).
			Bold(true)

	labelStyle = lipgloss.NewStyle().
			Foreground(colorMuted)

	valueStyle = lipgloss.NewStyle().
			Foreground(colorText)

	contentStyle = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(colorBorder).
			Padding(1, 2)

	normalStyle = lipgloss.NewStyle().
			Foreground(colorText)

	dimStyle = lipgloss.NewStyle().
			Foreground(colorMuted)

	selectedStyle = lipgloss.NewStyle().
			Foreground(colorAccent).
			Bold(true)

	successStyle = lipgloss.NewStyle().
			Foreground(colorSuccess).
			Bold(true)

	errorStyle = lipgloss.NewStyle().
			Foreground(colorRose).
			Bold(true)

	warningStyle = lipgloss.NewStyle().
			Foreground(colorWarning)

	helpStyle = lipgloss.NewStyle().
			Foreground(colorMuted)

	keyHelpStyle = lipgloss.NewStyle().
			Foreground(colorSubtle)

	spinnerStyle = lipgloss.NewStyle().
			Foreground(colorPrimary)

	progressActive = lipgloss.NewStyle().
			Foreground(colorPrimary)

	progressWait = lipgloss.NewStyle().
			Foreground(colorMuted)

	videoBadge = lipgloss.NewStyle().
			Foreground(colorBg).
			Background(colorPrimary).
			Padding(0, 1).
			Bold(true)

	audioBadge = lipgloss.NewStyle().
			Foreground(colorBg).
			Background(colorSecondary).
			Padding(0, 1).
			Bold(true)

	subtitleBadge = lipgloss.NewStyle().
			Foreground(colorBg).
			Background(colorAccent).
			Padding(0, 1).
			Bold(true)

	statLabelStyle = lipgloss.NewStyle().
			Foreground(colorSubtle)

	statValueStyle = lipgloss.NewStyle().
			Foreground(colorAccent).
			Bold(true)
)

var spinner = []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}
