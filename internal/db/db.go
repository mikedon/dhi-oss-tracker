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
	ID              int64      `json:"id"`
	RepoFullName    string     `json:"repo_full_name"`
	GitHubURL       string     `json:"github_url"`
	Stars           int        `json:"stars"`
	Description     string     `json:"description"`
	PrimaryLanguage string     `json:"primary_language"`
	DockerfilePath  string     `json:"dockerfile_path"`
	FileURL         string     `json:"file_url"`
	SourceType      string     `json:"source_type"`
	AdoptedAt       *time.Time `json:"adopted_at"`
	AdoptionCommit  string     `json:"adoption_commit"`
	FirstSeenAt     time.Time  `json:"first_seen_at"`
	LastSeenAt      time.Time  `json:"last_seen_at"`
	CreatedAt       time.Time  `json:"created_at"`
	UpdatedAt       time.Time  `json:"updated_at"`
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

type RefreshSnapshot struct {
	ID            int64     `json:"id"`
	RecordedAt    time.Time `json:"recorded_at"`
	TotalProjects int       `json:"total_projects"`
	TotalStars    int       `json:"total_stars"`
	PopularCount  int       `json:"popular_count"`
	NotableCount  int       `json:"notable_count"`
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
		adopted_at TIMESTAMP,
		adoption_commit TEXT DEFAULT '',
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

	CREATE TABLE IF NOT EXISTS refresh_snapshots (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		recorded_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
		total_projects INTEGER NOT NULL,
		total_stars INTEGER NOT NULL,
		popular_count INTEGER NOT NULL,
		notable_count INTEGER NOT NULL
	);

	CREATE INDEX IF NOT EXISTS idx_projects_stars ON projects(stars DESC);
	CREATE INDEX IF NOT EXISTS idx_projects_repo ON projects(repo_full_name);
	CREATE INDEX IF NOT EXISTS idx_projects_first_seen ON projects(first_seen_at DESC);
	CREATE INDEX IF NOT EXISTS idx_projects_adopted ON projects(adopted_at DESC);
	CREATE INDEX IF NOT EXISTS idx_snapshots_recorded ON refresh_snapshots(recorded_at DESC);


	`

	_, err := db.Exec(schema)
	if err != nil {
		return fmt.Errorf("running migrations: %w", err)
	}

	// Migration: add adopted_at column if it doesn't exist (ignore error if already exists)
	db.Exec("ALTER TABLE projects ADD COLUMN adopted_at TIMESTAMP")
	db.Exec("ALTER TABLE projects ADD COLUMN adoption_commit TEXT DEFAULT ''")


	return nil
}

// Project operations

func (db *DB) UpsertProject(p *Project) error {
	query := `
	INSERT INTO projects (repo_full_name, github_url, stars, description, primary_language, dockerfile_path, file_url, source_type, adopted_at, first_seen_at, last_seen_at, updated_at)
	VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP)
	ON CONFLICT(repo_full_name) DO UPDATE SET
		stars = excluded.stars,
		description = excluded.description,
		primary_language = excluded.primary_language,
		dockerfile_path = excluded.dockerfile_path,
		file_url = excluded.file_url,
		source_type = excluded.source_type,
		adopted_at = COALESCE(projects.adopted_at, excluded.adopted_at),
		last_seen_at = CURRENT_TIMESTAMP,
		updated_at = CURRENT_TIMESTAMP
	`
	_, err := db.Exec(query, p.RepoFullName, p.GitHubURL, p.Stars, p.Description, p.PrimaryLanguage, p.DockerfilePath, p.FileURL, p.SourceType, p.AdoptedAt)
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
	query := `SELECT id, repo_full_name, github_url, stars, description, primary_language, dockerfile_path, file_url, source_type, adopted_at, adoption_commit, first_seen_at, last_seen_at, created_at, updated_at FROM projects WHERE 1=1`
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
		err := rows.Scan(&p.ID, &p.RepoFullName, &p.GitHubURL, &p.Stars, &p.Description, &p.PrimaryLanguage, &p.DockerfilePath, &p.FileURL, &p.SourceType, &p.AdoptedAt, &p.AdoptionCommit, &p.FirstSeenAt, &p.LastSeenAt, &p.CreatedAt, &p.UpdatedAt)
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

func (db *DB) GetLastCompletedRefreshJob() (*RefreshJob, error) {
	row := db.QueryRow(`SELECT id, status, started_at, completed_at, projects_found, error_message, created_at FROM refresh_jobs WHERE status = 'completed' ORDER BY completed_at DESC LIMIT 1`)
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

// Snapshot operations

// RecordSnapshot saves current stats as a snapshot
func (db *DB) RecordSnapshot() error {
	total, totalStars, popular, notable, err := db.GetStats()
	if err != nil {
		return fmt.Errorf("getting stats for snapshot: %w", err)
	}

	_, err = db.Exec(`INSERT INTO refresh_snapshots (total_projects, total_stars, popular_count, notable_count) VALUES (?, ?, ?, ?)`,
		total, totalStars, popular, notable)
	return err
}

// AdoptionByDate represents adoption count for a specific date
type AdoptionByDate struct {
	Date           string `json:"date"`
	Count          int    `json:"count"`
	CumulativeCount int   `json:"cumulative_count"`
	CumulativeStars int   `json:"cumulative_stars"`
}

// GetAdoptionByDate returns daily adoption counts with cumulative totals
func (db *DB) GetAdoptionByDate(days int) ([]AdoptionByDate, error) {
	query := `
		WITH daily_adoptions AS (
			SELECT 
				date(adopted_at) as date,
				COUNT(*) as count,
				SUM(stars) as stars
			FROM projects 
			WHERE adopted_at IS NOT NULL 
				AND adopted_at >= date('now', ?)
			GROUP BY date(adopted_at)
			ORDER BY date(adopted_at)
		)
		SELECT 
			date,
			count,
			(SELECT COUNT(*) FROM projects WHERE adopted_at IS NOT NULL AND date(adopted_at) <= daily_adoptions.date) as cumulative_count,
			(SELECT COALESCE(SUM(stars), 0) FROM projects WHERE adopted_at IS NOT NULL AND date(adopted_at) <= daily_adoptions.date) as cumulative_stars
		FROM daily_adoptions
	`
	
	sinceArg := fmt.Sprintf("-%d days", days)
	rows, err := db.Query(query, sinceArg)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var results []AdoptionByDate
	for rows.Next() {
		var r AdoptionByDate
		err := rows.Scan(&r.Date, &r.Count, &r.CumulativeCount, &r.CumulativeStars)
		if err != nil {
			return nil, err
		}
		results = append(results, r)
	}
	return results, rows.Err()
}

// GetSnapshots returns historical snapshots, most recent first
func (db *DB) GetSnapshots(limit int) ([]RefreshSnapshot, error) {
	query := `SELECT id, recorded_at, total_projects, total_stars, popular_count, notable_count FROM refresh_snapshots ORDER BY recorded_at DESC`
	if limit > 0 {
		query += fmt.Sprintf(" LIMIT %d", limit)
	}

	rows, err := db.Query(query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var snapshots []RefreshSnapshot
	for rows.Next() {
		var s RefreshSnapshot
		err := rows.Scan(&s.ID, &s.RecordedAt, &s.TotalProjects, &s.TotalStars, &s.PopularCount, &s.NotableCount)
		if err != nil {
			return nil, err
		}
		snapshots = append(snapshots, s)
	}
	return snapshots, rows.Err()
}

// GetNewProjectsSince returns projects adopted after the given time
func (db *DB) GetNewProjectsSince(since time.Time) ([]Project, error) {
	query := `SELECT id, repo_full_name, github_url, stars, description, primary_language, dockerfile_path, file_url, source_type, adopted_at, adoption_commit, first_seen_at, last_seen_at, created_at, updated_at 
		FROM projects WHERE adopted_at IS NOT NULL AND adopted_at > ? ORDER BY adopted_at DESC`

	rows, err := db.Query(query, since)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var projects []Project
	for rows.Next() {
		var p Project
		err := rows.Scan(&p.ID, &p.RepoFullName, &p.GitHubURL, &p.Stars, &p.Description, &p.PrimaryLanguage, &p.DockerfilePath, &p.FileURL, &p.SourceType, &p.AdoptedAt, &p.AdoptionCommit, &p.FirstSeenAt, &p.LastSeenAt, &p.CreatedAt, &p.UpdatedAt)
		if err != nil {
			return nil, err
		}
		projects = append(projects, p)
	}
	return projects, rows.Err()
}

// GetNewProjectsCount returns count of projects adopted after the given time
func (db *DB) GetNewProjectsCount(since time.Time) (int, error) {
	var count int
	err := db.QueryRow(`SELECT COUNT(*) FROM projects WHERE adopted_at IS NOT NULL AND adopted_at > ?`, since).Scan(&count)
	return count, err
}

// GetProjectsWithoutAdoptionDate returns projects that need adoption date fetched
func (db *DB) GetProjectsWithoutAdoptionDate() ([]Project, error) {
	query := `SELECT id, repo_full_name, github_url, stars, description, primary_language, dockerfile_path, file_url, source_type, adopted_at, adoption_commit, first_seen_at, last_seen_at, created_at, updated_at 
		FROM projects WHERE adopted_at IS NULL`

	rows, err := db.Query(query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var projects []Project
	for rows.Next() {
		var p Project
		err := rows.Scan(&p.ID, &p.RepoFullName, &p.GitHubURL, &p.Stars, &p.Description, &p.PrimaryLanguage, &p.DockerfilePath, &p.FileURL, &p.SourceType, &p.AdoptedAt, &p.AdoptionCommit, &p.FirstSeenAt, &p.LastSeenAt, &p.CreatedAt, &p.UpdatedAt)
		if err != nil {
			return nil, err
		}
		projects = append(projects, p)
	}
	return projects, rows.Err()
}

// UpdateProjectAdoption sets the adoption date and commit URL for a project
func (db *DB) UpdateProjectAdoption(id int64, adoptedAt time.Time, commitURL string) error {
	_, err := db.Exec(`UPDATE projects SET adopted_at = ?, adoption_commit = ?, updated_at = CURRENT_TIMESTAMP WHERE id = ?`, adoptedAt, commitURL, id)
	return err
}
