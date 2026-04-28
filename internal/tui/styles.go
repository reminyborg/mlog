package tui

import "github.com/charmbracelet/lipgloss"

var (
	titleStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("#C9A227")).
			Padding(0, 1)

	sectionStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("#7FB069"))

	projectStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#6C91BF"))

	mutedStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("240"))

	helpStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("244")).
			MarginTop(1)

	inputPromptStyle = lipgloss.NewStyle().
				Foreground(lipgloss.Color("#C9A227")).
				Bold(true)

	errorStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#E06C75"))

	statusStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#98C379"))
)
