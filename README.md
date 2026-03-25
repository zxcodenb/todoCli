# Todo CLI

A personal todolist app built with Go, Bubble Tea, and SQLite.

## Features

- Full-screen terminal UI
- SQLite persistence
- Eisenhower Matrix layout
- Search and keyboard-first workflow

## Run

```bash
go run .
```

The database is stored at `~/.todo-cli/todos.db` by default.

You can override it with:

```bash
TODOCLI_DB_PATH=/custom/path/todos.db go run .
```

## Quadrants

- `Q1`: 重要且紧急，立即做
- `Q2`: 重要但不紧急，计划做
- `Q3`: 紧急但不重要，授权做
- `Q4`: 不重要且不紧急，减少做

Existing tasks from the previous priority-based version are migrated like this:

- `High -> Q1`
- `Medium -> Q2`
- `Low -> Q4`

## Keys

- `h` / `l` or left/right: switch quadrant
- `j` / `k` or up/down: move inside the current quadrant
- `a`: add a task
- In add mode, `1 / 2 / 3 / 4` or left/right picks the target quadrant
- `/`: search
- `1 / 2 / 3 / 4`: move the selected task to `Q1 / Q2 / Q3 / Q4`
- `enter` or `space`: toggle done
- `d`: delete selected task
- `esc`: clear active search
- `r`: reload
- `q`: quit
