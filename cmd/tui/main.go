package main

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"time"

	"github.com/charmbracelet/bubbles/list"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"film-heatmap/internal/domain"
	"film-heatmap/internal/service"
	store "film-heatmap/internal/store/sqlite"
)

type state int

const (
	stateMenu state = iota
	stateListLogs
	stateAddLog
	stateHeatmap
	stateStats
)

type logItem struct {
	id    string
	title string
	date  string
}

func (i logItem) Title() string       { return i.title }
func (i logItem) Description() string { return i.date }
func (i logItem) FilterValue() string { return i.title }

type model struct {
	state      state
	menuCursor int
	list       list.Model
	input      textinput.Model
	heatmap *domain.HeatmapMatrix
	stats   *domain.YearStats
	err     error
	width   int
	height  int

	logService   *service.LogService
	heatService  *service.HeatmapService
	statsService *service.StatsService
	ctx          context.Context
}

type loadedLogsMsg []logItem
type loadedHeatmapMsg *domain.HeatmapMatrix
type loadedStatsMsg *domain.YearStats
type logAddedMsg struct{}
type errorMsg error

func initialModel(dbPath string) (model, error) {
	st, err := store.Open(dbPath)
	if err != nil {
		return model{}, err
	}

	ls := service.NewLogService(st)
	hs := service.NewHeatmapService(st)
	ss := service.NewStatsService(st)

	ti := textinput.New()
	ti.Placeholder = "Enter film title..."
	ti.Focus()
	ti.CharLimit = 156
	ti.Width = 30

	l := list.New([]list.Item{}, list.NewDefaultDelegate(), 0, 0)
	l.Title = "Film Logs"
	l.SetShowHelp(false)

	return model{
		state:        stateMenu,
		list:         l,
		input:        ti,
		logService:   ls,
		heatService:  hs,
		statsService: ss,
		ctx:          context.Background(),
	}, nil
}

func (m model) Init() tea.Cmd {
	return tea.Batch(
		tea.EnterAltScreen,
		m.loadLogsCmd(),
		m.loadHeatmapCmd(),
		m.loadStatsCmd(),
	)
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmd tea.Cmd
	var cmds []tea.Cmd

	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "ctrl+c", "q":
			if m.state == stateMenu || (m.state != stateAddLog && m.input.Focused()) {
				return m, tea.Quit
			}
			if m.state != stateMenu {
				m.state = stateMenu
				return m, nil
			}
		case "esc":
			if m.state != stateMenu {
				m.state = stateMenu
				return m, nil
			}
		case "up", "k":
			if m.state == stateMenu {
				if m.menuCursor > 0 {
					m.menuCursor--
				}
			}
		case "down", "j":
			if m.state == stateMenu {
				if m.menuCursor < 3 {
					m.menuCursor++
				}
			}
		case "enter":
			if m.state == stateMenu {
				switch m.menuCursor {
				case 0:
					m.state = stateAddLog
					m.input.SetValue("")
					m.input.Focus()
					return m, nil
				case 1:
					m.state = stateListLogs
					// Resize list to fit window
					h, v := docStyle.GetFrameSize()
					m.list.SetSize(m.width-h, m.height-v)
					return m, m.list.StartSpinner()
				case 2:
					m.state = stateHeatmap
					return m, m.loadHeatmapCmd()
				case 3:
					m.state = stateStats
					return m, m.loadStatsCmd()
				}
			} else if m.state == stateAddLog {
				title := m.input.Value()
				if title != "" {
					return m, m.addLogCmd(title)
				}
			}
		}

	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		h, v := docStyle.GetFrameSize()
		m.list.SetSize(msg.Width-h, msg.Height-v)

	case loadedLogsMsg:
		items := make([]list.Item, len(msg))
		for i, l := range msg {
			items[i] = l
		}
		m.list.SetItems(items)
		m.list.StopSpinner()

	case loadedHeatmapMsg:
		m.heatmap = msg

	case loadedStatsMsg:
		m.stats = msg

	case logAddedMsg:
		m.state = stateListLogs
		m.input.SetValue("")
		return m, tea.Batch(m.loadLogsCmd(), m.loadHeatmapCmd(), m.loadStatsCmd())

	case errorMsg:
		m.err = msg
	}

	switch m.state {
	case stateListLogs:
		m.list, cmd = m.list.Update(msg)
		cmds = append(cmds, cmd)
	case stateAddLog:
		m.input, cmd = m.input.Update(msg)
		cmds = append(cmds, cmd)
	}

	return m, tea.Batch(cmds...)
}

func (m model) View() string {
	if m.err != nil {
		return fmt.Sprintf("Error: %v\nPress q to quit.", m.err)
	}

	switch m.state {
	case stateMenu:
		return m.viewMenu()
	case stateListLogs:
		return docStyle.Render(m.list.View())
	case stateAddLog:
		return m.viewAdd()
	case stateHeatmap:
		return m.viewHeatmap()
	case stateStats:
		return m.viewStats()
	default:
		return "Unknown state"
	}
}

func (m model) viewMenu() string {
	choices := []string{"Add Log", "View Logs", "View Heatmap", "View Stats"}
	s := titleStyle.Render("Film Heatmap TUI") + "\n\n"

	for i, choice := range choices {
		cursor := " "
		if m.menuCursor == i {
			cursor = ">"
			choice = selectedItemStyle.Render(choice)
		} else {
			choice = itemStyle.Render(choice)
		}
		s += fmt.Sprintf("%s %s\n", cursor, choice)
	}
	s += "\n" + helpStyle.Render("Use arrow keys to navigate, enter to select, q to quit")
	return docStyle.Render(s)
}

