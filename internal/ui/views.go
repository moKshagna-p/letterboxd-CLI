package ui

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
	"film-heatmap/internal/domain"
)

// Theme colors matching the original design
var (
	accentColor = lipgloss.Color("#e8b478")   // warm gold
	warmColor   = lipgloss.Color("#cc6666")   // dusty red
	roseColor   = lipgloss.Color("#c7869d")   // muted mauve
	coolColor   = lipgloss.Color("#82b4cd")   // powder blue
	errorColor  = lipgloss.Color("#ff5252")   // error red
	dimColor    = lipgloss.Color("#929ba5")   // soft gray
	panelColor  = lipgloss.Color("#5a6473")   // slate-blue
	uiReset     = "\x1b[0m"
)

// Box drawing styles
var (
	boxStyle = lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(panelColor).
		Padding(1, 2)

	titleStyle = lipgloss.NewStyle().
		Foreground(accentColor).
		Bold(true)

	subtitleStyle = lipgloss.NewStyle().
		Foreground(dimColor).
		Italic(true)

	headerStyle = lipgloss.NewStyle().
		Foreground(accentColor).
		Bold(true).
		Underline(true)

	selectedStyle = lipgloss.NewStyle().
		Foreground(lipgloss.Color("#000000")).
		Background(accentColor).
		Bold(true)

	dimStyle = lipgloss.NewStyle().
		Foreground(dimColor)
)

// viewMainMenu renders the main menu
func (m Model) viewMainMenu() string {
	menu := strings.Builder{}

	menu.WriteString(renderBanner("LETTERBOXD TUI", "Film Logging & Analytics"))
	menu.WriteString("\n\n")

	options := []string{
		"1  Watched   - Your recently logged films",
		"2  Ratings   - Your top-rated watches",
		"3  Reviews   - Notes & commentary",
		"4  Heatmap   - Visual activity map",
		"5  Stats     - Year overview",
		"6  Watchlist - Planned watches",
		"7  Lists     - Collections",
	}

	menu.WriteString(renderCard("MENU", "Choose a view", options, accentColor))
	menu.WriteString("\n\n")
	menu.WriteString(dimStyle.Render("↑↓: Navigate  │  q/Esc: Quit  │  r: Refresh  │  ?: Help"))

	return menu.String()
}

// viewWatched renders the watched films view
func (m Model) viewWatched() string {
	view := strings.Builder{}
	view.WriteString(renderBanner("WATCHED FILMS", fmt.Sprintf("Latest %d entries", len(m.watched))))
	view.WriteString("\n\n")

	lines := make([]string, 0)
	lines = append(lines, renderTableHeader([]string{"Date", "Rating", "Title"}, []int{12, 8, 50}))

	for i, log := range m.watched {
		if i >= m.height-12 {
			break
		}
		rating := "─"
		if log.Rating != nil {
			rating = fmt.Sprintf("%.1f★", *log.Rating)
		}

		row := renderTableRow([]string{log.LocalDate, rating, log.Title}, []int{12, 8, 50})
		if i == m.selectedIndex {
			row = selectedStyle.Render(row)
		}
		lines = append(lines, row)
	}

	view.WriteString(renderCard("WATCHED", fmt.Sprintf("%d films logged", len(m.watched)), lines, accentColor))
	view.WriteString("\n" + dimStyle.Render("↑↓: Navigate  │  q/Esc: Back  │  1-7: Menu  │  r: Refresh"))

	return view.String()
}

// viewRatings renders the ratings view
func (m Model) viewRatings() string {
	view := strings.Builder{}
	view.WriteString(renderBanner("RATINGS DESK", "Top-rated films"))
	view.WriteString("\n\n")

	lines := make([]string, 0)
	lines = append(lines, renderTableHeader([]string{"Score", "Date", "Title"}, []int{8, 12, 50}))

	for i, log := range m.ratings {
		if i >= m.height-12 {
			break
		}
		score := fmt.Sprintf("%.1f★", *log.Rating)
		row := renderTableRow([]string{score, log.LocalDate, log.Title}, []int{8, 12, 50})
		if i == m.selectedIndex {
			row = selectedStyle.Render(row)
		}
		lines = append(lines, row)
	}

	view.WriteString(renderCard("RATINGS", fmt.Sprintf("%d rated films", len(m.ratings)), lines, warmColor))
	view.WriteString("\n" + dimStyle.Render("↑↓: Navigate  │  q/Esc: Back  │  1-7: Menu  │  r: Refresh"))

	return view.String()
}

