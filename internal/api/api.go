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
	"dhi-oss-usage/internal/notifications"
)

type API struct {
	db               *db.DB
	ghClient         *github.Client
	notificationsSvc *notifications.Service
	refreshMu        sync.Mutex
	refreshRunning   bool
	nextRefreshFn    func() *time.Time // function to get next scheduled refresh time
}

func New(database *db.DB, ghClient *github.Client) *API {
	return &API{
		db:               database,
		ghClient:         ghClient,
		notificationsSvc: notifications.NewService(database),
	}
}

// RegisterRoutes adds API routes to the mux
// SetNextRefreshFunc sets a function that returns the next scheduled refresh time
func (a *API) SetNextRefreshFunc(fn func() *time.Time) {
	a.nextRefreshFn = fn
}

func (a *API) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("/api/projects", a.handleProjects)
	mux.HandleFunc("/api/projects/new", a.handleNewProjects)
	mux.HandleFunc("/api/stats", a.handleStats)
	mux.HandleFunc("/api/source-types", a.handleSourceTypes)
	mux.HandleFunc("/api/refresh", a.handleRefresh)
	mux.HandleFunc("/api/refresh/status", a.handleRefreshStatus)
	mux.HandleFunc("/api/history", a.handleHistory)

	// Notification endpoints
	mux.HandleFunc("/api/notifications", a.handleNotifications)
	mux.HandleFunc("/api/notifications/", a.handleNotificationsSingle) // handles /api/notifications/:id paths
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

	// Get count of new projects this week (current calendar week, Monday-Sunday)
	weekStart := startOfWeek(time.Now())
	newThisWeek, err := a.db.GetNewProjectsCount(weekStart)
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

	// Get new projects from this week to notify about
	weekStart := startOfWeek(time.Now())
	newProjects, err := a.db.GetNewProjectsSince(weekStart)
	if err != nil {
		log.Printf("Error getting new projects for notification: %v", err)
	} else if len(newProjects) > 0 {
		log.Printf("Sending notifications for %d new projects", len(newProjects))
		if err := a.notificationsSvc.NotifyNewProjects(newProjects); err != nil {
			log.Printf("Error sending notifications: %v", err)
		}
	}

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

		log.Printf("Fetching adoption info for %s (%d/%d)", p.RepoFullName, i+1, len(projects))

		adoptionInfo, err := a.ghClient.GetFileFirstCommit(ctx, p.RepoFullName, p.DockerfilePath)
		if err != nil {
			log.Printf("Error getting adoption info for %s: %v", p.RepoFullName, err)
			// If rate limited, wait and retry
			if strings.Contains(err.Error(), "rate limited") {
				log.Printf("Rate limited, waiting 60s...")
				time.Sleep(60 * time.Second)
				adoptionInfo, err = a.ghClient.GetFileFirstCommit(ctx, p.RepoFullName, p.DockerfilePath)
				if err != nil {
					log.Printf("Retry failed for %s: %v", p.RepoFullName, err)
					continue
				}
			} else {
				continue
			}
		}

		if err := a.db.UpdateProjectAdoption(p.ID, adoptionInfo.Date, adoptionInfo.CommitURL); err != nil {
			log.Printf("Error updating adoption info for %s: %v", p.RepoFullName, err)
		} else {
			log.Printf("Set adoption for %s: %s (%s)", p.RepoFullName, adoptionInfo.Date.Format("2006-01-02"), adoptionInfo.CommitURL)
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

// handleHistory returns adoption history by date
func (a *API) handleHistory(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	days := 14 // default to 2 weeks
	if daysStr := r.URL.Query().Get("days"); daysStr != "" {
		if v, err := strconv.Atoi(daysStr); err == nil && v > 0 {
			days = v
		}
	}

	adoptions, err := a.db.GetAdoptionByDate(days)
	if err != nil {
		log.Printf("Error getting adoption history: %v", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"adoptions": adoptions,
	})
}

// handleNewProjects returns projects adopted within a time period
func (a *API) handleNewProjects(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Parse 'since' parameter (e.g., "7d", "30d", "1w", "thisweek")
	sinceStr := r.URL.Query().Get("since")
	if sinceStr == "" {
		sinceStr = "thisweek" // default to current calendar week
	}

	var since time.Time
	if sinceStr == "thisweek" {
		since = startOfWeek(time.Now())
	} else {
		duration, err := parseDuration(sinceStr)
		if err != nil {
			http.Error(w, "Invalid 'since' parameter. Use 'thisweek', '7d', '1w', '30d'", http.StatusBadRequest)
			return
		}
		since = time.Now().Add(-duration)
	}
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
// startOfWeek returns the start of the current week (Monday 00:00:00 UTC)
func startOfWeek(t time.Time) time.Time {
	t = t.UTC()
	weekday := int(t.Weekday())
	if weekday == 0 {
		weekday = 7 // Sunday is 7, not 0
	}
	// Go back to Monday
	monday := t.AddDate(0, 0, -(weekday - 1))
	// Return start of that day
	return time.Date(monday.Year(), monday.Month(), monday.Day(), 0, 0, 0, 0, time.UTC)
}

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

	// Add next scheduled refresh time if available
	if a.nextRefreshFn != nil {
		if nextTime := a.nextRefreshFn(); nextTime != nil {
			response["next_refresh"] = nextTime
		}
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

// Notification handlers

// handleNotifications handles listing all configs (GET) or creating a new one (POST)
func (a *API) handleNotifications(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		a.listNotifications(w, r)
	case http.MethodPost:
		a.createNotification(w, r)
	default:
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}

// handleNotificationsSingle handles operations on a single notification config
func (a *API) handleNotificationsSingle(w http.ResponseWriter, r *http.Request) {
	// Extract ID from path
	path := strings.TrimPrefix(r.URL.Path, "/api/notifications/")
	parts := strings.Split(path, "/")
	if len(parts) == 0 || parts[0] == "" {
		http.Error(w, "Notification ID required", http.StatusBadRequest)
		return
	}

	id, err := strconv.ParseInt(parts[0], 10, 64)
	if err != nil {
		http.Error(w, "Invalid notification ID", http.StatusBadRequest)
		return
	}

	// Check if this is a sub-action like /test or /logs
	if len(parts) > 1 {
		action := parts[1]
		switch action {
		case "test":
			a.testNotification(w, r, id)
			return
		case "logs":
			a.getNotificationLogs(w, r, id)
			return
		default:
			http.Error(w, "Unknown action", http.StatusNotFound)
			return
		}
	}

	// Handle single resource operations
	switch r.Method {
	case http.MethodGet:
		a.getNotification(w, r, id)
	case http.MethodPut:
		a.updateNotification(w, r, id)
	case http.MethodDelete:
		a.deleteNotification(w, r, id)
	default:
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}

func (a *API) listNotifications(w http.ResponseWriter, r *http.Request) {
	configs, err := a.db.ListNotificationConfigs()
	if err != nil {
		log.Printf("Error listing notification configs: %v", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(configs)
}

func (a *API) createNotification(w http.ResponseWriter, r *http.Request) {
	var config db.NotificationConfig
	if err := json.NewDecoder(r.Body).Decode(&config); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	// Validate required fields
	if config.Name == "" || config.Type == "" || config.ConfigJSON == "" {
		http.Error(w, "name, type, and config_json are required", http.StatusBadRequest)
		return
	}

	// Validate type
	if config.Type != "slack" && config.Type != "email" {
		http.Error(w, "type must be 'slack' or 'email'", http.StatusBadRequest)
		return
	}

	// Validate config by trying to create a provider
	if config.Type == "slack" {
		var slackConfig notifications.SlackConfig
		if err := json.Unmarshal([]byte(config.ConfigJSON), &slackConfig); err != nil {
			http.Error(w, fmt.Sprintf("Invalid slack config: %v", err), http.StatusBadRequest)
			return
		}
		if slackConfig.WebhookURL == "" {
			http.Error(w, "webhook_url is required for Slack notifications", http.StatusBadRequest)
			return
		}
	} else if config.Type == "email" {
		var emailConfig notifications.EmailConfig
		if err := json.Unmarshal([]byte(config.ConfigJSON), &emailConfig); err != nil {
			http.Error(w, fmt.Sprintf("Invalid email config: %v", err), http.StatusBadRequest)
			return
		}
		if emailConfig.To == "" || emailConfig.SMTPHost == "" || emailConfig.SMTPPort == 0 || emailConfig.From == "" {
			http.Error(w, "to, smtp_host, smtp_port, and from are required for email notifications", http.StatusBadRequest)
			return
		}
	}

	id, err := a.db.CreateNotificationConfig(&config)
	if err != nil {
		log.Printf("Error creating notification config: %v", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	config.ID = id
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(config)
}

func (a *API) getNotification(w http.ResponseWriter, r *http.Request, id int64) {
	config, err := a.db.GetNotificationConfig(id)
	if err != nil {
		log.Printf("Error getting notification config: %v", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}
	if config == nil {
		http.Error(w, "Notification config not found", http.StatusNotFound)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(config)
}

func (a *API) updateNotification(w http.ResponseWriter, r *http.Request, id int64) {
	var config db.NotificationConfig
	if err := json.NewDecoder(r.Body).Decode(&config); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	config.ID = id

	// Validate required fields
	if config.Name == "" || config.Type == "" || config.ConfigJSON == "" {
		http.Error(w, "name, type, and config_json are required", http.StatusBadRequest)
		return
	}

	// Validate type
	if config.Type != "slack" && config.Type != "email" {
		http.Error(w, "type must be 'slack' or 'email'", http.StatusBadRequest)
		return
	}

	if err := a.db.UpdateNotificationConfig(&config); err != nil {
		log.Printf("Error updating notification config: %v", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(config)
}

func (a *API) deleteNotification(w http.ResponseWriter, r *http.Request, id int64) {
	if err := a.db.DeleteNotificationConfig(id); err != nil {
		log.Printf("Error deleting notification config: %v", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

func (a *API) testNotification(w http.ResponseWriter, r *http.Request, id int64) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	if err := a.notificationsSvc.SendTestNotification(id); err != nil {
		log.Printf("Error sending test notification: %v", err)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"success": false,
			"error":   err.Error(),
		})
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"success": true,
		"message": "Test notification sent",
	})
}

func (a *API) getNotificationLogs(w http.ResponseWriter, r *http.Request, id int64) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	limit := 50 // default
	if limitStr := r.URL.Query().Get("limit"); limitStr != "" {
		if v, err := strconv.Atoi(limitStr); err == nil && v > 0 {
			limit = v
		}
	}

	logs, err := a.db.GetNotificationLogs(id, limit)
	if err != nil {
		log.Printf("Error getting notification logs: %v", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(logs)
}
