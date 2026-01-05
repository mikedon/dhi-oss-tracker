package main

import (
	"encoding/json"
	"log"
	"net/http"
	"os"

	"dhi-oss-usage/internal/api"
	"dhi-oss-usage/internal/db"
	"dhi-oss-usage/internal/github"
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

	// Setup routes
	mux := http.NewServeMux()
	mux.HandleFunc("/health", healthHandler)

	// Register API routes
	apiHandler.RegisterRoutes(mux)

	log.Printf("Server starting on port %s", port)
	if err := http.ListenAndServe(":"+port, mux); err != nil {
		log.Fatalf("Server failed: %v", err)
	}
}

func healthHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}
