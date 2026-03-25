package main

import (
	"fmt"
	"os"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"todoCli/internal/store"
)

type inputMode int

const (
	modeBoard inputMode = iota
	modeAdd
	modeSearch
)

type errMsg struct {
	err error
}

type todosLoadedMsg struct {
	todos []store.Todo
	stats store.Stats
}

type operationDoneMsg struct {
	status string
	err    error
}

type model struct {
	store           *store.Store
	dbPath          string
	width           int
	height          int
	todos           []store.Todo
	board           map[store.Quadrant][]store.Todo
	stats           store.Stats
	focus           store.Quadrant
	cursors         map[store.Quadrant]int
	mode            inputMode
	input           textinput.Model
	addQuadrant     store.Quadrant
	search          string
	pendingDeleteID int64
	status          string
	lastErr         error
}

var (
	appStyle = lipgloss.NewStyle().
			Padding(1, 2)
	titleStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("#FFF8E7")).
			Background(lipgloss.Color("#D94841")).
			Padding(0, 1)
	quadrantTitleBaseStyle = lipgloss.NewStyle().
				Bold(true)
	metaStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#5F5F5F"))
	statusStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#12664F")).
			Bold(true)
	errorStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#A61B1B")).
			Bold(true)
	doneStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#7A7A7A")).
			Strikethrough(true)
	idleBadgeStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#4B5563")).
			Padding(0, 1)
	highlightedMetaStyle = lipgloss.NewStyle().
				Bold(true)
)

func initialModel(todoStore *store.Store, dbPath string) model {
	input := textinput.New()
	input.CharLimit = 120
	input.Prompt = "> "

	return model{
		store:       todoStore,
		dbPath:      dbPath,
		board:       emptyBoard(),
		focus:       store.QuadrantOne,
		cursors:     make(map[store.Quadrant]int),
		mode:        modeBoard,
		input:       input,
		addQuadrant: store.QuadrantTwo,
		status:      "Database ready",
	}
}

func (m model) Init() tea.Cmd {
	return loadTodosCmd(m.store, m.search)
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		return m, nil

	case errMsg:
		m.lastErr = msg.err
		m.status = msg.err.Error()
		return m, nil

	case todosLoadedMsg:
		m.todos = msg.todos
		m.stats = msg.stats
		m.board = partitionTodos(m.todos)
		m.clampCursors()
		m.pendingDeleteID = 0
		return m, nil

	case operationDoneMsg:
		if msg.err != nil {
			m.lastErr = msg.err
			m.status = msg.err.Error()
			return m, nil
		}
		m.lastErr = nil
		m.status = msg.status
		return m, loadTodosCmd(m.store, m.search)

	case tea.KeyMsg:
		switch m.mode {
		case modeAdd:
			return m.updateAddMode(msg)
		case modeSearch:
			return m.updateSearchMode(msg)
		default:
			return m.updateBoardMode(msg)
		}
	}

	return m, nil
}

