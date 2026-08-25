package store

import (
	"database/sql"
	"strings"
	"time"

	"github.com/sharathk-dev/canopy/internal/protocol"
	_ "modernc.org/sqlite"
)

// Store wraps a SQLite database for canopy persistence.
type Store struct {
	db *sql.DB
}

const schema = `
CREATE TABLE IF NOT EXISTS projects (
	id        TEXT PRIMARY KEY,
	repo_path TEXT NOT NULL,
	name      TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS worktrees (
	id         TEXT PRIMARY KEY,
	project_id TEXT NOT NULL,
	repo_path  TEXT NOT NULL,
	path       TEXT NOT NULL,
	branch     TEXT NOT NULL,
	is_main    INTEGER NOT NULL DEFAULT 0,
	missing    INTEGER NOT NULL DEFAULT 0
);

CREATE TABLE IF NOT EXISTS sessions (
	id              TEXT PRIMARY KEY,
	worktree_id     TEXT NOT NULL DEFAULT '',
	kind            TEXT NOT NULL,
	tool            TEXT NOT NULL DEFAULT '',
	cwd             TEXT NOT NULL,
	cli_session_id  TEXT NOT NULL DEFAULT '',
	title           TEXT NOT NULL DEFAULT '',
	title_locked    INTEGER NOT NULL DEFAULT 0,
	state           TEXT NOT NULL,
	archived        INTEGER NOT NULL DEFAULT 0,
	started_at      DATETIME NOT NULL
);
`

// Open opens (or creates) the SQLite database at path and applies the schema.
func Open(path string) (*Store, error) {
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1) // SQLite WAL is fine with one writer
	if _, err := db.Exec(schema); err != nil {
		db.Close()
		return nil, err
	}
	return &Store{db: db}, nil
}

// Close closes the underlying database.
func (s *Store) Close() error { return s.db.Close() }

// --- Projects ---

func (s *Store) UpsertProject(p protocol.Project) error {
	_, err := s.db.Exec(
		`INSERT INTO projects(id,repo_path,name) VALUES(?,?,?)
		 ON CONFLICT(id) DO UPDATE SET repo_path=excluded.repo_path, name=excluded.name`,
		p.ID, p.RepoPath, p.Name,
	)
	return err
}

func (s *Store) GetProject(id string) (protocol.Project, error) {
	row := s.db.QueryRow(`SELECT id,repo_path,name FROM projects WHERE id=?`, id)
	var p protocol.Project
	err := row.Scan(&p.ID, &p.RepoPath, &p.Name)
	return p, err
}