// viewReviews renders the reviews view
func (m Model) viewReviews() string {
	view := strings.Builder{}
	view.WriteString(renderBanner("REVIEW NOTES", "Your commentary"))
	view.WriteString("\n\n")

	lines := make([]string, 0)
	lines = append(lines, renderTableHeader([]string{"Date", "Film", "Note Preview"}, []int{12, 26, 32}))

	for i, log := range m.reviews {
		if i >= m.height-12 {
			break
		}
		notePreview := ""
		if log.Notes != nil {
			notePreview = *log.Notes
			if len(notePreview) > 32 {
				notePreview = notePreview[:29] + "..."
			}
		}
		row := renderTableRow([]string{log.LocalDate, log.Title, notePreview}, []int{12, 26, 32})
		if i == m.selectedIndex {
			row = selectedStyle.Render(row)
		}
		lines = append(lines, row)
	}

	view.WriteString(renderCard("REVIEWS", fmt.Sprintf("%d notes recorded", len(m.reviews)), lines, roseColor))
	view.WriteString("\n" + dimStyle.Render("↑↓: Navigate  │  q/Esc: Back  │  1-7: Menu  │  r: Refresh"))

	return view.String()
}

// viewHeatmap renders the heatmap view
func (m Model) viewHeatmap() string {
	view := strings.Builder{}
	view.WriteString(renderBanner("ACTIVITY MAP", "Your viewing pattern over the last year"))
	view.WriteString("\n\n")

	// Group weeks chronologically
	weeks := orderWeeksChronologically(m.heatmap.Weeks)
	if len(weeks) == 0 {
		view.WriteString(dimStyle.Render("( no heatmap data )"))
		view.WriteString("\n\n" + dimStyle.Render("q/Esc: Back"))
		return view.String()
	}

	totalFilms := totalFilmsInWeeks(weeks)

	// Build month headers
	monthLine := buildMonthHeaderLine(weeks)
	view.WriteString(dimStyle.Render(monthLine))
	view.WriteString("\n")

	// Render 7 day rows (Sunday through Saturday)
	dayLabels := []string{"Sun", "Mon", "Tue", "Wed", "Thu", "Fri", "Sat"}
	for day := 0; day < 7; day++ {
		line := fmt.Sprintf(" %s  ", dayLabels[day])
		for _, week := range weeks {
			if day < len(week) {
				line += renderHeatmapCell(week[day].Intensity) + " "
			}
		}
		view.WriteString(line)
		view.WriteString("\n")
	}

	view.WriteString("\n")
	legend := fmt.Sprintf("%sLess%s %s %s %s %s %s %sMore%s       %sTotal: %d films%s",
		dimStyle.Render(" "),
		dimStyle.Render(" "),
		renderHeatmapCell(0), renderHeatmapCell(1), renderHeatmapCell(2), renderHeatmapCell(3), renderHeatmapCell(4),
		dimStyle.Render(" "),
		dimStyle.Render(" "),
		accentColor, totalFilms, uiReset)
	view.WriteString(centerPad(legend, 70))
	view.WriteString("\n\n")
	view.WriteString(dimStyle.Render("q/Esc: Back  │  1-7: Menu  │  r: Refresh"))

	return view.String()
}