func (m model) updateBoardMode(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "ctrl+c", "q":
		return m, tea.Quit
	case "up", "k":
		m.clearDeletePrompt()
		if m.cursors[m.focus] > 0 {
			m.cursors[m.focus]--
		}
	case "down", "j":
		m.clearDeletePrompt()
		if m.cursors[m.focus] < len(m.board[m.focus])-1 {
			m.cursors[m.focus]++
		}
	case "left", "h":
		m.clearDeletePrompt()
		m.focus = previousQuadrant(m.focus)
	case "right", "l", "tab":
		m.clearDeletePrompt()
		m.focus = nextQuadrant(m.focus)
	case "shift+tab":
		m.clearDeletePrompt()
		m.focus = previousQuadrant(m.focus)
	case "a", "n":
		m.clearDeletePrompt()
		m.mode = modeAdd
		m.input.SetValue("")
		m.input.Placeholder = "Write a task and press Enter"
		m.input.Focus()
		m.addQuadrant = m.focus
		m.status = "Add a new task"
	case "/":
		m.clearDeletePrompt()
		m.mode = modeSearch
		m.input.SetValue(m.search)
		m.input.Placeholder = "Search tasks"
		m.input.Focus()
		m.status = "Search and press Enter"
	case "r":
		m.clearDeletePrompt()
		m.status = "Reloaded"
		return m, loadTodosCmd(m.store, m.search)
	case "esc":
		m.clearDeletePrompt()
		if m.search != "" {
			m.search = ""
			m.status = "Search cleared"
			return m, loadTodosCmd(m.store, m.search)
		}
	case "enter", " ":
		m.clearDeletePrompt()
		if todo, ok := m.currentTodo(); ok {
			return m, toggleTodoCmd(m.store, todo.ID)
		}
	case "d", "x", "backspace", "delete":
		if todo, ok := m.currentTodo(); ok {
			if m.pendingDeleteID == todo.ID {
				m.clearDeletePrompt()
				return m, deleteTodoCmd(m.store, todo.ID, todo.Title)
			}
			m.pendingDeleteID = todo.ID
			m.status = fmt.Sprintf("Press delete again to remove %q", truncate(todo.Title, 28))
			m.lastErr = nil
			return m, nil
		}
		m.clearDeletePrompt()
	case "1", "2", "3", "4":
		m.clearDeletePrompt()
		target := parseQuadrantKey(msg.String())
		if todo, ok := m.currentTodo(); ok {
			m.focus = target
			return m, setQuadrantCmd(m.store, todo.ID, target)
		}
	default:
		m.clearDeletePrompt()
	}

	return m, nil
}

func (m model) updateAddMode(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	m.pendingDeleteID = 0
	switch msg.String() {
	case "ctrl+c":
		return m, tea.Quit
	case "esc":
		m.mode = modeBoard
		m.input.Blur()
		m.status = "Add cancelled"
		return m, nil
	case "tab", "right":
		m.addQuadrant = nextQuadrant(m.addQuadrant)
		m.status = fmt.Sprintf("New task in %s", m.addQuadrant.ShortLabel())
		return m, nil
	case "shift+tab", "left":
		m.addQuadrant = previousQuadrant(m.addQuadrant)
		m.status = fmt.Sprintf("New task in %s", m.addQuadrant.ShortLabel())
		return m, nil
	case "1", "2", "3", "4":
		m.addQuadrant = parseQuadrantKey(msg.String())
		m.status = fmt.Sprintf("New task in %s", m.addQuadrant.ShortLabel())
		return m, nil
	case "enter":
		title := strings.TrimSpace(m.input.Value())
		if title == "" {
			m.status = "Task title cannot be empty"
			m.lastErr = fmt.Errorf("task title cannot be empty")
			return m, nil
		}
		m.mode = modeBoard
		m.input.Blur()
		m.focus = m.addQuadrant
		return m, addTodoCmd(m.store, title, m.addQuadrant)
	}

	var cmd tea.Cmd
	m.input, cmd = m.input.Update(msg)
	return m, cmd
}

func (m model) updateSearchMode(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	m.pendingDeleteID = 0
	switch msg.String() {
	case "ctrl+c":
		return m, tea.Quit
	case "esc":
		m.mode = modeBoard
		m.input.Blur()
		m.status = "Search cancelled"
		return m, nil
	case "enter":
		m.search = strings.TrimSpace(m.input.Value())
		m.mode = modeBoard
		m.input.Blur()
		if m.search == "" {
			m.status = "Search cleared"
		} else {
			m.status = fmt.Sprintf("Search: %q", m.search)
		}
		return m, loadTodosCmd(m.store, m.search)
	}

	var cmd tea.Cmd
	m.input, cmd = m.input.Update(msg)
	return m, cmd
}

