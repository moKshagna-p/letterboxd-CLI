package ui

import (
	"context"
	"fmt"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"film-heatmap/internal/domain"
	"film-heatmap/internal/service"
)

// ViewID represents which view is currently active
type ViewID int

const (
	ViewMainMenu ViewID = iota
	ViewWatched
	ViewRatings
	ViewReviews
	ViewHeatmap
	ViewStats
	ViewWatchlist
	ViewLists
	ViewEditLog
)

// Model is the main Bubble Tea model
type Model struct {
	ctx         context.Context
	deps        *Dependencies
	currentView ViewID
	width       int
	height      int

	// View state
	watched         []domain.FilmLog
	ratings         []domain.FilmLog
	reviews         []domain.FilmLog
	heatmap         domain.HeatmapMatrix
	stats           domain.YearStats
	watchlist       []domain.WatchlistItem
	lists           []domain.FilmList
	selectedIndex   int
	editingLog      *domain.FilmLog
	editField       string
	editValue       string
	message         string
	messageTime     time.Time
}

// Dependencies holds all the service dependencies
type Dependencies struct {
	Logs   *service.LogService
	Heat   *service.HeatmapService
	Stats  *service.StatsService
	LBStats *service.LetterboxdStatsService
	CSV    *service.CSVService
	Lib    *service.LibraryService
	Cfg    *service.AppConfigService
	Sync   *service.LetterboxdSyncService
}

// New creates a new TUI model
func New(ctx context.Context, deps *Dependencies) *Model {
	return &Model{
		ctx:       ctx,
		deps:      deps,
		currentView: ViewMainMenu,
		selectedIndex: 0,
	}
}

// Init initializes the model
func (m Model) Init() tea.Cmd {
	return nil
}

// Update handles all messages
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		return m.handleKeyPress(msg)
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		return m, nil
	case loadWatchedMsg:
		m.watched = msg.logs
		m.currentView = ViewWatched
		return m, nil
	case loadRatingsMsg:
		m.ratings = msg.logs
		m.currentView = ViewRatings
		return m, nil
	case loadReviewsMsg:
		m.reviews = msg.logs
		m.currentView = ViewReviews
		return m, nil
	case loadHeatmapMsg:
		m.heatmap = msg.heatmap
		m.currentView = ViewHeatmap
		return m, nil
	case loadStatsMsg:
		m.stats = msg.stats
		m.currentView = ViewStats
		return m, nil
	case errorMsg:
		m.message = fmt.Sprintf("Error: %s", msg.err)
		m.messageTime = time.Now()
		return m, nil
	case successMsg:
		m.message = string(msg)
		m.messageTime = time.Now()
		return m, nil
	}
	return m, nil
}

// View renders the current view
func (m Model) View() string {
	var content string

	switch m.currentView {
	case ViewMainMenu:
		content = m.viewMainMenu()
	case ViewWatched:
		content = m.viewWatched()
	case ViewRatings:
		content = m.viewRatings()
	case ViewReviews:
		content = m.viewReviews()
	case ViewHeatmap:
		content = m.viewHeatmap()
	case ViewStats:
		content = m.viewStats()
	case ViewWatchlist:
		content = m.viewWatchlist()
	case ViewLists:
		content = m.viewLists()
	default:
		content = "Unknown view"
	}

	// Add status message if present
	if m.message != "" && time.Since(m.messageTime) < 3*time.Second {
		content += "\n\n" + m.renderMessage()
	}

	return content
}

// handleKeyPress handles keyboard input
func (m Model) handleKeyPress(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "ctrl+c", "q":
		return m, tea.Quit

	case "up", "k":
		if m.selectedIndex > 0 {
			m.selectedIndex--
		}
		return m, nil

	case "down", "j":
		m.selectedIndex++
		return m, nil

	case "home":
		m.currentView = ViewMainMenu
		m.selectedIndex = 0
		return m, nil

	case "1":
		return m, m.loadWatched()
	case "2":
		return m, m.loadRatings()
	case "3":
		return m, m.loadReviews()
	case "4":
		return m, m.loadHeatmap()
	case "5":
		return m, m.loadStats()
	case "6":
		return m, m.loadWatchlist()
	case "7":
		return m, m.loadLists()

	case "r":
		return m, m.refreshData()
	case "?":
		return m, m.showHelp()
	}

	return m, nil
}

// Message types for asynchronous operations
type loadWatchedMsg struct{ logs []domain.FilmLog }
type loadRatingsMsg struct{ logs []domain.FilmLog }
type loadReviewsMsg struct{ logs []domain.FilmLog }
type loadHeatmapMsg struct{ heatmap domain.HeatmapMatrix }
type loadStatsMsg struct{ stats domain.YearStats }
type errorMsg struct{ err error }
type successMsg string

// Command generators
func (m Model) loadWatched() tea.Cmd {
	return func() tea.Msg {
		logs, err := m.deps.Logs.List(m.ctx, domain.ListFilter{})
		if err != nil {
			return errorMsg{err}
		}
		return loadWatchedMsg{logs}
	}
}

func (m Model) loadRatings() tea.Cmd {
	return func() tea.Msg {
		logs, err := m.deps.Logs.List(m.ctx, domain.ListFilter{})
		if err != nil {
			return errorMsg{err}
		}
		// Filter rated only
		var rated []domain.FilmLog
		for _, l := range logs {
			if l.Rating != nil {
				rated = append(rated, l)
			}
		}
		return loadRatingsMsg{rated}
	}
}

func (m Model) loadReviews() tea.Cmd {
	return func() tea.Msg {
		logs, err := m.deps.Logs.List(m.ctx, domain.ListFilter{})
		if err != nil {
			return errorMsg{err}
		}
		// Filter with notes only
		var reviewed []domain.FilmLog
		for _, l := range logs {
			if l.Notes != nil && *l.Notes != "" {
				reviewed = append(reviewed, l)
			}
		}
		return loadReviewsMsg{reviewed}
	}
}

func (m Model) loadHeatmap() tea.Cmd {
	return func() tea.Msg {
		hm, err := m.deps.Heat.RecentWeeks(m.ctx, 53, time.Now())
		if err != nil {
			return errorMsg{err}
		}
		return loadHeatmapMsg{hm}
	}
}

func (m Model) loadStats() tea.Cmd {
	return func() tea.Msg {
		st, err := m.deps.Stats.Year(m.ctx, time.Now().Year())
		if err != nil {
			return errorMsg{err}
		}
		return loadStatsMsg{st}
	}
}

func (m Model) loadWatchlist() tea.Cmd {
	return func() tea.Msg {
		// Implement watchlist loading
		return nil
	}
}

func (m Model) loadLists() tea.Cmd {
	return func() tea.Msg {
		// Implement lists loading
		return nil
	}
}

func (m Model) refreshData() tea.Cmd {
	return func() tea.Msg {
		// TODO: Trigger sync
		return successMsg("Data refreshed")
	}
}

func (m Model) showHelp() tea.Cmd {
	return func() tea.Msg {
		return nil
	}
}

// Render helper functions
func (m Model) renderMessage() string {
	return lipgloss.NewStyle().
		Foreground(lipgloss.Color("3")).
		Render("● " + m.message)
}