// viewStats renders the stats view
func (m Model) viewStats() string {
	view := strings.Builder{}
	view.WriteString(renderBanner("ANALYTICS", "Year summary"))
	view.WriteString("\n\n")

	lines := []string{
		fmt.Sprintf("Year: %d", m.stats.Year),
		fmt.Sprintf("Total Logs: %d", m.stats.TotalLogs),
		fmt.Sprintf("Active Days: %d", m.stats.ActiveDays),
		fmt.Sprintf("Longest Streak: %d days", m.stats.LongestStreak),
		fmt.Sprintf("Current Streak: %d days", m.stats.CurrentStreak),
	}

	formattedLines := make([]string, 0)
	for _, line := range lines {
		formattedLines = append(formattedLines, centerPad(line, m.width-6))
	}

	view.WriteString(renderCard("STATS", "Annual overview", formattedLines, coolColor))
	view.WriteString("\n" + dimStyle.Render("q/Esc: Back  │  1-7: Menu  │  r: Refresh"))

	return view.String()
}

// viewWatchlist renders the watchlist view
func (m Model) viewWatchlist() string {
	view := strings.Builder{}
	view.WriteString(renderBanner("WATCHLIST", "Planned watches"))
	view.WriteString("\n\n")

	lines := make([]string, 0)
	lines = append(lines, renderTableHeader([]string{"Added", "Title / Notes"}, []int{12, 60}))

	for i, item := range m.watchlist {
		if i >= m.height-12 {
			break
		}
		note := ""
		if item.Notes != nil {
			note = " | " + *item.Notes
		}
		row := renderTableRow([]string{item.AddedAt.In(time.Local).Format("2006-01-02"), item.Title + note}, []int{12, 60})
		if i == m.selectedIndex {
			row = selectedStyle.Render(row)
		}
		lines = append(lines, row)
	}

	view.WriteString(renderCard("WATCHLIST", fmt.Sprintf("%d items", len(m.watchlist)), lines, coolColor))
	view.WriteString("\n" + dimStyle.Render("↑↓: Navigate  │  q/Esc: Back  │  1-7: Menu  │  r: Refresh"))

	return view.String()
}

// viewLists renders the lists view
func (m Model) viewLists() string {
	view := strings.Builder{}
	view.WriteString(renderBanner("COLLECTIONS", "Custom lists"))
	view.WriteString("\n\n")

	lines := make([]string, 0)
	lines = append(lines, renderTableHeader([]string{"List", "Films"}, []int{60, 8}))

	for i, list := range m.lists {
		if i >= m.height-12 {
			break
		}
		row := renderTableRow([]string{list.Name, fmt.Sprintf("%d", 0)}, []int{60, 8})
		if i == m.selectedIndex {
			row = selectedStyle.Render(row)
		}
		lines = append(lines, row)
	}

	view.WriteString(renderCard("LISTS", fmt.Sprintf("%d collections", len(m.lists)), lines, warmColor))
	view.WriteString("\n" + dimStyle.Render("↑↓: Navigate  │  Enter: View  │  q/Esc: Back  │  1-7: Menu  │  r: Refresh"))

	return view.String()
}

// viewListItems renders the list items view
func (m Model) viewListItems() string {
	view := strings.Builder{}
	view.WriteString(renderBanner("COLLECTION: "+strings.ToUpper(m.currentListName), fmt.Sprintf("%d films", len(m.listItems))))
	view.WriteString("\n\n")

	lines := make([]string, 0)
	lines = append(lines, renderTableHeader([]string{"#", "Title", "Notes"}, []int{3, 50, 20}))

	for i, item := range m.listItems {
		if i >= m.height-12 {
			break
		}
		notes := ""
		if item.Notes != nil {
			notes = *item.Notes
		}
		row := renderTableRow([]string{fmt.Sprintf("%d", item.Position), item.Title, notes}, []int{3, 50, 20})
		if i == m.selectedIndex {
			row = selectedStyle.Render(row)
		}
		lines = append(lines, row)
	}

	view.WriteString(renderCard("ITEMS", m.currentListName, lines, warmColor))
	view.WriteString("\n" + dimStyle.Render("↑↓: Navigate  │  q/Esc: Back  │  7: Lists  │  r: Refresh"))

	return view.String()
}

// Helper rendering functions

