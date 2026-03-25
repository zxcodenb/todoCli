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
	store       *store.Store
	dbPath      string
	width       int
	height      int
	todos       []store.Todo
	board       map[store.Quadrant][]store.Todo
	stats       store.Stats
	focus       store.Quadrant
	cursors     map[store.Quadrant]int
	mode        inputMode
	input       textinput.Model
	addQuadrant store.Quadrant
	search      string
	status      string
	lastErr     error
}

var (
	appStyle = lipgloss.NewStyle().
			Padding(1, 2)
	headerRowStyle = lipgloss.NewStyle().
			PaddingBottom(1)
	titleStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("#FFF8E7")).
			Background(lipgloss.Color("#D94841")).
			Padding(0, 1)
	subtitleStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#6B7280"))
	metaStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#5F5F5F"))
	helpStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#6B7280")).
			PaddingTop(1)
	statusStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#12664F")).
			Bold(true)
	errorStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#A61B1B")).
			Bold(true)
	selectedStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#FFFAF0")).
			Background(lipgloss.Color("#0F766E"))
	doneStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#7A7A7A")).
			Strikethrough(true)
	idleBadgeStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#4B5563")).
			Padding(0, 1)
	statChipStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#1F2937")).
			Background(lipgloss.Color("#E5E7EB")).
			Padding(0, 1)
	searchChipStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#F8FAFC")).
			Background(lipgloss.Color("#0F766E")).
			Padding(0, 1)
	taskMetaStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#94A3B8"))
	emptyStateStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#9CA3AF")).
			Italic(true)
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
		if m.cursors[m.focus] > 0 {
			m.cursors[m.focus]--
		}
	case "down", "j":
		if m.cursors[m.focus] < len(m.board[m.focus])-1 {
			m.cursors[m.focus]++
		}
	case "left", "h":
		m.focus = previousQuadrant(m.focus)
	case "right", "l", "tab":
		m.focus = nextQuadrant(m.focus)
	case "shift+tab":
		m.focus = previousQuadrant(m.focus)
	case "a", "n":
		m.mode = modeAdd
		m.input.SetValue("")
		m.input.Placeholder = "Write a task and press Enter"
		m.input.Focus()
		m.addQuadrant = m.focus
		m.status = "Add a new task"
	case "/":
		m.mode = modeSearch
		m.input.SetValue(m.search)
		m.input.Placeholder = "Search tasks"
		m.input.Focus()
		m.status = "Search and press Enter"
	case "r":
		m.status = "Reloaded"
		return m, loadTodosCmd(m.store, m.search)
	case "esc":
		if m.search != "" {
			m.search = ""
			m.status = "Search cleared"
			return m, loadTodosCmd(m.store, m.search)
		}
	case "enter", " ":
		if todo, ok := m.currentTodo(); ok {
			return m, toggleTodoCmd(m.store, todo.ID)
		}
	case "d", "x", "backspace", "delete":
		if todo, ok := m.currentTodo(); ok {
			return m, deleteTodoCmd(m.store, todo.ID, todo.Title)
		}
	case "1", "2", "3", "4":
		target := parseQuadrantKey(msg.String())
		if todo, ok := m.currentTodo(); ok {
			m.focus = target
			return m, setQuadrantCmd(m.store, todo.ID, target)
		}
	}

	return m, nil
}

func (m model) updateAddMode(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
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
	subtitle := subtitleStyle.Render(fmt.Sprintf("SQLite: %s", m.dbPath))
	summary := m.renderSummary()
	help := helpStyle.Render("h/l switch quadrant  j/k move  1-4 move task  a add  / search  enter toggle  d delete  esc clear search  q quit")

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
		headerRowStyle.Render(lipgloss.JoinHorizontal(lipgloss.Left, title, "  ", subtitle)),
		summary,
		m.renderBoard(),
		footer,
		help,
	)

	return appStyle.Render(view)
}

