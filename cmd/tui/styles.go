package main

import (
	"github.com/charmbracelet/bubbles/list"
	"github.com/charmbracelet/lipgloss"
)

var (
	subtle    = lipgloss.AdaptiveColor{Light: "#D9DCCF", Dark: "#383838"}
	highlight = lipgloss.AdaptiveColor{Light: "#874BFD", Dark: "#7D56F4"}
	special   = lipgloss.AdaptiveColor{Light: "#43BF6D", Dark: "#73F59F"}
	warning   = lipgloss.AdaptiveColor{Light: "#F25D94", Dark: "#F55687"}

	titleStyle = lipgloss.NewStyle().
			MarginLeft(1).
			MarginRight(5).
			Padding(0, 1).
			Italic(true).
			Foreground(lipgloss.Color("#FFF7DB")).
			SetString("Film Heatmap")

	itemStyle = lipgloss.NewStyle().PaddingLeft(4)

	selectedItemStyle = lipgloss.NewStyle().
				PaddingLeft(2).
				Foreground(lipgloss.Color("170")).
				SetString("• ")

	paginationStyle = list.DefaultStyles().PaginationStyle.PaddingLeft(4)

	helpStyle = list.DefaultStyles().HelpStyle.PaddingLeft(4).PaddingBottom(1)

	quitTextStyle = lipgloss.NewStyle().Margin(1, 0, 2, 4)

	// Heatmap styles
	cellStyle = lipgloss.NewStyle().
			Width(3).
			Height(1).
			Align(lipgloss.Center, lipgloss.Center).
			Margin(0, 0)

	// Intensity colors (Green shades)
	colorIntensity0 = lipgloss.Color("#161b22") // Dark/Empty
	colorIntensity1 = lipgloss.Color("#0e4429") // Low
	colorIntensity2 = lipgloss.Color("#006d32") // Medium
	colorIntensity3 = lipgloss.Color("#26a641") // High
	colorIntensity4 = lipgloss.Color("#39d353") // Max

	// Stats styles
	statBoxStyle = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(highlight).
			Padding(1, 2).
			Margin(1).
			Width(20).
			Height(5).
			Align(lipgloss.Center)

	statLabelStyle = lipgloss.NewStyle().
			Foreground(subtle).
			Bold(true).
			MarginBottom(1)

	statValueStyle = lipgloss.NewStyle().
			Foreground(special).
			Bold(true)
			// FontSize(2) // Lipgloss doesn't support font size, but we can make it bold/colored
)

func getHeatmapColor(intensity int) lipgloss.Color {
	switch intensity {
	case 1:
		return colorIntensity1
	case 2:
		return colorIntensity2
	case 3:
		return colorIntensity3
	case 4:
		return colorIntensity4
	default:
		return colorIntensity0
	}
}