func (m model) View() string {
	title := titleStyle.Render("Todo CLI")
	subtitle := metaStyle.Render(fmt.Sprintf("SQLite: %s", m.dbPath))

	summary := lipgloss.JoinHorizontal(
		lipgloss.Left,
		metaStyle.Render(fmt.Sprintf("All %d", m.stats.All)),
		"   ",
		renderSummaryStat(store.QuadrantOne, m.stats.QuadrantOne),
		"   ",
		renderSummaryStat(store.QuadrantTwo, m.stats.QuadrantTwo),
		"   ",
		renderSummaryStat(store.QuadrantThree, m.stats.QuadrantThree),
		"   ",
		renderSummaryStat(store.QuadrantFour, m.stats.QuadrantFour),
	)

	help := metaStyle.Render("h/l switch quadrant  j/k move  1-4 move task  a add  / search  enter toggle  d delete  esc clear search  q quit")

	var footer string
	switch m.mode {
	case modeAdd:
		footer = quadrantPanelStyle(m.addQuadrant, true).
			Width(max(44, m.width-8)).
			Render("New task\nQuadrant: " + renderQuadrantPicker(m.addQuadrant) + "\n" + m.input.View())
	case modeSearch:
		footer = quadrantPanelStyle(m.focus, true).
			Width(max(44, m.width-8)).
			Render("Search\n" + m.input.View())
	default:
		if m.lastErr != nil {
			footer = errorStyle.Render(m.status)
		} else {
			footer = statusStyle.Render(m.status)
		}
	}

	view := lipgloss.JoinVertical(
		lipgloss.Left,
		lipgloss.JoinHorizontal(lipgloss.Left, title, "  ", subtitle),
		summary,
		m.renderBoard(),
		footer,
		help,
	)

	return appStyle.Render(view)
}

func (m model) renderBoard() string {
	panelWidth := max(36, (max(m.width, 100)-10)/2)
	panelHeight := max(10, (max(m.height, 28)-12)/2)

	top := lipgloss.JoinHorizontal(
		lipgloss.Top,
		m.renderQuadrantPanel(store.QuadrantOne, panelWidth, panelHeight),
		"  ",
		m.renderQuadrantPanel(store.QuadrantTwo, panelWidth, panelHeight),
	)

	bottom := lipgloss.JoinHorizontal(
		lipgloss.Top,
		m.renderQuadrantPanel(store.QuadrantThree, panelWidth, panelHeight),
		"  ",
		m.renderQuadrantPanel(store.QuadrantFour, panelWidth, panelHeight),
	)

	return lipgloss.JoinVertical(lipgloss.Left, top, bottom)
}

func (m model) renderQuadrantPanel(quadrant store.Quadrant, width, height int) string {
	todos := m.board[quadrant]
	header := lipgloss.JoinHorizontal(
		lipgloss.Left,
		quadrantTitleStyle(quadrant).Render(quadrant.ShortLabel()),
		" ",
		quadrantCountStyle(quadrant).Render(fmt.Sprintf("%d", len(todos))),
	)
	lines := []string{header}
	itemSlots := max(1, height-3)

	if len(todos) == 0 {
		lines = append(lines, quadrantMetaStyle(quadrant).Render("No tasks"))
	} else {
		start, visible := visibleTodos(todos, m.cursors[quadrant], itemSlots)
		if start > 0 {
			lines = append(lines, quadrantMetaStyle(quadrant).Render("..."))
		}

		for i, todo := range visible {
			actualIndex := start + i
			line := renderTodoLine(todo, quadrant, quadrant == m.focus && actualIndex == m.cursors[quadrant], width-6)
			lines = append(lines, line)
		}

		if start+len(visible) < len(todos) {
			lines = append(lines, quadrantMetaStyle(quadrant).Render("..."))
		}
	}

	content := strings.Join(lines, "\n")
	return quadrantPanelStyle(quadrant, quadrant == m.focus).
		Width(width).
		Height(height).
		Render(content)
}

func renderTodoLine(todo store.Todo, quadrant store.Quadrant, selected bool, width int) string {
	cursor := " "
	if selected {
		cursor = "▸"
	}

	check := " "
	if todo.Done {
		check = "x"
	}

	label := truncate(todo.Title, max(12, width-10))
	line := fmt.Sprintf("%s [%s] #%d %s", cursor, check, todo.ID, label)
	if todo.Done {
		line = doneStyle.Render(line)
	}
	return line
}