func (m model) renderSummary() string {
	chips := []string{
		statChipStyle.Render(fmt.Sprintf("All %d", m.stats.All)),
		renderQuadrantStat(store.QuadrantOne, m.stats.QuadrantOne),
		renderQuadrantStat(store.QuadrantTwo, m.stats.QuadrantTwo),
		renderQuadrantStat(store.QuadrantThree, m.stats.QuadrantThree),
		renderQuadrantStat(store.QuadrantFour, m.stats.QuadrantFour),
	}

	if m.search != "" {
		chips = append(chips, searchChipStyle.Render(fmt.Sprintf("Search %q", truncate(m.search, 24))))
	}

	return lipgloss.JoinHorizontal(lipgloss.Left, chips...)
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
		quadrantCountStyle(quadrant, false).Render(fmt.Sprintf("%d", len(todos))),
	)

	if quadrant == m.focus {
		header = lipgloss.JoinHorizontal(
			lipgloss.Left,
			header,
			" ",
			quadrantCountStyle(quadrant, true).Render("FOCUS"),
		)
	}

	desc := quadrantDescStyle(quadrant).Render(quadrant.ActionLabel())

	lines := []string{header, desc}
	itemSlots := max(1, height-5)

	if len(todos) == 0 {
		lines = append(lines, emptyStateStyle.Render("No tasks"))
	} else {
		start, visible := visibleTodos(todos, m.cursors[quadrant], itemSlots)
		if start > 0 {
			lines = append(lines, metaStyle.Render("..."))
		}

		for i, todo := range visible {
			actualIndex := start + i
			line := renderTodoLine(todo, quadrant, quadrant == m.focus && actualIndex == m.cursors[quadrant], width-6)
			lines = append(lines, line)
		}

		if start+len(visible) < len(todos) {
			lines = append(lines, metaStyle.Render("..."))
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
	line := fmt.Sprintf("%s [%s] %s", cursor, check, label)
	meta := taskMetaStyle.Render(fmt.Sprintf("#%d", todo.ID))
	body := lipgloss.JoinVertical(lipgloss.Left, line, meta)

	if todo.Done {
		body = lipgloss.JoinVertical(
			lipgloss.Left,
			doneStyle.Render(line),
			taskMetaStyle.Render(fmt.Sprintf("#%d done", todo.ID)),
		)
	}
	if selected {
		body = selectedTaskStyle(quadrant).
			Width(max(12, width)).
			Padding(0, 1).
			Render(body)
		return body
	}
	return lipgloss.NewStyle().
		Width(max(12, width)).
		Padding(0, 1).
		Render(body)
}

func (m model) currentTodo() (store.Todo, bool) {
	todos := m.board[m.focus]
	cursor := m.cursors[m.focus]
	if len(todos) == 0 || cursor < 0 || cursor >= len(todos) {
		return store.Todo{}, false
	}
	return todos[cursor], true
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

	color := quadrantColor(quadrant)

	if focused {
		return style.
			BorderForeground(color).
			Background(quadrantPanelBackground(quadrant, true))
	}
	return style.
		BorderForeground(lipgloss.Color("#D1D5DB")).
		Background(quadrantPanelBackground(quadrant, false))
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
		parts = append(parts, idleBadgeStyle.Render(quadrant.ShortLabel()))
	}
	return lipgloss.JoinHorizontal(lipgloss.Left, parts...)
}

func renderQuadrantStat(quadrant store.Quadrant, count int) string {
	return quadrantCountStyle(quadrant, false).Render(fmt.Sprintf("%s %d", quadrant.ShortLabel(), count))
}

func quadrantColor(quadrant store.Quadrant) lipgloss.Color {
	switch quadrant {
	case store.QuadrantOne:
		return lipgloss.Color("#B42318")
	case store.QuadrantTwo:
		return lipgloss.Color("#1D4ED8")
	case store.QuadrantThree:
		return lipgloss.Color("#B45309")
	case store.QuadrantFour:
		return lipgloss.Color("#475569")
	default:
		return lipgloss.Color("#64748B")
	}
}

func quadrantPanelBackground(quadrant store.Quadrant, focused bool) lipgloss.Color {
	if focused {
		switch quadrant {
		case store.QuadrantOne:
			return lipgloss.Color("#FFF1F2")
		case store.QuadrantTwo:
			return lipgloss.Color("#EFF6FF")
		case store.QuadrantThree:
			return lipgloss.Color("#FFFBEB")
		case store.QuadrantFour:
			return lipgloss.Color("#F8FAFC")
		}
	}

	switch quadrant {
	case store.QuadrantOne:
		return lipgloss.Color("#FFFBFB")
	case store.QuadrantTwo:
		return lipgloss.Color("#FAFCFF")
	case store.QuadrantThree:
		return lipgloss.Color("#FFFDF7")
	case store.QuadrantFour:
		return lipgloss.Color("#FCFCFD")
	default:
		return lipgloss.Color("#FFFFFF")
	}
}

func quadrantTitleStyle(quadrant store.Quadrant) lipgloss.Style {
	return lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color("#F8FAFC")).
		Background(quadrantColor(quadrant)).
		Padding(0, 1)
}

func quadrantCountStyle(quadrant store.Quadrant, focused bool) lipgloss.Style {
	style := lipgloss.NewStyle().
		Foreground(quadrantColor(quadrant)).
		Background(quadrantPanelBackground(quadrant, false)).
		Border(lipgloss.RoundedBorder()).
		BorderForeground(quadrantColor(quadrant)).
		Padding(0, 1)

	if focused {
		return style.
			Foreground(lipgloss.Color("#F8FAFC")).
			Background(quadrantColor(quadrant))
	}
	return style
}

func quadrantDescStyle(quadrant store.Quadrant) lipgloss.Style {
	return lipgloss.NewStyle().
		Foreground(quadrantColor(quadrant))
}

func selectedTaskStyle(quadrant store.Quadrant) lipgloss.Style {
	return lipgloss.NewStyle().
		Foreground(lipgloss.Color("#0F172A")).
		Background(quadrantPanelBackground(quadrant, true)).
		Border(lipgloss.RoundedBorder()).
		BorderForeground(quadrantColor(quadrant))
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
