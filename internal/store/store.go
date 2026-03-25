package store

import (
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	_ "github.com/mattn/go-sqlite3"
)

type Quadrant int

const (
	QuadrantOne Quadrant = iota + 1
	QuadrantTwo
	QuadrantThree
	QuadrantFour
)

type Todo struct {
	ID        int64
	Title     string
	Important bool
	Urgent    bool
	Done      bool
	CreatedAt time.Time
	UpdatedAt time.Time
}

type Stats struct {
	All           int
	QuadrantOne   int
	QuadrantTwo   int
	QuadrantThree int
	QuadrantFour  int
}

type Store struct {
	db *sql.DB
}

func AllQuadrants() []Quadrant {
	return []Quadrant{QuadrantOne, QuadrantTwo, QuadrantThree, QuadrantFour}
}

func (q Quadrant) ShortLabel() string {
	switch q {
	case QuadrantOne:
		return "Q1 立即做"
	case QuadrantTwo:
		return "Q2 计划做"
	case QuadrantThree:
		return "Q3 授权做"
	case QuadrantFour:
		return "Q4 减少做"
	default:
		return "Q2 计划做"
	}
}

func (q Quadrant) ActionLabel() string {
	switch q {
	case QuadrantOne:
		return "第一象限 重要且紧急，立即做"
	case QuadrantTwo:
		return "第二象限 重要但不紧急，计划做"
	case QuadrantThree:
		return "第三象限 紧急但不重要，授权做"
	case QuadrantFour:
		return "第四象限 不重要且不紧急，减少做"
	default:
		return "第二象限 重要但不紧急，计划做"
	}
}

func (q Quadrant) flags() (important bool, urgent bool) {
	switch q {
	case QuadrantOne:
		return true, true
	case QuadrantTwo:
		return true, false
	case QuadrantThree:
		return false, true
	case QuadrantFour:
		return false, false
	default:
		return true, false
	}
}

func normalizeQuadrant(q Quadrant) Quadrant {
	switch q {
	case QuadrantOne, QuadrantTwo, QuadrantThree, QuadrantFour:
		return q
	default:
		return 0
	}
}

func QuadrantFromFlags(important, urgent bool) Quadrant {
	switch {
	case important && urgent:
		return QuadrantOne
	case important && !urgent:
		return QuadrantTwo
	case !important && urgent:
		return QuadrantThree
	default:
		return QuadrantFour
	}
}

func (t Todo) Quadrant() Quadrant {
	return QuadrantFromFlags(t.Important, t.Urgent)
}

func DefaultDBPath() (string, error) {
	if envPath := strings.TrimSpace(os.Getenv("TODOCLI_DB_PATH")); envPath != "" {
		return filepath.Abs(envPath)
	}

	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolve home directory: %w", err)
	}

	return filepath.Join(home, ".todo-cli", "todos.db"), nil
}

func Open(path string) (*Store, error) {
	if path == "" {
		return nil, errors.New("database path is empty")
	}

	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, fmt.Errorf("create database directory: %w", err)
	}

	db, err := sql.Open("sqlite3", path)
	if err != nil {
		return nil, fmt.Errorf("open sqlite database: %w", err)
	}

	store := &Store{db: db}
	if err := store.init(); err != nil {
		db.Close()
		return nil, err
	}

	return store, nil
}

func (s *Store) Close() error {
	if s == nil || s.db == nil {
		return nil
	}
	return s.db.Close()
}

func (s *Store) init() error {
	pragmas := []string{
		"PRAGMA journal_mode = WAL;",
		"PRAGMA busy_timeout = 5000;",
		"PRAGMA foreign_keys = ON;",
	}

	for _, stmt := range pragmas {
		if _, err := s.db.Exec(stmt); err != nil {
			return fmt.Errorf("apply sqlite pragma: %w", err)
		}
	}

	schema := `
	CREATE TABLE IF NOT EXISTS todos (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		title TEXT NOT NULL,
		done INTEGER NOT NULL DEFAULT 0,
		created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
		updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
	);
	`

	if _, err := s.db.Exec(schema); err != nil {
		return fmt.Errorf("create schema: %w", err)
	}

	columns, err := s.columns()
	if err != nil {
		return err
	}

	if err := s.ensureQuadrantColumns(columns); err != nil {
		return err
	}

	if _, err := s.db.Exec(`
	CREATE INDEX IF NOT EXISTS idx_todos_quadrant_done_updated
	ON todos (important DESC, urgent DESC, done ASC, updated_at DESC);
	`); err != nil {
		return fmt.Errorf("create quadrant index: %w", err)
	}

	return nil
}

func (s *Store) columns() (map[string]bool, error) {
	rows, err := s.db.Query("PRAGMA table_info(todos)")
	if err != nil {
		return nil, fmt.Errorf("inspect todos schema: %w", err)
	}
	defer rows.Close()

	columns := make(map[string]bool)
	for rows.Next() {
		var (
			cid       int
			name      string
			dataType  string
			notNull   int
			defaultV  sql.NullString
			primaryKV int
		)
		if err := rows.Scan(&cid, &name, &dataType, &notNull, &defaultV, &primaryKV); err != nil {
			return nil, fmt.Errorf("scan todos schema: %w", err)
		}
		columns[name] = true
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate todos schema: %w", err)
	}

	return columns, nil
}

