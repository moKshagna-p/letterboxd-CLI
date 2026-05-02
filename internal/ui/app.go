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
	ViewListItems
	ViewEditLog
)

// Model is the main Bubble Tea model
type Model struct {
	ctx         context.Context
	deps        *Dependencies
	currentView ViewID
	width       int
	height      int
	loading     bool

	// View state
	watched         []domain.FilmLog
	ratings         []domain.FilmLog
	reviews         []domain.FilmLog
	heatmap         domain.HeatmapMatrix
	stats           domain.YearStats
	watchlist       []domain.WatchlistItem
	lists           []domain.FilmList
	listItems       []domain.FilmListItem
	currentListID   string
	currentListName string
	selectedIndex   int
	scrollOffset    int
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
		scrollOffset: 0,
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
		m.selectedIndex = 0
		m.scrollOffset = 0
		m.currentView = ViewWatched
		m.loading = false
		return m, nil
	case loadReviewsMsg:
		m.reviews = msg.logs
		m.selectedIndex = 0
		m.scrollOffset = 0
		m.currentView = ViewReviews
		m.loading = false
		return m, nil
	case loadHeatmapMsg:
		m.heatmap = msg.heatmap
		m.selectedIndex = 0
		m.scrollOffset = 0
		m.currentView = ViewHeatmap
		m.loading = false
		return m, nil
	case loadStatsMsg:
		m.stats = msg.stats
		m.selectedIndex = 0
		m.scrollOffset = 0
		m.currentView = ViewStats
		m.loading = false
		return m, nil
	case loadWatchlistMsg:
		m.watchlist = msg.items
		m.selectedIndex = 0
		m.scrollOffset = 0
		m.currentView = ViewWatchlist
		m.loading = false
		return m, nil
	case loadListsMsg:
		m.lists = msg.lists
		m.selectedIndex = 0
		m.scrollOffset = 0
		m.currentView = ViewLists
		m.loading = false
		return m, nil
	case loadListItemsMsg:
		m.listItems = msg.items
		m.currentListName = msg.listName
		m.selectedIndex = 0
		m.scrollOffset = 0
		m.currentView = ViewListItems
		m.loading = false
		return m, nil
	case errorMsg:
		m.message = fmt.Sprintf("Error: %s", msg.err)
		m.messageTime = time.Now()
		m.loading = false
		return m, nil
	case successMsg:
		m.message = string(msg)
		m.messageTime = time.Now()
		return m, nil
	case loadingMsg:
		m.loading = true
		return m, nil
	}
	return m, nil
}

// View renders the current view
func (m Model) View() string {
	if m.loading {
		return lipgloss.NewStyle().
			Foreground(lipgloss.Color("3")).
			Render("⏳ Loading...")
	}

	var content string

	switch m.currentView {
	case ViewMainMenu:
		content = m.viewMainMenu()
	case ViewWatched:
		content = m.viewWatched()
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
	case ViewListItems:
		content = m.viewListItems()
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
	case "ctrl+c":
		return m, tea.Quit

	case "q", "esc":
		// Go back to main menu from any view
		if m.currentView != ViewMainMenu {
			m.currentView = ViewMainMenu
			m.selectedIndex = 0
			m.scrollOffset = 0
			return m, nil
		}
		// Quit if already in main menu
		return m, tea.Quit

	case "up", "k":
		if m.selectedIndex > 0 {
			m.selectedIndex--
			if m.selectedIndex < m.scrollOffset {
				m.scrollOffset = m.selectedIndex
			}
		}
		return m, nil

	case "down", "j":
		// Get current list length based on view
		maxIndex := m.getMaxIndex() - 1
		if m.selectedIndex < maxIndex {
			m.selectedIndex++
			// Calculate visible area
			visibleHeight := m.height - 15 // Roughly the card content area
			if visibleHeight < 1 {
				visibleHeight = 1
			}
			if m.selectedIndex >= m.scrollOffset+visibleHeight {
				m.scrollOffset = m.selectedIndex - visibleHeight + 1
			}
		}
		return m, nil

	case "home":
		m.currentView = ViewMainMenu
		m.selectedIndex = 0
		m.scrollOffset = 0
		return m, nil


	// Menu shortcuts
	case "1":
		return m, m.loadWatched()
	case "2":
		return m, m.loadReviews()
	case "3":
		return m, m.loadHeatmap()
	case "4":
		return m, m.loadStats()
	case "5":
		return m, m.loadWatchlist()
	case "6":
		return m, m.loadLists()

	case "r":
		return m, m.refreshData()
	case "enter":
		if m.currentView == ViewLists && m.selectedIndex < len(m.lists) {
			list := m.lists[m.selectedIndex]
			return m, m.loadListItems(list.ID, list.Name)
		}
		return m, nil
	case "?":
		m.message = "↑↓/jk: Navigate  │  1-6: Menu  │  Home: Menu  │  q/Esc: Back  │  r: Refresh  │  Ctrl+C: Quit"
		m.messageTime = time.Now()
		return m, nil
	}

	return m, nil
}

func (m Model) getMaxIndex() int {
	switch m.currentView {
	case ViewWatched:
		return len(m.watched)
	case ViewReviews:
		return len(m.reviews)
	case ViewWatchlist:
		return len(m.watchlist)
	case ViewLists:
		return len(m.lists)
	case ViewListItems:
		return len(m.listItems)
	default:
		return 0
	}
}

// Message types for asynchronous operations
type loadWatchedMsg struct{ logs []domain.FilmLog }
type loadReviewsMsg struct{ logs []domain.FilmLog }
type loadHeatmapMsg struct{ heatmap domain.HeatmapMatrix }
type loadStatsMsg struct{ stats domain.YearStats }
type loadWatchlistMsg struct{ items []domain.WatchlistItem }
type loadListsMsg struct{ lists []domain.FilmList }
type loadListItemsMsg struct{ items []domain.FilmListItem; listName string }
type errorMsg struct{ err error }
type successMsg string
type loadingMsg struct{}
type doneLoadingMsg struct{}

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
		// Calculate how many weeks fit in the terminal width
		// Each week takes ~2 chars, add buffer for day label (5 chars) and spacing
		weeksToShow := (m.width - 10) / 2
		if weeksToShow < 20 {
			weeksToShow = 20
		}
		if weeksToShow > 104 { // ~2 years
			weeksToShow = 104
		}
		hm, err := m.deps.Heat.RecentWeeks(m.ctx, weeksToShow, time.Now())
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
		items, err := m.deps.Lib.ListWatchlist(m.ctx)
		if err != nil {
			return errorMsg{err}
		}
		return loadWatchlistMsg{items}
	}
}

func (m Model) loadLists() tea.Cmd {
	return func() tea.Msg {
		lists, err := m.deps.Lib.ListLists(m.ctx)
		if err != nil {
			return errorMsg{err}
		}
		return loadListsMsg{lists}
	}
}

func (m Model) loadListItems(listID string, listName string) tea.Cmd {
	return func() tea.Msg {
		items, err := m.deps.Lib.ListListItems(m.ctx, listID)
		if err != nil {
			return errorMsg{err}
		}
		return loadListItemsMsg{items, listName}
	}
}

func (m Model) refreshData() tea.Cmd {
	return func() tea.Msg {
		res, err := m.deps.Sync.SyncAndImport(m.ctx)
		if err != nil {
			return errorMsg{err}
		}
		if res.ImportResult.Imported == 0 && res.WatchlistAdded == 0 {
			return successMsg("No new data found")
		}
		return successMsg(fmt.Sprintf("Imported %d new films", res.ImportResult.Imported))
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
