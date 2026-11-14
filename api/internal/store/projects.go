package store

import (
	"context"
	"database/sql"
	"errors"
	"sort"
	"strings"
	"time"

	"tracker/api/internal/auth"
)

// Project is a project with its children. Which children are filled depends
// on the call: the board listing carries only the 2 most recent log entries.
type Project struct {
	ID          string     `json:"id"`
	Name        string     `json:"name"`
	Description string     `json:"description"`
	Position    int64      `json:"position"`
	ArchivedAt  *time.Time `json:"archived_at"`
	CreatedAt   time.Time  `json:"created_at"`
	UpdatedAt   time.Time  `json:"updated_at"`
	Tags        []string   `json:"tags"`
	Tasks       []Task     `json:"tasks"`
	Log         []LogEntry `json:"log"`
}

// Task is a project task.
type Task struct {
	ID          string     `json:"id"`
	ProjectID   string     `json:"project_id"`
	Title       string     `json:"title"`
	Done        bool       `json:"done"`
	Position    int64      `json:"position"`
	CompletedAt *time.Time `json:"completed_at"`
	CreatedAt   time.Time  `json:"created_at"`
	UpdatedAt   time.Time  `json:"updated_at"`
}

// LogEntry is a dated paragraph in a project's log.
type LogEntry struct {
	ID        string    `json:"id"`
	ProjectID string    `json:"project_id"`
	Body      string    `json:"body"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// Tag is a label with the number of projects carrying it.
type Tag struct {
	ID           string `json:"id"`
	Name         string `json:"name"`
	ProjectCount int    `json:"project_count"`
}

// querier is satisfied by *sql.DB and *sql.Tx.
type querier interface {
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
}

const projectCols = `p.id, p.name, p.description, p.position, p.archived_at, p.created_at, p.updated_at`

func scanProject(sc interface{ Scan(...any) error }) (Project, error) {
	var p Project
	var archived sql.NullInt64
	var created, updated int64
	if err := sc.Scan(&p.ID, &p.Name, &p.Description, &p.Position, &archived, &created, &updated); err != nil {
		return Project{}, err
	}
	p.ArchivedAt, p.CreatedAt, p.UpdatedAt = fromNullMs(archived), fromMs(created), fromMs(updated)
	p.Tags, p.Tasks, p.Log = []string{}, []Task{}, []LogEntry{}
	return p, nil
}

const taskCols = `t.id, t.project_id, t.title, t.done, t.position, t.completed_at, t.created_at, t.updated_at`

func scanTask(sc interface{ Scan(...any) error }) (Task, error) {
	var t Task
	var done int64
	var completed sql.NullInt64
	var created, updated int64
	if err := sc.Scan(&t.ID, &t.ProjectID, &t.Title, &done, &t.Position, &completed, &created, &updated); err != nil {
		return Task{}, err
	}
	t.Done, t.CompletedAt, t.CreatedAt, t.UpdatedAt = done == 1, fromNullMs(completed), fromMs(created), fromMs(updated)
	return t, nil
}

const logCols = `l.id, l.project_id, l.body, l.created_at, l.updated_at`

func scanLog(sc interface{ Scan(...any) error }) (LogEntry, error) {
	var l LogEntry
	var created, updated int64
	if err := sc.Scan(&l.ID, &l.ProjectID, &l.Body, &created, &updated); err != nil {
		return LogEntry{}, err
	}
	l.CreatedAt, l.UpdatedAt = fromMs(created), fromMs(updated)
	return l, nil
}

// ListProjects returns all of the user's projects (archived ones included,
// marked by archived_at) in board order, each with its tags, all its tasks
// and its 2 most recent log entries.
func (s *Store) ListProjects(ctx context.Context, userID string) ([]Project, error) {
	projects := []Project{}
	err := s.tx(ctx, func(tx *sql.Tx) error {
		rows, err := tx.QueryContext(ctx, `SELECT `+projectCols+` FROM projects p
			WHERE p.user_id = ? ORDER BY p.position, p.created_at`, userID)
		if err != nil {
			return err
		}
		idx := map[string]int{}
		for rows.Next() {
			p, err := scanProject(rows)
			if err != nil {
				rows.Close()
				return err
			}
			idx[p.ID] = len(projects)
			projects = append(projects, p)
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return err
		}
		if len(projects) == 0 {
			return nil
		}
		if err := fillTags(ctx, tx, userID, "", projects, idx); err != nil {
			return err
		}
		if err := fillTasks(ctx, tx, userID, "", projects, idx); err != nil {
			return err
		}
		rows, err = tx.QueryContext(ctx, `SELECT id, project_id, body, created_at, updated_at FROM (
			SELECT `+logCols+`, ROW_NUMBER() OVER (PARTITION BY l.project_id ORDER BY l.created_at DESC, l.rowid DESC) AS rn
			  FROM log_entries l JOIN projects p ON p.id = l.project_id
			 WHERE p.user_id = ?) WHERE rn <= 2 ORDER BY project_id, created_at DESC, rn`, userID)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			l, err := scanLog(rows)
			if err != nil {
				return err
			}
			if i, ok := idx[l.ProjectID]; ok {
				projects[i].Log = append(projects[i].Log, l)
			}
		}
		return rows.Err()
	})
	return projects, err
}

// GetProject returns one project with all of its tasks and log entries.
func (s *Store) GetProject(ctx context.Context, userID, id string) (Project, error) {
	var p Project
	err := s.tx(ctx, func(tx *sql.Tx) error {
		var err error
		p, err = getProject(ctx, tx, userID, id)
		return err
	})
	return p, err
}

func getProject(ctx context.Context, q querier, userID, id string) (Project, error) {
	p, err := scanProject(q.QueryRowContext(ctx, `SELECT `+projectCols+` FROM projects p
		WHERE p.id = ? AND p.user_id = ?`, id, userID))
	if errors.Is(err, sql.ErrNoRows) {
		return Project{}, ErrNotFound
	}
	if err != nil {
		return Project{}, err
	}
	list := []Project{p}
	idx := map[string]int{p.ID: 0}
	if err := fillTags(ctx, q, userID, p.ID, list, idx); err != nil {
		return Project{}, err
	}
	if err := fillTasks(ctx, q, userID, p.ID, list, idx); err != nil {
		return Project{}, err
	}
	rows, err := q.QueryContext(ctx, `SELECT `+logCols+` FROM log_entries l JOIN projects p ON p.id = l.project_id
		WHERE p.user_id = ? AND p.id = ? ORDER BY l.created_at DESC, l.rowid DESC`, userID, p.ID)
	if err != nil {
		return Project{}, err
	}
	defer rows.Close()
	for rows.Next() {
		l, err := scanLog(rows)
		if err != nil {
			return Project{}, err
		}
		list[0].Log = append(list[0].Log, l)
	}
	return list[0], rows.Err()
}

// fillTags loads tag names for the projects in list. If projectID is set,
// only that project is queried.
func fillTags(ctx context.Context, q querier, userID, projectID string, list []Project, idx map[string]int) error {
	rows, err := q.QueryContext(ctx, `SELECT pt.project_id, t.name FROM project_tags pt
		JOIN projects p ON p.id = pt.project_id
		JOIN tags t ON t.id = pt.tag_id
		WHERE p.user_id = ?1 AND t.user_id = ?1 AND (?2 = '' OR p.id = ?2)`, userID, projectID)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var pid, name string
		if err := rows.Scan(&pid, &name); err != nil {
			return err
		}
		if i, ok := idx[pid]; ok {
			list[i].Tags = append(list[i].Tags, name)
		}
	}
	for i := range list {
		sortNames(list[i].Tags)
	}
	return rows.Err()
}

func fillTasks(ctx context.Context, q querier, userID, projectID string, list []Project, idx map[string]int) error {
	rows, err := q.QueryContext(ctx, `SELECT `+taskCols+` FROM tasks t JOIN projects p ON p.id = t.project_id
		WHERE p.user_id = ?1 AND (?2 = '' OR p.id = ?2) ORDER BY t.position, t.created_at`, userID, projectID)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		t, err := scanTask(rows)
		if err != nil {
			return err
		}
		if i, ok := idx[t.ProjectID]; ok {
			list[i].Tasks = append(list[i].Tasks, t)
		}
	}
	return rows.Err()
}

func sortNames(names []string) {
	sort.SliceStable(names, func(i, j int) bool {
		a, b := strings.ToLower(names[i]), strings.ToLower(names[j])
		if a != b {
			return a < b
		}
		return names[i] < names[j]
	})
}

// CreateProject creates a project at the end of the board.
func (s *Store) CreateProject(ctx context.Context, userID, name, description string, tags []string) (Project, error) {
	var p Project
	err := s.tx(ctx, func(tx *sql.Tx) error {
		now := ms(s.now())
		id := auth.NewID()
		if _, err := tx.ExecContext(ctx, `INSERT INTO projects (id, user_id, name, description, position, created_at, updated_at)
			VALUES (?1, ?2, ?3, ?4, (SELECT COALESCE(MAX(position), -1) + 1 FROM projects WHERE user_id = ?2), ?5, ?5)`,
			id, userID, name, description, now); err != nil {
			return err
		}
		if err := setTags(ctx, tx, userID, id, tags, now); err != nil {
			return err
		}
		var err error
		p, err = getProject(ctx, tx, userID, id)
		return err
	})
	return p, err
}

// ProjectPatch lists the fields to change; nil means unchanged.
type ProjectPatch struct {
	Name        *string
	Description *string
	Tags        *[]string
	Archived    *bool
}

// UpdateProject applies patch to one of the user's projects.
func (s *Store) UpdateProject(ctx context.Context, userID, id string, patch ProjectPatch) (Project, error) {
	var p Project
	err := s.tx(ctx, func(tx *sql.Tx) error {
		now := ms(s.now())
		// Ownership check and updated_at bump in one statement.
		if err := affected(tx.ExecContext(ctx, `UPDATE projects SET updated_at = ? WHERE id = ? AND user_id = ?`,
			now, id, userID)); err != nil {
			return err
		}
		if patch.Name != nil {
			if _, err := tx.ExecContext(ctx, `UPDATE projects SET name = ? WHERE id = ? AND user_id = ?`, *patch.Name, id, userID); err != nil {
				return err
			}
		}
		if patch.Description != nil {
			if _, err := tx.ExecContext(ctx, `UPDATE projects SET description = ? WHERE id = ? AND user_id = ?`, *patch.Description, id, userID); err != nil {
				return err
			}
		}
		if patch.Archived != nil {
			var err error
			if *patch.Archived {
				_, err = tx.ExecContext(ctx, `UPDATE projects SET archived_at = COALESCE(archived_at, ?) WHERE id = ? AND user_id = ?`, now, id, userID)
			} else {
				_, err = tx.ExecContext(ctx, `UPDATE projects SET archived_at = NULL WHERE id = ? AND user_id = ?`, id, userID)
			}
			if err != nil {
				return err
			}
		}
		if patch.Tags != nil {
			if err := setTags(ctx, tx, userID, id, *patch.Tags, now); err != nil {
				return err
			}
		}
		var err error
		p, err = getProject(ctx, tx, userID, id)
		return err
	})
	return p, err
}

// DeleteProject deletes a project; its tasks, log entries and tag links go
// with it through foreign-key cascades, and orphaned tags through a trigger.
func (s *Store) DeleteProject(ctx context.Context, userID, id string) error {
	return affected(s.db.ExecContext(ctx, `DELETE FROM projects WHERE id = ? AND user_id = ?`, id, userID))
}

// ReorderProjects sets the board order. ids must list every one of the
// user's projects exactly once.
func (s *Store) ReorderProjects(ctx context.Context, userID string, ids []string) error {
	return s.tx(ctx, func(tx *sql.Tx) error {
		rows, err := tx.QueryContext(ctx, `SELECT id FROM projects WHERE user_id = ?`, userID)
		if err != nil {
			return err
		}
		have := map[string]bool{}
		for rows.Next() {
			var id string
			if err := rows.Scan(&id); err != nil {
				rows.Close()
				return err
			}
			have[id] = true
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return err
		}
		if len(ids) != len(have) {
			return ErrInvalidOrder
		}
		seen := map[string]bool{}
		for _, id := range ids {
			if !have[id] || seen[id] {
				return ErrInvalidOrder
			}
			seen[id] = true
		}
		for pos, id := range ids {
			if _, err := tx.ExecContext(ctx, `UPDATE projects SET position = ? WHERE id = ? AND user_id = ?`, pos, id, userID); err != nil {
				return err
			}
		}
		return nil
	})
}

// setTags replaces the project's tag set with names, creating missing tags.
// Names are matched case-insensitively (Unicode simple folding) against the
// user's existing tags; a new tag is stored as first typed. The caller must
// have verified that projectID belongs to userID inside the same transaction.
func setTags(ctx context.Context, tx *sql.Tx, userID, projectID string, names []string, now int64) error {
	rows, err := tx.QueryContext(ctx, `SELECT id, name FROM tags WHERE user_id = ?`, userID)
	if err != nil {
		return err
	}
	var existing []Tag
	for rows.Next() {
		var t Tag
		if err := rows.Scan(&t.ID, &t.Name); err != nil {
			rows.Close()
			return err
		}
		existing = append(existing, t)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}

	want := map[string]bool{} // tag ids
	for _, name := range names {
		var id string
		for _, t := range existing {
			if strings.EqualFold(t.Name, name) {
				id = t.ID
				break
			}
		}
		if id == "" {
			id = auth.NewID()
			if _, err := tx.ExecContext(ctx, `INSERT INTO tags (id, user_id, name, created_at) VALUES (?, ?, ?, ?)`,
				id, userID, name, now); err != nil {
				return err
			}
			existing = append(existing, Tag{ID: id, Name: name})
		}
		want[id] = true
	}

	rows, err = tx.QueryContext(ctx, `SELECT pt.tag_id FROM project_tags pt JOIN projects p ON p.id = pt.project_id
		WHERE pt.project_id = ? AND p.user_id = ?`, projectID, userID)
	if err != nil {
		return err
	}
	have := map[string]bool{}
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return err
		}
		have[id] = true
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}
	for id := range have {
		if !want[id] {
			if _, err := tx.ExecContext(ctx, `DELETE FROM project_tags WHERE project_id = ? AND tag_id = ?`, projectID, id); err != nil {
				return err
			}
		}
	}
	for id := range want {
		if !have[id] {
			if _, err := tx.ExecContext(ctx, `INSERT INTO project_tags (project_id, tag_id)
				SELECT p.id, t.id FROM projects p, tags t
				 WHERE p.id = ?1 AND p.user_id = ?2 AND t.id = ?3 AND t.user_id = ?2`,
				projectID, userID, id); err != nil {
				return err
			}
		}
	}
	return nil
}

// ---------------------------------------------------------------- tags

// ListTags returns the user's tags sorted by name, with project counts.
func (s *Store) ListTags(ctx context.Context, userID string) ([]Tag, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT t.id, t.name, COUNT(pt.project_id) FROM tags t
		LEFT JOIN project_tags pt ON pt.tag_id = t.id
		WHERE t.user_id = ? GROUP BY t.id, t.name`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Tag{}
	for rows.Next() {
		var t Tag
		if err := rows.Scan(&t.ID, &t.Name, &t.ProjectCount); err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	sort.SliceStable(out, func(i, j int) bool {
		a, b := strings.ToLower(out[i].Name), strings.ToLower(out[j].Name)
		if a != b {
			return a < b
		}
		return out[i].Name < out[j].Name
	})
	return out, rows.Err()
}

// RenameTag renames one of the user's tags everywhere. It returns
// ErrConflict if another of the user's tags already has that name.
func (s *Store) RenameTag(ctx context.Context, userID, id, name string) (Tag, error) {
	var t Tag
	err := s.tx(ctx, func(tx *sql.Tx) error {
		rows, err := tx.QueryContext(ctx, `SELECT id, name FROM tags WHERE user_id = ?`, userID)
		if err != nil {
			return err
		}
		found := false
		conflict := false
		for rows.Next() {
			var tid, tname string
			if err := rows.Scan(&tid, &tname); err != nil {
				rows.Close()
				return err
			}
			if tid == id {
				found = true
			} else if strings.EqualFold(tname, name) {
				conflict = true
			}
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return err
		}
		if !found {
			return ErrNotFound
		}
		if conflict {
			return ErrConflict
		}
		if err := affected(tx.ExecContext(ctx, `UPDATE tags SET name = ? WHERE id = ? AND user_id = ?`, name, id, userID)); err != nil {
			return err
		}
		t.ID, t.Name = id, name
		return tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM project_tags pt JOIN projects p ON p.id = pt.project_id
			WHERE pt.tag_id = ? AND p.user_id = ?`, id, userID).Scan(&t.ProjectCount)
	})
	return t, err
}

// ---------------------------------------------------------------- log

// CreateLogEntry adds a log entry to one of the user's projects.
func (s *Store) CreateLogEntry(ctx context.Context, userID, projectID, body string) (LogEntry, error) {
	now := s.now().UTC()
	l := LogEntry{ID: auth.NewID(), ProjectID: projectID, Body: body, CreatedAt: now, UpdatedAt: now}
	err := affected(s.db.ExecContext(ctx, `INSERT INTO log_entries (id, project_id, body, created_at, updated_at)
		SELECT ?, p.id, ?, ?, ? FROM projects p WHERE p.id = ? AND p.user_id = ?`,
		l.ID, body, ms(now), ms(now), projectID, userID))
	return l, err
}

// UpdateLogEntry changes the body of one of the user's log entries.
func (s *Store) UpdateLogEntry(ctx context.Context, userID, id, body string) (LogEntry, error) {
	var l LogEntry
	err := s.tx(ctx, func(tx *sql.Tx) error {
		if err := affected(tx.ExecContext(ctx, `UPDATE log_entries SET body = ?, updated_at = ?
			WHERE id = ? AND project_id IN (SELECT id FROM projects WHERE user_id = ?)`,
			body, ms(s.now()), id, userID)); err != nil {
			return err
		}
		var err error
		l, err = scanLog(tx.QueryRowContext(ctx, `SELECT `+logCols+` FROM log_entries l JOIN projects p ON p.id = l.project_id
			WHERE l.id = ? AND p.user_id = ?`, id, userID))
		return err
	})
	return l, err
}

// DeleteLogEntry deletes one of the user's log entries.
func (s *Store) DeleteLogEntry(ctx context.Context, userID, id string) error {
	return affected(s.db.ExecContext(ctx, `DELETE FROM log_entries
		WHERE id = ? AND project_id IN (SELECT id FROM projects WHERE user_id = ?)`, id, userID))
}

// ---------------------------------------------------------------- tasks

// CreateTask appends a task to one of the user's projects.
func (s *Store) CreateTask(ctx context.Context, userID, projectID, title string) (Task, error) {
	var t Task
	err := s.tx(ctx, func(tx *sql.Tx) error {
		now := ms(s.now())
		id := auth.NewID()
		if err := affected(tx.ExecContext(ctx, `INSERT INTO tasks (id, project_id, title, done, position, created_at, updated_at)
			SELECT ?1, p.id, ?2, 0, (SELECT COALESCE(MAX(position), -1) + 1 FROM tasks WHERE project_id = p.id), ?3, ?3
			  FROM projects p WHERE p.id = ?4 AND p.user_id = ?5`,
			id, title, now, projectID, userID)); err != nil {
			return err
		}
		var err error
		t, err = getTask(ctx, tx, userID, id)
		return err
	})
	return t, err
}

func getTask(ctx context.Context, q querier, userID, id string) (Task, error) {
	t, err := scanTask(q.QueryRowContext(ctx, `SELECT `+taskCols+` FROM tasks t JOIN projects p ON p.id = t.project_id
		WHERE t.id = ? AND p.user_id = ?`, id, userID))
	if errors.Is(err, sql.ErrNoRows) {
		return Task{}, ErrNotFound
	}
	return t, err
}

// TaskPatch lists the fields to change; nil means unchanged.
type TaskPatch struct {
	Title *string
	Done  *bool
}

// UpdateTask applies patch to one of the user's tasks.
func (s *Store) UpdateTask(ctx context.Context, userID, id string, patch TaskPatch) (Task, error) {
	var t Task
	err := s.tx(ctx, func(tx *sql.Tx) error {
		now := ms(s.now())
		const owned = ` WHERE id = ? AND project_id IN (SELECT id FROM projects WHERE user_id = ?)`
		if err := affected(tx.ExecContext(ctx, `UPDATE tasks SET updated_at = ?`+owned, now, id, userID)); err != nil {
			return err
		}
		if patch.Title != nil {
			if _, err := tx.ExecContext(ctx, `UPDATE tasks SET title = ?`+owned, *patch.Title, id, userID); err != nil {
				return err
			}
		}
		if patch.Done != nil {
			var err error
			if *patch.Done {
				_, err = tx.ExecContext(ctx, `UPDATE tasks SET done = 1, completed_at = COALESCE(completed_at, ?)`+owned, now, id, userID)
			} else {
				_, err = tx.ExecContext(ctx, `UPDATE tasks SET done = 0, completed_at = NULL`+owned, id, userID)
			}
			if err != nil {
				return err
			}
		}
		var err error
		t, err = getTask(ctx, tx, userID, id)
		return err
	})
	return t, err
}

// DeleteTask deletes one of the user's tasks.
func (s *Store) DeleteTask(ctx context.Context, userID, id string) error {
	return affected(s.db.ExecContext(ctx, `DELETE FROM tasks
		WHERE id = ? AND project_id IN (SELECT id FROM projects WHERE user_id = ?)`, id, userID))
}
