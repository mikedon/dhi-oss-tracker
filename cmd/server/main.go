package main

import (
	"encoding/json"
	"log"
	"net/http"
	"os"
	"strings"
	"time"

	"dhi-oss-usage/internal/api"
	"dhi-oss-usage/internal/db"
	"dhi-oss-usage/internal/github"

	"github.com/robfig/cron/v3"
)

func main() {
	// Get port from env or default to 8000
	port := os.Getenv("PORT")
	if port == "" {
		port = "8000"
	}

	// Get database path from env or default
	dbPath := os.Getenv("DB_PATH")
	if dbPath == "" {
		dbPath = "dhi-oss-usage.db"
	}

	// Get GitHub token
	ghToken := os.Getenv("GITHUB_TOKEN")
	if ghToken == "" {
		log.Println("WARNING: GITHUB_TOKEN not set, refresh will not work")
	}

	// Get refresh schedule (cron syntax, empty = disabled)
	refreshSchedule := os.Getenv("REFRESH_SCHEDULE")
	if refreshSchedule == "" {
		refreshSchedule = "0 3 * * *" // Default: 3 AM daily
	}
	if strings.ToLower(refreshSchedule) == "disabled" {
		refreshSchedule = ""
	}

	// Open database
	database, err := db.Open(dbPath)
	if err != nil {
		log.Fatalf("Failed to open database: %v", err)
	}
	defer database.Close()

	// Run migrations
	if err := database.Migrate(); err != nil {
		log.Fatalf("Failed to run migrations: %v", err)
	}
	log.Println("Database initialized")

	// Create GitHub client
	ghClient := github.NewClient(ghToken)

	// Create API
	apiHandler := api.New(database, ghClient)

	// Setup scheduler
	if refreshSchedule != "" {
		setupScheduler(apiHandler, refreshSchedule)
	} else {
		log.Println("Scheduled refresh disabled")
	}

	// Check if data is stale and trigger immediate refresh if needed
	checkAndRefreshStaleData(apiHandler)

	// Setup routes
	mux := http.NewServeMux()
	mux.HandleFunc("/health", healthHandler)

	// Register API routes
	apiHandler.RegisterRoutes(mux)

	// Serve static files
	staticDir := os.Getenv("STATIC_DIR")
	if staticDir == "" {
		staticDir = "static"
	}
	mux.Handle("/", http.FileServer(http.Dir(staticDir)))

	log.Printf("Server starting on port %s", port)
	if err := http.ListenAndServe(":"+port, mux); err != nil {
		log.Fatalf("Server failed: %v", err)
	}
}

func healthHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}

func setupScheduler(apiHandler *api.API, schedule string) {
	c := cron.New()
	_, err := c.AddFunc(schedule, func() {
		log.Printf("Scheduled refresh triggered (schedule: %s)", schedule)
		apiHandler.TriggerRefresh("scheduled")
	})
	if err != nil {
		log.Printf("ERROR: Failed to setup scheduler with schedule '%s': %v", schedule, err)
		return
	}
	c.Start()
	log.Printf("Scheduler started: refresh at '%s'", schedule)

	// Set function to get next scheduled refresh time
	apiHandler.SetNextRefreshFunc(func() *time.Time {
		entries := c.Entries()
		if len(entries) > 0 {
			next := entries[0].Next
			return &next
		}
		return nil
	})
}

func checkAndRefreshStaleData(apiHandler *api.API) {
	lastRefresh := apiHandler.GetLastRefreshTime()
	if lastRefresh == nil {
		log.Println("No previous refresh found, triggering startup refresh")
		apiHandler.TriggerRefresh("startup")
		return
	}

	staleThreshold := 24 * time.Hour
	age := time.Since(*lastRefresh)
	if age > staleThreshold {
		log.Printf("Data is stale (last refresh: %s, age: %s), triggering startup refresh", lastRefresh.Format(time.RFC3339), age.Round(time.Minute))
		apiHandler.TriggerRefresh("startup")
	} else {
		log.Printf("Data is fresh (last refresh: %s, age: %s)", lastRefresh.Format(time.RFC3339), age.Round(time.Minute))
	}
}