func (m model) currentTodo() (store.Todo, bool) {
	todos := m.board[m.focus]
	cursor := m.cursors[m.focus]
	if len(todos) == 0 || cursor < 0 || cursor >= len(todos) {
		return store.Todo{}, false
	}
	return todos[cursor], true
}

func (m *model) clearDeletePrompt() {
	if m.pendingDeleteID != 0 {
		m.pendingDeleteID = 0
		m.status = "Delete cancelled"
		m.lastErr = nil
	}
}

func (m model) clampCursors() {
	for _, quadrant := range store.AllQuadrants() {
		todos := m.board[quadrant]
		if len(todos) == 0 {
			m.cursors[quadrant] = 0
			continue
		}
		if m.cursors[quadrant] >= len(todos) {
			m.cursors[quadrant] = len(todos) - 1
		}
		if m.cursors[quadrant] < 0 {
			m.cursors[quadrant] = 0
		}
	}
}

func quadrantPanelStyle(quadrant store.Quadrant, focused bool) lipgloss.Style {
	style := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		Padding(0, 1)

	color := quadrantMutedColor(quadrant)

	if focused {
		return style.BorderForeground(quadrantColor(quadrant))
	}
	return style.BorderForeground(color)
}

func nextQuadrant(current store.Quadrant) store.Quadrant {
	switch current {
	case store.QuadrantOne:
		return store.QuadrantTwo
	case store.QuadrantTwo:
		return store.QuadrantThree
	case store.QuadrantThree:
		return store.QuadrantFour
	default:
		return store.QuadrantOne
	}
}

func previousQuadrant(current store.Quadrant) store.Quadrant {
	switch current {
	case store.QuadrantFour:
		return store.QuadrantThree
	case store.QuadrantThree:
		return store.QuadrantTwo
	case store.QuadrantTwo:
		return store.QuadrantOne
	default:
		return store.QuadrantFour
	}
}

func parseQuadrantKey(key string) store.Quadrant {
	switch key {
	case "1":
		return store.QuadrantOne
	case "2":
		return store.QuadrantTwo
	case "3":
		return store.QuadrantThree
	case "4":
		return store.QuadrantFour
	default:
		return store.QuadrantTwo
	}
}

func renderQuadrantPicker(current store.Quadrant) string {
	parts := make([]string, 0, 4)
	for _, quadrant := range store.AllQuadrants() {
		if quadrant == current {
			parts = append(parts, quadrantTitleStyle(quadrant).Render(quadrant.ShortLabel()))
			continue
		}
		parts = append(parts, quadrantMetaStyle(quadrant).Render(quadrant.ShortLabel()))
	}
	return lipgloss.JoinHorizontal(lipgloss.Left, parts...)
}

func renderSummaryStat(quadrant store.Quadrant, count int) string {
	return quadrantMetaStyle(quadrant).Render(fmt.Sprintf("%s %d", quadrant.ShortLabel(), count))
}

func quadrantColor(quadrant store.Quadrant) lipgloss.Color {
	switch quadrant {
	case store.QuadrantOne:
		return lipgloss.Color("#C2410C")
	case store.QuadrantTwo:
		return lipgloss.Color("#1D4ED8")
	case store.QuadrantThree:
		return lipgloss.Color("#A16207")
	case store.QuadrantFour:
		return lipgloss.Color("#4B5563")
	default:
		return lipgloss.Color("#4B5563")
	}
}

func quadrantMutedColor(quadrant store.Quadrant) lipgloss.Color {
	switch quadrant {
	case store.QuadrantOne:
		return lipgloss.Color("#EAAC8B")
	case store.QuadrantTwo:
		return lipgloss.Color("#93C5FD")
	case store.QuadrantThree:
		return lipgloss.Color("#FCD34D")
	case store.QuadrantFour:
		return lipgloss.Color("#9CA3AF")
	default:
		return lipgloss.Color("#9CA3AF")
	}
}

