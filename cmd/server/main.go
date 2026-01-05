package main

import (
	"encoding/json"
	"log"
	"net/http"
	"os"

	"dhi-oss-usage/internal/db"
)

var database *db.DB

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

	// Open database
	var err error
	database, err = db.Open(dbPath)
	if err != nil {
		log.Fatalf("Failed to open database: %v", err)
	}
	defer database.Close()

	// Run migrations
	if err := database.Migrate(); err != nil {
		log.Fatalf("Failed to run migrations: %v", err)
	}
	log.Println("Database initialized")

	// Setup routes
	mux := http.NewServeMux()
	mux.HandleFunc("/health", healthHandler)
	mux.HandleFunc("/api/stats", statsHandler)

	log.Printf("Server starting on port %s", port)
	if err := http.ListenAndServe(":"+port, mux); err != nil {
		log.Fatalf("Server failed: %v", err)
	}
}

func healthHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}

func statsHandler(w http.ResponseWriter, r *http.Request) {
	total, totalStars, popular, notable, err := database.GetStats()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
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
