package api

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"dhi-oss-usage/internal/db"
	"dhi-oss-usage/internal/github"
)

type API struct {
	db           *db.DB
	ghClient     *github.Client
	refreshMu    sync.Mutex
	refreshRunning bool
}

func New(database *db.DB, ghClient *github.Client) *API {
	return &API{
		db:       database,
		ghClient: ghClient,
	}
}

// RegisterRoutes adds API routes to the mux
func (a *API) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("/api/projects", a.handleProjects)
	mux.HandleFunc("/api/projects/new", a.handleNewProjects)
	mux.HandleFunc("/api/stats", a.handleStats)
	mux.HandleFunc("/api/source-types", a.handleSourceTypes)
	mux.HandleFunc("/api/refresh", a.handleRefresh)
	mux.HandleFunc("/api/refresh/status", a.handleRefreshStatus)
	mux.HandleFunc("/api/history", a.handleHistory)
}

// handleProjects returns list of projects with filtering/sorting
func (a *API) handleProjects(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	q := r.URL.Query()

	filter := db.ProjectFilter{
		Search:     q.Get("search"),
		SourceType: q.Get("source_type"),
		SortBy:     q.Get("sort"),
		SortOrder:  q.Get("order"),
	}

	if minStars := q.Get("min_stars"); minStars != "" {
		if v, err := strconv.Atoi(minStars); err == nil {
			filter.MinStars = v
		}
	}
	if maxStars := q.Get("max_stars"); maxStars != "" {
		if v, err := strconv.Atoi(maxStars); err == nil {
			filter.MaxStars = v
		}
	}
	if limit := q.Get("limit"); limit != "" {
		if v, err := strconv.Atoi(limit); err == nil {
			filter.Limit = v
		}
	}
	if offset := q.Get("offset"); offset != "" {
		if v, err := strconv.Atoi(offset); err == nil {
			filter.Offset = v
		}
	}

	projects, err := a.db.ListProjects(filter)
	if err != nil {
		log.Printf("Error listing projects: %v", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(projects)
}

// handleSourceTypes returns list of distinct source types
func (a *API) handleSourceTypes(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	types, err := a.db.GetSourceTypes()
	if err != nil {
		log.Printf("Error getting source types: %v", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(types)
}

// handleStats returns summary statistics
func (a *API) handleStats(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	total, totalStars, popular, notable, err := a.db.GetStats()
	if err != nil {
		log.Printf("Error getting stats: %v", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	// Get count of new projects this week
	weekAgo := time.Now().Add(-7 * 24 * time.Hour)
	newThisWeek, err := a.db.GetNewProjectsCount(weekAgo)
	if err != nil {
		log.Printf("Error getting new projects count: %v", err)
		newThisWeek = 0 // Don't fail the whole request
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]int{
		"total_projects":  total,
		"total_stars":     totalStars,
		"popular_count":   popular,
		"notable_count":   notable,
		"new_this_week":   newThisWeek,
	})
}

// handleRefresh triggers an async refresh
func (a *API) handleRefresh(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Check if refresh is already running
	a.refreshMu.Lock()
	if a.refreshRunning {
		a.refreshMu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"success": false,
			"message": "Refresh already in progress",
		})
		return
	}
	a.refreshRunning = true
	a.refreshMu.Unlock()

	// Create job record
	jobID, err := a.db.CreateRefreshJob()
	if err != nil {
		log.Printf("Error creating refresh job: %v", err)
		a.refreshMu.Lock()
		a.refreshRunning = false
		a.refreshMu.Unlock()
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	// Start async refresh
	go a.runRefresh(jobID, "manual")

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"success": true,
		"job_id":  jobID,
		"message": "Refresh started",
	})
}

func (a *API) runRefresh(jobID int64, source string) {
	defer func() {
		a.refreshMu.Lock()
		a.refreshRunning = false
		a.refreshMu.Unlock()
	}()

	log.Printf("Starting refresh job %d (source: %s)", jobID, source)

	if err := a.db.StartRefreshJob(jobID); err != nil {
		log.Printf("Error starting job: %v", err)
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()

	projects, err := a.ghClient.FetchAllProjects(ctx, nil)
	if err != nil {
		log.Printf("Error fetching projects: %v", err)
		a.db.FailRefreshJob(jobID, err.Error())
		return
	}

	// Upsert all projects
	for _, p := range projects {
		dbProject := &db.Project{
			RepoFullName:    p.RepoFullName,
			GitHubURL:       p.GitHubURL,
			Stars:           p.Stars,
			Description:     p.Description,
			PrimaryLanguage: p.PrimaryLanguage,
			DockerfilePath:  p.DockerfilePath,
			FileURL:         p.FileURL,
			SourceType:      p.SourceType,
		}
		if err := a.db.UpsertProject(dbProject); err != nil {
			log.Printf("Error upserting project %s: %v", p.RepoFullName, err)
		}
	}

	if err := a.db.CompleteRefreshJob(jobID, len(projects)); err != nil {
		log.Printf("Error completing job: %v", err)
	}

	// Fetch adoption dates for projects that don't have them
	a.fetchAdoptionDates(ctx)

	// Record snapshot for historical tracking
	if err := a.db.RecordSnapshot(); err != nil {
		log.Printf("Error recording snapshot: %v", err)
	} else {
		log.Printf("Recorded snapshot after refresh")
	}

	log.Printf("Refresh job %d completed (source: %s): %d projects", jobID, source, len(projects))
}

// fetchAdoptionDates fetches adoption dates for projects that don't have them
func (a *API) fetchAdoptionDates(ctx context.Context) {
	projects, err := a.db.GetProjectsWithoutAdoptionDate()
	if err != nil {
		log.Printf("Error getting projects without adoption date: %v", err)
		return
	}

	if len(projects) == 0 {
		log.Printf("All projects have adoption dates")
		return
	}

	log.Printf("Fetching adoption dates for %d projects...", len(projects))

	for i, p := range projects {
		select {
		case <-ctx.Done():
			log.Printf("Context cancelled, stopping adoption date fetch")
			return
		default:
		}

		log.Printf("Fetching adoption date for %s (%d/%d)", p.RepoFullName, i+1, len(projects))

		adoptedAt, err := a.ghClient.GetFileFirstCommitDate(ctx, p.RepoFullName, p.DockerfilePath)
		if err != nil {
			log.Printf("Error getting adoption date for %s: %v", p.RepoFullName, err)
			// If rate limited, wait and retry
			if strings.Contains(err.Error(), "rate limited") {
				log.Printf("Rate limited, waiting 60s...")
				time.Sleep(60 * time.Second)
				adoptedAt, err = a.ghClient.GetFileFirstCommitDate(ctx, p.RepoFullName, p.DockerfilePath)
				if err != nil {
					log.Printf("Retry failed for %s: %v", p.RepoFullName, err)
					continue
				}
			} else {
				continue
			}
		}

		if err := a.db.UpdateProjectAdoptionDate(p.ID, *adoptedAt); err != nil {
			log.Printf("Error updating adoption date for %s: %v", p.RepoFullName, err)
		} else {
			log.Printf("Set adoption date for %s: %s", p.RepoFullName, adoptedAt.Format("2006-01-02"))
		}

		// Rate limit: commits API is part of the 5000/hr limit
		time.Sleep(500 * time.Millisecond)
	}

	log.Printf("Finished fetching adoption dates")
}

// TriggerRefresh starts a refresh if one isn't already running.
// Returns true if a refresh was started, false if one was already running.
// This is used by the scheduler for automated refreshes.
func (a *API) TriggerRefresh(source string) bool {
	a.refreshMu.Lock()
	if a.refreshRunning {
		a.refreshMu.Unlock()
		log.Printf("Skipping %s refresh: already running", source)
		return false
	}
	a.refreshRunning = true
	a.refreshMu.Unlock()

	jobID, err := a.db.CreateRefreshJob()
	if err != nil {
		log.Printf("Error creating refresh job for %s refresh: %v", source, err)
		a.refreshMu.Lock()
		a.refreshRunning = false
		a.refreshMu.Unlock()
		return false
	}

	go a.runRefresh(jobID, source)
	return true
}

// GetLastRefreshTime returns the completion time of the last successful refresh.
// Returns nil if no successful refresh has occurred.
func (a *API) GetLastRefreshTime() *time.Time {
	job, err := a.db.GetLastCompletedRefreshJob()
	if err != nil || job == nil {
		return nil
	}
	return job.CompletedAt
}

// handleHistory returns historical snapshots
func (a *API) handleHistory(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	limit := 100 // default limit
	if limitStr := r.URL.Query().Get("limit"); limitStr != "" {
		if v, err := strconv.Atoi(limitStr); err == nil && v > 0 {
			limit = v
		}
	}

	snapshots, err := a.db.GetSnapshots(limit)
	if err != nil {
		log.Printf("Error getting snapshots: %v", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"snapshots": snapshots,
	})
}

// handleNewProjects returns projects first seen within a time period
func (a *API) handleNewProjects(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Parse 'since' parameter (e.g., "7d", "30d", "1w")
	sinceStr := r.URL.Query().Get("since")
	if sinceStr == "" {
		sinceStr = "7d" // default to 7 days
	}

	duration, err := parseDuration(sinceStr)
	if err != nil {
		http.Error(w, "Invalid 'since' parameter. Use format like '7d', '1w', '30d'", http.StatusBadRequest)
		return
	}

	since := time.Now().Add(-duration)
	projects, err := a.db.GetNewProjectsSince(since)
	if err != nil {
		log.Printf("Error getting new projects: %v", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(projects)
}

// parseDuration parses a duration string like "7d", "1w", "30d"
func parseDuration(s string) (time.Duration, error) {
	if len(s) < 2 {
		return 0, fmt.Errorf("invalid duration: %s", s)
	}

	unit := s[len(s)-1]
	valueStr := s[:len(s)-1]
	value, err := strconv.Atoi(valueStr)
	if err != nil {
		return 0, fmt.Errorf("invalid duration value: %s", s)
	}

	switch unit {
	case 'd':
		return time.Duration(value) * 24 * time.Hour, nil
	case 'w':
		return time.Duration(value) * 7 * 24 * time.Hour, nil
	case 'h':
		return time.Duration(value) * time.Hour, nil
	default:
		return 0, fmt.Errorf("invalid duration unit: %c (use h, d, or w)", unit)
	}
}

// handleRefreshStatus returns the current refresh status
func (a *API) handleRefreshStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	a.refreshMu.Lock()
	isRunning := a.refreshRunning
	a.refreshMu.Unlock()

	job, err := a.db.GetLatestRefreshJob()
	if err != nil {
		log.Printf("Error getting refresh status: %v", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	response := map[string]interface{}{
		"is_running": isRunning,
	}

	if job != nil {
		response["last_job"] = job
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}