func (s *Store) ListProjects() ([]protocol.Project, error) {
	rows, err := s.db.Query(`SELECT id,repo_path,name FROM projects ORDER BY name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []protocol.Project
	for rows.Next() {
		var p protocol.Project
		if err := rows.Scan(&p.ID, &p.RepoPath, &p.Name); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// --- Worktrees ---

func (s *Store) UpsertWorktree(w protocol.Worktree) error {
	_, err := s.db.Exec(
		`INSERT INTO worktrees(id,project_id,repo_path,path,branch,is_main,missing)
		 VALUES(?,?,?,?,?,?,0)
		 ON CONFLICT(id) DO UPDATE SET
		   repo_path=excluded.repo_path,
		   path=excluded.path,
		   branch=excluded.branch,
		   is_main=excluded.is_main,
		   missing=0`,
		w.ID, "", w.RepoPath, w.Path, w.Branch, boolInt(w.IsMain),
	)
	return err
}

func (s *Store) GetWorktree(id string) (protocol.Worktree, error) {
	row := s.db.QueryRow(
		`SELECT id,repo_path,path,branch,is_main FROM worktrees WHERE id=?`, id)
	var w protocol.Worktree
	var isMain int
	err := row.Scan(&w.ID, &w.RepoPath, &w.Path, &w.Branch, &isMain)
	w.IsMain = isMain != 0
	return w, err
}

func (s *Store) ListWorktreesByRepo(repoPath string) ([]protocol.Worktree, error) {
	rows, err := s.db.Query(
		`SELECT id,repo_path,path,branch,is_main FROM worktrees WHERE repo_path=? ORDER BY path`,
		repoPath,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []protocol.Worktree
	for rows.Next() {
		var w protocol.Worktree
		var isMain int
		if err := rows.Scan(&w.ID, &w.RepoPath, &w.Path, &w.Branch, &isMain); err != nil {
			return nil, err
		}
		w.IsMain = isMain != 0
		out = append(out, w)
	}
	return out, rows.Err()
}

func (s *Store) MarkWorktreeMissing(id string, missing bool) error {
	_, err := s.db.Exec(`UPDATE worktrees SET missing=? WHERE id=?`, boolInt(missing), id)
	return err
}

func (s *Store) DeleteWorktree(id string) error {
	_, err := s.db.Exec(`DELETE FROM worktrees WHERE id=?`, id)
	return err
}

func (s *Store) GetWorktreeByPath(path string) (protocol.Worktree, error) {
	row := s.db.QueryRow(
		`SELECT id,repo_path,path,branch,is_main FROM worktrees WHERE path=?`, path)
	var w protocol.Worktree
	var isMain int
	err := row.Scan(&w.ID, &w.RepoPath, &w.Path, &w.Branch, &isMain)
	w.IsMain = isMain != 0
	return w, err
}

// GetWorktreeByPathPrefix finds the worktree whose path is the longest prefix of dir.
func (s *Store) GetWorktreeByPathPrefix(dir string) (protocol.Worktree, error) {
	rows, err := s.db.Query(
		`SELECT id,repo_path,path,branch,is_main FROM worktrees ORDER BY length(path) DESC`)
	if err != nil {
		return protocol.Worktree{}, err
	}
	defer rows.Close()
	for rows.Next() {
		var w protocol.Worktree
		var isMain int
		if err := rows.Scan(&w.ID, &w.RepoPath, &w.Path, &w.Branch, &isMain); err != nil {
			return protocol.Worktree{}, err
		}
		w.IsMain = isMain != 0
		if strings.HasPrefix(dir, w.Path) {
			return w, nil
		}
	}
	return protocol.Worktree{}, sql.ErrNoRows
}

// --- Sessions ---

func (s *Store) CreateSession(sess protocol.Session) error {
	_, err := s.db.Exec(
		`INSERT INTO sessions(id,worktree_id,kind,tool,cwd,cli_session_id,title,title_locked,state,archived,started_at)
		 VALUES(?,?,?,?,?,?,?,?,?,?,?)`,
		sess.ID, sess.WorktreeID, sess.Kind, sess.Tool, sess.CWD,
		sess.CLISessionID, sess.Title, boolInt(sess.TitleLocked),
		sess.State, boolInt(sess.Archived), sess.StartedAt.UTC(),
	)
	return err
}

func (s *Store) UpdateSession(sess protocol.Session) error {
	_, err := s.db.Exec(
		`UPDATE sessions SET worktree_id=?,kind=?,tool=?,cwd=?,cli_session_id=?,title=?,
		 title_locked=?,state=?,archived=? WHERE id=?`,
		sess.WorktreeID, sess.Kind, sess.Tool, sess.CWD,
		sess.CLISessionID, sess.Title, boolInt(sess.TitleLocked),
		sess.State, boolInt(sess.Archived), sess.ID,
	)
	return err
}

func (s *Store) GetSession(id string) (protocol.Session, error) {
	row := s.db.QueryRow(
		`SELECT id,worktree_id,kind,tool,cwd,cli_session_id,title,title_locked,state,archived,started_at
		 FROM sessions WHERE id=?`, id)
	return scanSession(row)
}

func (s *Store) ListSessions() ([]protocol.Session, error) {
	rows, err := s.db.Query(
		`SELECT id,worktree_id,kind,tool,cwd,cli_session_id,title,title_locked,state,archived,started_at
		 FROM sessions ORDER BY started_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanSessions(rows)
}

func (s *Store) ListActiveSessions() ([]protocol.Session, error) {
	rows, err := s.db.Query(
		`SELECT id,worktree_id,kind,tool,cwd,cli_session_id,title,title_locked,state,archived,started_at
		 FROM sessions WHERE archived=0 AND state NOT IN ('finished','terminated')
		 ORDER BY started_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanSessions(rows)
}

// --- helpers ---

type scanner interface {
	Scan(dest ...any) error
}

func scanSession(row scanner) (protocol.Session, error) {
	var sess protocol.Session
	var titleLocked, archived int
	var startedAt string
	err := row.Scan(
		&sess.ID, &sess.WorktreeID, &sess.Kind, &sess.Tool, &sess.CWD,
		&sess.CLISessionID, &sess.Title, &titleLocked, &sess.State, &archived, &startedAt,
	)
	if err != nil {
		return sess, err
	}
	sess.TitleLocked = titleLocked != 0
	sess.Archived = archived != 0
	if t, err := time.Parse(time.RFC3339, startedAt); err == nil {
		sess.StartedAt = t
	}
	return sess, nil
}

func scanSessions(rows *sql.Rows) ([]protocol.Session, error) {
	var out []protocol.Session
	for rows.Next() {
		sess, err := scanSession(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, sess)
	}
	return out, rows.Err()
}

func boolInt(b bool) int {
	if b {
		return 1
	}
	return 0
}