func quadrantTitleStyle(quadrant store.Quadrant) lipgloss.Style {
	return quadrantTitleBaseStyle.Foreground(quadrantColor(quadrant))
}

func quadrantMetaStyle(quadrant store.Quadrant) lipgloss.Style {
	return metaStyle.Foreground(quadrantColor(quadrant))
}

func quadrantCountStyle(quadrant store.Quadrant) lipgloss.Style {
	return highlightedMetaStyle.Foreground(quadrantColor(quadrant))
}

func visibleTodos(todos []store.Todo, cursor, slots int) (int, []store.Todo) {
	if len(todos) <= slots {
		return 0, todos
	}

	start := cursor - slots/2
	if start < 0 {
		start = 0
	}
	if start+slots > len(todos) {
		start = len(todos) - slots
	}

	return start, todos[start : start+slots]
}

func truncate(text string, limit int) string {
	runes := []rune(text)
	if len(runes) <= limit {
		return text
	}
	if limit <= 1 {
		return string(runes[:limit])
	}
	return string(runes[:limit-1]) + "…"
}

func emptyBoard() map[store.Quadrant][]store.Todo {
	board := make(map[store.Quadrant][]store.Todo, 4)
	for _, quadrant := range store.AllQuadrants() {
		board[quadrant] = nil
	}
	return board
}

func partitionTodos(todos []store.Todo) map[store.Quadrant][]store.Todo {
	board := emptyBoard()
	for _, todo := range todos {
		quadrant := todo.Quadrant()
		board[quadrant] = append(board[quadrant], todo)
	}
	return board
}

func loadTodosCmd(todoStore *store.Store, search string) tea.Cmd {
	return func() tea.Msg {
		todos, err := todoStore.ListTodos(search)
		if err != nil {
			return errMsg{err: err}
		}

		stats, err := todoStore.Stats(search)
		if err != nil {
			return errMsg{err: err}
		}

		return todosLoadedMsg{todos: todos, stats: stats}
	}
}

func addTodoCmd(todoStore *store.Store, title string, quadrant store.Quadrant) tea.Cmd {
	return func() tea.Msg {
		if err := todoStore.AddTodo(title, quadrant); err != nil {
			return operationDoneMsg{err: err}
		}
		return operationDoneMsg{status: fmt.Sprintf("Added %q to %s", title, quadrant.ShortLabel())}
	}
}

func toggleTodoCmd(todoStore *store.Store, id int64) tea.Cmd {
	return func() tea.Msg {
		if err := todoStore.ToggleTodo(id); err != nil {
			return operationDoneMsg{err: err}
		}
		return operationDoneMsg{status: fmt.Sprintf("Toggled task #%d", id)}
	}
}

func deleteTodoCmd(todoStore *store.Store, id int64, title string) tea.Cmd {
	return func() tea.Msg {
		if err := todoStore.DeleteTodo(id); err != nil {
			return operationDoneMsg{err: err}
		}
		return operationDoneMsg{status: fmt.Sprintf("Deleted %q", title)}
	}
}

func setQuadrantCmd(todoStore *store.Store, id int64, quadrant store.Quadrant) tea.Cmd {
	return func() tea.Msg {
		if err := todoStore.SetQuadrant(id, quadrant); err != nil {
			return operationDoneMsg{err: err}
		}
		return operationDoneMsg{status: fmt.Sprintf("Moved task #%d to %s", id, quadrant.ShortLabel())}
	}
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func main() {
	dbPath, err := store.DefaultDBPath()
	if err != nil {
		fmt.Fprintf(os.Stderr, "resolve database path: %v\n", err)
		os.Exit(1)
	}

	todoStore, err := store.Open(dbPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "open database: %v\n", err)
		os.Exit(1)
	}
	defer todoStore.Close()

	p := tea.NewProgram(initialModel(todoStore, dbPath), tea.WithAltScreen())
	if _, err := p.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "run app: %v\n", err)
		os.Exit(1)
	}
}