func (m model) viewAdd() string {
	return docStyle.Render(fmt.Sprintf(
		"Add a new film log\n\n%s\n\n%s",
		m.input.View(),
		"(esc to back)",
	))
}

func (m model) viewHeatmap() string {
	if m.heatmap == nil {
		return docStyle.Render("Loading heatmap...")
	}
	
	// Title
	s := titleStyle.Render(fmt.Sprintf("Heatmap %d", m.heatmap.Year)) + "\n\n"
	
	// Month header (simplified, just spacing)
	// We can iterate weeks and print month name if it changes? 
	// For TUI simplicity, maybe just labels at the top?
	// Let's just do the grid first.

	grid := ""
	for day := 0; day < 7; day++ {
		row := ""
		// Day labels
		dayLabel := "   "
		switch day {
		case 1:
			dayLabel = "Mon"
		case 3:
			dayLabel = "Wed"
		case 5:
			dayLabel = "Fri"
		}
		row += lipgloss.NewStyle().Foreground(subtle).Width(4).Render(dayLabel)

		for _, w := range m.heatmap.Weeks {
			c := w[day]
			color := getHeatmapColor(c.Intensity)
			block := lipgloss.NewStyle().Background(color).SetString("  ").String()
			row += block
		}
		grid += row + "\n"
	}

	// Legend
	legend := "\n" + lipgloss.NewStyle().Foreground(subtle).Render("Less ")
	legend += lipgloss.NewStyle().Background(colorIntensity0).SetString("  ").String() + " "
	legend += lipgloss.NewStyle().Background(colorIntensity1).SetString("  ").String() + " "
	legend += lipgloss.NewStyle().Background(colorIntensity2).SetString("  ").String() + " "
	legend += lipgloss.NewStyle().Background(colorIntensity3).SetString("  ").String() + " "
	legend += lipgloss.NewStyle().Background(colorIntensity4).SetString("  ").String()
	legend += lipgloss.NewStyle().Foreground(subtle).Render(" More")

	s += lipgloss.JoinVertical(lipgloss.Left, grid, legend)
	s += "\n\n(esc to back)"
	
	return docStyle.Render(s)
}

func (m model) viewStats() string {
	if m.stats == nil {
		return docStyle.Render("Loading stats...")
	}
	
	// Create stylized boxes for stats
	total := statBoxStyle.Render(fmt.Sprintf("%s\n%s", statLabelStyle.Render("Total Logs"), statValueStyle.Render(strconv.Itoa(m.stats.TotalLogs))))
	active := statBoxStyle.Render(fmt.Sprintf("%s\n%s", statLabelStyle.Render("Active Days"), statValueStyle.Render(strconv.Itoa(m.stats.ActiveDays))))
	streak := statBoxStyle.Render(fmt.Sprintf("%s\n%s", statLabelStyle.Render("Longest Streak"), statValueStyle.Render(strconv.Itoa(m.stats.LongestStreak))))
	current := statBoxStyle.Render(fmt.Sprintf("%s\n%s", statLabelStyle.Render("Current Streak"), statValueStyle.Render(strconv.Itoa(m.stats.CurrentStreak))))

	grid := lipgloss.JoinHorizontal(lipgloss.Top, total, active)
	grid2 := lipgloss.JoinHorizontal(lipgloss.Top, streak, current)
	
	return docStyle.Render(lipgloss.JoinVertical(lipgloss.Center, 
		titleStyle.Render(fmt.Sprintf("Statistics %d", m.stats.Year)),
		grid,
		grid2,
		"\n(esc to back)",
	))
}

// Commands
func (m model) loadLogsCmd() tea.Cmd {
	return func() tea.Msg {
		logs, err := m.logService.List(m.ctx, domain.ListFilter{})
		if err != nil {
			return errorMsg(err)
		}
		items := make([]logItem, len(logs))
		for i, l := range logs {
			items[i] = logItem{id: l.ID, title: l.Title, date: l.LocalDate}
		}
		return loadedLogsMsg(items)
	}
}

func (m model) loadHeatmapCmd() tea.Cmd {
	return func() tea.Msg {
		hm, err := m.heatService.Year(m.ctx, time.Now().Year())
		if err != nil {
			return errorMsg(err)
		}
		return loadedHeatmapMsg(&hm)
	}
}

func (m model) loadStatsCmd() tea.Cmd {
	return func() tea.Msg {
		st, err := m.statsService.Year(m.ctx, time.Now().Year())
		if err != nil {
			return errorMsg(err)
		}
		return loadedStatsMsg(&st)
	}
}

func (m model) addLogCmd(title string) tea.Cmd {
	return func() tea.Msg {
		_, err := m.logService.Add(m.ctx, domain.AddLogInput{
			Title:    title,
			LoggedAt: time.Now(),
		})
		if err != nil {
			return errorMsg(err)
		}
		return logAddedMsg{}
	}
}

var docStyle = lipgloss.NewStyle().Margin(1, 2)

func main() {
	dbPath := os.Getenv("FILM_HEATMAP_DB")
	if dbPath == "" {
		dbPath = "film-heatmap.db"
	}
	m, err := initialModel(dbPath)
	if err != nil {
		fmt.Println("Error initializing model:", err)
		os.Exit(1)
	}
	p := tea.NewProgram(m, tea.WithAltScreen())
	if _, err := p.Run(); err != nil {
		fmt.Println("Error running program:", err)
		os.Exit(1)
	}
}