func renderBanner(title, subtitle string) string {
	titleText := letterSpace(title)
	s := lipgloss.NewStyle().
		Foreground(accentColor).
		Bold(true)

	result := s.Render(centerPad(titleText, 80))
	if subtitle != "" {
		result += "\n" + dimStyle.Render(centerPad("─ "+subtitle+" ─", 80))
	}
	return result
}

func renderCard(title, subtitle string, lines []string, color lipgloss.Color) string {
	content := strings.Builder{}
	content.WriteString(titleStyle.Foreground(color).Render(centerPad(strings.ToUpper(title), 70)) + "\n")

	if subtitle != "" {
		content.WriteString(subtitleStyle.Render(centerPad("─ "+subtitle+" ─", 70)) + "\n")
		content.WriteString(strings.Repeat("─", 70) + "\n")
	}

	for _, line := range lines {
		content.WriteString(line + "\n")
	}

	return boxStyle.BorderForeground(color).Render(content.String())
}

func renderTableHeader(cols []string, widths []int) string {
	parts := make([]string, len(cols))
	for i, col := range cols {
		w := 12
		if i < len(widths) {
			w = widths[i]
		}
		parts[i] = padRight(strings.ToUpper(col), w)
	}
	return headerStyle.Render(strings.Join(parts, "  "))
}

func renderTableRow(cols []string, widths []int) string {
	parts := make([]string, len(cols))
	for i, col := range cols {
		w := 12
		if i < len(widths) {
			w = widths[i]
		}
		parts[i] = padRight(truncate(col, w), w)
	}
	return strings.Join(parts, "  ")
}

func renderHeatmapCell(intensity int) string {
	switch intensity {
	case 0:
		return dimStyle.Render("■")
	case 1:
		return lipgloss.NewStyle().Foreground(lipgloss.Color("#a08c64")).Render("■")
	case 2:
		return lipgloss.NewStyle().Foreground(lipgloss.Color("#c8966e")).Render("■")
	case 3:
		return lipgloss.NewStyle().Foreground(lipgloss.Color("#cc7864")).Render("■")
	default:
		return titleStyle.Render("■")
	}
}

// Utility functions

func letterSpace(s string) string {
	return strings.Join(strings.Split(s, ""), " ")
}

func centerPad(s string, width int) string {
	if len(s) >= width {
		return s
	}
	left := (width - len(s)) / 2
	right := width - len(s) - left
	return strings.Repeat(" ", left) + s + strings.Repeat(" ", right)
}

func padRight(s string, n int) string {
	if len(s) >= n {
		return s[:n]
	}
	return s + strings.Repeat(" ", n-len(s))
}

func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	if maxLen <= 3 {
		return s[:maxLen]
	}
	return s[:maxLen-3] + "..."
}

// Heatmap helpers
func orderWeeksChronologically(weeks [][]domain.HeatmapCell) [][]domain.HeatmapCell {
	sorted := make([][]domain.HeatmapCell, 0, len(weeks))
	for _, week := range weeks {
		if len(week) > 0 {
			sorted = append(sorted, week)
		}
	}
	sort.SliceStable(sorted, func(i, j int) bool {
		if len(sorted[i]) > 0 && len(sorted[j]) > 0 {
			return sorted[i][0].Date < sorted[j][0].Date
		}
		return false
	})
	return sorted
}

func buildMonthHeaderLine(weeks [][]domain.HeatmapCell) string {
	if len(weeks) == 0 {
		return ""
	}
	line := "        " // space for day labels
	for _, week := range weeks {
		if len(week) > 0 {
			t, err := time.Parse("2006-01-02", week[0].Date)
			if err == nil {
				line += fmt.Sprintf("%s ", t.Format("Jan")[0:1])
			} else {
				line += "  "
			}
		}
	}
	return line
}

func totalFilmsInWeeks(weeks [][]domain.HeatmapCell) int {
	total := 0
	for _, week := range weeks {
		for _, cell := range week {
			total += cell.Count
		}
	}
	return total
}
