package api

import (
	"context"
	"encoding/json"
	"log"
	"net/http"
	"strconv"
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
	mux.HandleFunc("/api/stats", a.handleStats)
	mux.HandleFunc("/api/source-types", a.handleSourceTypes)
	mux.HandleFunc("/api/refresh", a.handleRefresh)
	mux.HandleFunc("/api/refresh/status", a.handleRefreshStatus)
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

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]int{
		"total_projects": total,
		"total_stars":    totalStars,
		"popular_count":  popular,
		"notable_count":  notable,
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
	go a.runRefresh(jobID)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"success": true,
		"job_id":  jobID,
		"message": "Refresh started",
	})
}

func (a *API) runRefresh(jobID int64) {
	defer func() {
		a.refreshMu.Lock()
		a.refreshRunning = false
		a.refreshMu.Unlock()
	}()

	log.Printf("Starting refresh job %d", jobID)

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

	log.Printf("Refresh job %d completed: %d projects", jobID, len(projects))
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
