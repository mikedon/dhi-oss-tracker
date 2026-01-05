//go:build ignore

package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"sort"
	"time"

	"dhi-oss-usage/internal/github"
)

func main() {
	token := os.Getenv("GITHUB_TOKEN")
	if token == "" {
		log.Fatal("GITHUB_TOKEN not set")
	}

	client := github.NewClient(token)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	projects, err := client.FetchAllProjects(ctx, func(status string, current, total int) {
		fmt.Printf("Status: %s %d/%d\n", status, current, total)
	})
	if err != nil {
		log.Fatalf("Error: %v", err)
	}

	// Sort by stars
	sort.Slice(projects, func(i, j int) bool {
		return projects[i].Stars > projects[j].Stars
	})

	fmt.Printf("\n=== Found %d projects ===", len(projects))
	fmt.Println("\n\nTop projects by stars:")
	for i, p := range projects {
		if i >= 20 {
			break
		}
		fmt.Printf("%d. %s - %d stars\n", i+1, p.RepoFullName, p.Stars)
	}
}
