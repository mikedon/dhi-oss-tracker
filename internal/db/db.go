package db

import (
	"database/sql"
	"fmt"
	"time"

	_ "github.com/mattn/go-sqlite3"
)

type DB struct {
	*sql.DB
}

type Project struct {
	ID              int64     `json:"id"`
	RepoFullName    string    `json:"repo_full_name"`
	GitHubURL       string    `json:"github_url"`
	Stars           int       `json:"stars"`
	Description     string    `json:"description"`
	PrimaryLanguage string    `json:"primary_language"`
	DockerfilePath  string    `json:"dockerfile_path"`
	FileURL         string    `json:"file_url"`
	SourceType      string    `json:"source_type"`
	FirstSeenAt     time.Time `json:"first_seen_at"`
	LastSeenAt      time.Time `json:"last_seen_at"`
	CreatedAt       time.Time `json:"created_at"`
	UpdatedAt       time.Time `json:"updated_at"`
}

type RefreshJob struct {
	ID            int64      `json:"id"`
	Status        string     `json:"status"` // pending, running, completed, failed
	StartedAt     *time.Time `json:"started_at"`
	CompletedAt   *time.Time `json:"completed_at"`
	ProjectsFound int        `json:"projects_found"`
	ErrorMessage  string     `json:"error_message"`
	CreatedAt     time.Time  `json:"created_at"`
}

func Open(path string) (*DB, error) {
	db, err := sql.Open("sqlite3", path+"?_journal_mode=WAL&_foreign_keys=on")
	if err != nil {
		return nil, fmt.Errorf("opening database: %w", err)
	}

	if err := db.Ping(); err != nil {
		return nil, fmt.Errorf("pinging database: %w", err)
	}

	return &DB{db}, nil
}

func (db *DB) Migrate() error {
	schema := `
	CREATE TABLE IF NOT EXISTS projects (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		repo_full_name TEXT UNIQUE NOT NULL,
		github_url TEXT NOT NULL,
		stars INTEGER DEFAULT 0,
		description TEXT DEFAULT '',
		primary_language TEXT DEFAULT '',
		dockerfile_path TEXT DEFAULT '',
		file_url TEXT DEFAULT '',
		source_type TEXT DEFAULT '',
		first_seen_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
		last_seen_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
		created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
		updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
	);

	CREATE TABLE IF NOT EXISTS refresh_jobs (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		status TEXT NOT NULL DEFAULT 'pending',
		started_at TIMESTAMP,
		completed_at TIMESTAMP,
		projects_found INTEGER DEFAULT 0,
		error_message TEXT DEFAULT '',
		created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
	);

	CREATE INDEX IF NOT EXISTS idx_projects_stars ON projects(stars DESC);
	CREATE INDEX IF NOT EXISTS idx_projects_repo ON projects(repo_full_name);
	`

	_, err := db.Exec(schema)
	if err != nil {
		return fmt.Errorf("running migrations: %w", err)
	}

	return nil
}

// Project operations

func (db *DB) UpsertProject(p *Project) error {
	query := `
	INSERT INTO projects (repo_full_name, github_url, stars, description, primary_language, dockerfile_path, file_url, source_type, first_seen_at, last_seen_at, updated_at)
	VALUES (?, ?, ?, ?, ?, ?, ?, ?, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP)
	ON CONFLICT(repo_full_name) DO UPDATE SET
		stars = excluded.stars,
		description = excluded.description,
		primary_language = excluded.primary_language,
		dockerfile_path = excluded.dockerfile_path,
		file_url = excluded.file_url,
		source_type = excluded.source_type,
		last_seen_at = CURRENT_TIMESTAMP,
		updated_at = CURRENT_TIMESTAMP
	`
	_, err := db.Exec(query, p.RepoFullName, p.GitHubURL, p.Stars, p.Description, p.PrimaryLanguage, p.DockerfilePath, p.FileURL, p.SourceType)
	return err
}

type ProjectFilter struct {
	MinStars   int
	MaxStars   int
	Search     string
	SourceType string
	SortBy     string // stars, name, first_seen
	SortOrder  string // asc, desc
	Limit      int
	Offset     int
}