func (s *Store) ensureQuadrantColumns(columns map[string]bool) error {
	addedImportant := false
	addedUrgent := false

	if !columns["important"] {
		if _, err := s.db.Exec("ALTER TABLE todos ADD COLUMN important INTEGER NOT NULL DEFAULT 1"); err != nil {
			return fmt.Errorf("add important column: %w", err)
		}
		addedImportant = true
	}

	if !columns["urgent"] {
		if _, err := s.db.Exec("ALTER TABLE todos ADD COLUMN urgent INTEGER NOT NULL DEFAULT 0"); err != nil {
			return fmt.Errorf("add urgent column: %w", err)
		}
		addedUrgent = true
	}

	if (addedImportant || addedUrgent) && columns["priority"] {
		if _, err := s.db.Exec(`
		UPDATE todos
		SET important = CASE WHEN priority IN (2, 3) THEN 1 ELSE 0 END,
		    urgent = CASE WHEN priority = 3 THEN 1 ELSE 0 END
		`); err != nil {
			return fmt.Errorf("migrate priority into quadrants: %w", err)
		}
	}

	return nil
}

func (s *Store) ListTodos(search string) ([]Todo, error) {
	query := `
	SELECT id, title, important, urgent, done, created_at, updated_at
	FROM todos
	`

	args := make([]any, 0, 1)
	search = strings.TrimSpace(search)
	if search != "" {
		query += " WHERE title LIKE '%' || ? || '%' COLLATE NOCASE"
		args = append(args, search)
	}

	query += " ORDER BY important DESC, urgent DESC, done ASC, updated_at DESC, id DESC"

	rows, err := s.db.Query(query, args...)
	if err != nil {
		return nil, fmt.Errorf("list todos: %w", err)
	}
	defer rows.Close()

	todos := make([]Todo, 0)
	for rows.Next() {
		var todo Todo
		if err := rows.Scan(&todo.ID, &todo.Title, &todo.Important, &todo.Urgent, &todo.Done, &todo.CreatedAt, &todo.UpdatedAt); err != nil {
			return nil, fmt.Errorf("scan todo: %w", err)
		}
		todos = append(todos, todo)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate todos: %w", err)
	}

	return todos, nil
}

func (s *Store) Stats(search string) (Stats, error) {
	query := `
	SELECT
		COUNT(*) AS all_count,
		COALESCE(SUM(CASE WHEN important = 1 AND urgent = 1 THEN 1 ELSE 0 END), 0) AS q1_count,
		COALESCE(SUM(CASE WHEN important = 1 AND urgent = 0 THEN 1 ELSE 0 END), 0) AS q2_count,
		COALESCE(SUM(CASE WHEN important = 0 AND urgent = 1 THEN 1 ELSE 0 END), 0) AS q3_count,
		COALESCE(SUM(CASE WHEN important = 0 AND urgent = 0 THEN 1 ELSE 0 END), 0) AS q4_count
	FROM todos
	`

	args := make([]any, 0, 1)
	search = strings.TrimSpace(search)
	if search != "" {
		query += " WHERE title LIKE '%' || ? || '%' COLLATE NOCASE"
		args = append(args, search)
	}

	var stats Stats
	if err := s.db.QueryRow(query, args...).Scan(
		&stats.All,
		&stats.QuadrantOne,
		&stats.QuadrantTwo,
		&stats.QuadrantThree,
		&stats.QuadrantFour,
	); err != nil {
		return Stats{}, fmt.Errorf("load stats: %w", err)
	}

	return stats, nil
}

func (s *Store) AddTodo(title string, quadrant Quadrant) error {
	title = strings.TrimSpace(title)
	if title == "" {
		return errors.New("task title cannot be empty")
	}
	if quadrant = normalizeQuadrant(quadrant); quadrant == 0 {
		return errors.New("invalid quadrant")
	}

	important, urgent := quadrant.flags()

	const query = `
	INSERT INTO todos (title, important, urgent, done, created_at, updated_at)
	VALUES (?, ?, ?, 0, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP)
	`

	if _, err := s.db.Exec(query, title, important, urgent); err != nil {
		return fmt.Errorf("add todo: %w", err)
	}

	return nil
}

func (s *Store) ToggleTodo(id int64) error {
	const query = `
	UPDATE todos
	SET done = CASE done WHEN 0 THEN 1 ELSE 0 END,
	    updated_at = CURRENT_TIMESTAMP
	WHERE id = ?
	`

	result, err := s.db.Exec(query, id)
	if err != nil {
		return fmt.Errorf("toggle todo: %w", err)
	}

	affected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("inspect toggle result: %w", err)
	}
	if affected == 0 {
		return fmt.Errorf("todo #%d not found", id)
	}

	return nil
}

func (s *Store) DeleteTodo(id int64) error {
	result, err := s.db.Exec("DELETE FROM todos WHERE id = ?", id)
	if err != nil {
		return fmt.Errorf("delete todo: %w", err)
	}

	affected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("inspect delete result: %w", err)
	}
	if affected == 0 {
		return fmt.Errorf("todo #%d not found", id)
	}

	return nil
}

func (s *Store) SetQuadrant(id int64, quadrant Quadrant) error {
	if quadrant = normalizeQuadrant(quadrant); quadrant == 0 {
		return errors.New("invalid quadrant")
	}

	important, urgent := quadrant.flags()
	const query = `
	UPDATE todos
	SET important = ?, urgent = ?, updated_at = CURRENT_TIMESTAMP
	WHERE id = ?
	`

	result, err := s.db.Exec(query, important, urgent, id)
	if err != nil {
		return fmt.Errorf("set quadrant: %w", err)
	}

	affected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("inspect quadrant result: %w", err)
	}
	if affected == 0 {
		return fmt.Errorf("todo #%d not found", id)
	}

	return nil
}