func (db *DB) ListProjects(filter ProjectFilter) ([]Project, error) {
	query := `SELECT id, repo_full_name, github_url, stars, description, primary_language, dockerfile_path, file_url, source_type, first_seen_at, last_seen_at, created_at, updated_at FROM projects WHERE 1=1`
	args := []interface{}{}

	if filter.MinStars > 0 {
		query += " AND stars >= ?"
		args = append(args, filter.MinStars)
	}
	if filter.MaxStars > 0 {
		query += " AND stars <= ?"
		args = append(args, filter.MaxStars)
	}
	if filter.Search != "" {
		query += " AND (repo_full_name LIKE ? OR description LIKE ?)"
		searchPattern := "%" + filter.Search + "%"
		args = append(args, searchPattern, searchPattern)
	}
	if filter.SourceType != "" {
		query += " AND source_type = ?"
		args = append(args, filter.SourceType)
	}

	// Sorting
	sortCol := "stars"
	switch filter.SortBy {
	case "name":
		sortCol = "repo_full_name"
	case "first_seen":
		sortCol = "first_seen_at"
	case "stars":
		sortCol = "stars"
	}
	sortOrder := "DESC"
	if filter.SortOrder == "asc" {
		sortOrder = "ASC"
	}
	query += fmt.Sprintf(" ORDER BY %s %s", sortCol, sortOrder)

	if filter.Limit > 0 {
		query += " LIMIT ?"
		args = append(args, filter.Limit)
	}
	if filter.Offset > 0 {
		query += " OFFSET ?"
		args = append(args, filter.Offset)
	}

	rows, err := db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var projects []Project
	for rows.Next() {
		var p Project
		err := rows.Scan(&p.ID, &p.RepoFullName, &p.GitHubURL, &p.Stars, &p.Description, &p.PrimaryLanguage, &p.DockerfilePath, &p.FileURL, &p.SourceType, &p.FirstSeenAt, &p.LastSeenAt, &p.CreatedAt, &p.UpdatedAt)
		if err != nil {
			return nil, err
		}
		projects = append(projects, p)
	}
	return projects, rows.Err()
}

func (db *DB) GetSourceTypes() ([]string, error) {
	rows, err := db.Query(`SELECT DISTINCT source_type FROM projects WHERE source_type != '' ORDER BY source_type`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var types []string
	for rows.Next() {
		var t string
		if err := rows.Scan(&t); err != nil {
			return nil, err
		}
		types = append(types, t)
	}
	return types, rows.Err()
}

func (db *DB) GetStats() (total int, totalStars int, popular int, notable int, err error) {
	err = db.QueryRow(`SELECT COUNT(*), COALESCE(SUM(stars), 0) FROM projects`).Scan(&total, &totalStars)
	if err != nil {
		return
	}
	err = db.QueryRow(`SELECT COUNT(*) FROM projects WHERE stars >= 1000`).Scan(&popular)
	if err != nil {
		return
	}
	err = db.QueryRow(`SELECT COUNT(*) FROM projects WHERE stars >= 100 AND stars < 1000`).Scan(&notable)
	return
}

// Refresh job operations

func (db *DB) CreateRefreshJob() (int64, error) {
	result, err := db.Exec(`INSERT INTO refresh_jobs (status) VALUES ('pending')`)
	if err != nil {
		return 0, err
	}
	return result.LastInsertId()
}

func (db *DB) StartRefreshJob(id int64) error {
	_, err := db.Exec(`UPDATE refresh_jobs SET status = 'running', started_at = CURRENT_TIMESTAMP WHERE id = ?`, id)
	return err
}

func (db *DB) CompleteRefreshJob(id int64, projectsFound int) error {
	_, err := db.Exec(`UPDATE refresh_jobs SET status = 'completed', completed_at = CURRENT_TIMESTAMP, projects_found = ? WHERE id = ?`, projectsFound, id)
	return err
}

func (db *DB) FailRefreshJob(id int64, errMsg string) error {
	_, err := db.Exec(`UPDATE refresh_jobs SET status = 'failed', completed_at = CURRENT_TIMESTAMP, error_message = ? WHERE id = ?`, errMsg, id)
	return err
}

func (db *DB) GetLatestRefreshJob() (*RefreshJob, error) {
	row := db.QueryRow(`SELECT id, status, started_at, completed_at, projects_found, error_message, created_at FROM refresh_jobs ORDER BY id DESC LIMIT 1`)
	var job RefreshJob
	err := row.Scan(&job.ID, &job.Status, &job.StartedAt, &job.CompletedAt, &job.ProjectsFound, &job.ErrorMessage, &job.CreatedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &job, nil
}

func (db *DB) GetRunningRefreshJob() (*RefreshJob, error) {
	row := db.QueryRow(`SELECT id, status, started_at, completed_at, projects_found, error_message, created_at FROM refresh_jobs WHERE status = 'running' ORDER BY id DESC LIMIT 1`)
	var job RefreshJob
	err := row.Scan(&job.ID, &job.Status, &job.StartedAt, &job.CompletedAt, &job.ProjectsFound, &job.ErrorMessage, &job.CreatedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &job, nil
}
