package github

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const (
	baseURL         = "https://api.github.com"
	searchRateDelay = 6 * time.Second // GitHub code search: ~10 req/min
)

type Client struct {
	token      string
	httpClient *http.Client
}

func NewClient(token string) *Client {
	return &Client{
		token: token,
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
		},
	}
}

// CodeSearchResult represents a single code search hit
type CodeSearchResult struct {
	Path       string `json:"path"`
	Repository struct {
		FullName string `json:"full_name"`
		HTMLURL  string `json:"html_url"`
	} `json:"repository"`
}

// CodeSearchResponse represents GitHub's code search API response
type CodeSearchResponse struct {
	TotalCount        int                `json:"total_count"`
	IncompleteResults bool               `json:"incomplete_results"`
	Items             []CodeSearchResult `json:"items"`
}

// RepoDetails represents repository metadata
type RepoDetails struct {
	FullName        string `json:"full_name"`
	HTMLURL         string `json:"html_url"`
	Description     string `json:"description"`
	StargazersCount int    `json:"stargazers_count"`
	Language        string `json:"language"`
}

// Project combines search result with repo details
type Project struct {
	RepoFullName    string
	GitHubURL       string
	Stars           int
	Description     string
	PrimaryLanguage string
	DockerfilePath  string
}

func (c *Client) doRequest(ctx context.Context, method, endpoint string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, method, baseURL+endpoint, nil)
	if err != nil {
		return nil, err
	}

	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	if resp.StatusCode == 403 {
		// Rate limited - check headers
		return nil, fmt.Errorf("rate limited: %s", string(body))
	}

	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("API error %d: %s", resp.StatusCode, string(body))
	}

	return body, nil
}

// SearchDHIDockerfiles searches for Dockerfiles containing dhi.io
// Returns unique repos found with their dockerfile paths
func (c *Client) SearchDHIDockerfiles(ctx context.Context, progressFn func(found int, page int)) (map[string]string, error) {
	repos := make(map[string]string) // repo full name -> dockerfile path
	page := 1
	perPage := 100

	for {
		select {
		case <-ctx.Done():
			return repos, ctx.Err()
		default:
		}

		query := url.QueryEscape(`"dhi.io" language:Dockerfile`)
		endpoint := fmt.Sprintf("/search/code?q=%s&per_page=%d&page=%d", query, perPage, page)

		log.Printf("Searching page %d...", page)
		body, err := c.doRequest(ctx, "GET", endpoint)
		if err != nil {
			// If rate limited, wait and retry
			if strings.Contains(err.Error(), "rate limited") {
				log.Printf("Rate limited, waiting 60s...")
				time.Sleep(60 * time.Second)
				continue
			}
			return repos, err
		}

		var searchResp CodeSearchResponse
		if err := json.Unmarshal(body, &searchResp); err != nil {
			return repos, err
		}

		for _, item := range searchResp.Items {
			if _, exists := repos[item.Repository.FullName]; !exists {
				repos[item.Repository.FullName] = item.Path
			}
		}

		if progressFn != nil {
			progressFn(len(repos), page)
		}

		log.Printf("Page %d: found %d items, total unique repos: %d", page, len(searchResp.Items), len(repos))

		// Check if we've got all results
		if len(searchResp.Items) < perPage || page*perPage >= searchResp.TotalCount {
			break
		}

		// GitHub only returns first 1000 results
		if page >= 10 {
			log.Printf("Reached GitHub's 1000 result limit")
			break
		}

		page++
		// Rate limit delay for code search
		time.Sleep(searchRateDelay)
	}

	return repos, nil
}

// GetRepoDetails fetches details for a single repository
func (c *Client) GetRepoDetails(ctx context.Context, repoFullName string) (*RepoDetails, error) {
	endpoint := "/repos/" + repoFullName
	body, err := c.doRequest(ctx, "GET", endpoint)
	if err != nil {
		return nil, err
	}

	var repo RepoDetails
	if err := json.Unmarshal(body, &repo); err != nil {
		return nil, err
	}

	return &repo, nil
}

// FetchAllProjects searches for DHI usage and fetches details for each repo
func (c *Client) FetchAllProjects(ctx context.Context, progressFn func(status string, current, total int)) ([]Project, error) {
	// Step 1: Search for all repos
	if progressFn != nil {
		progressFn("searching", 0, 0)
	}

	repos, err := c.SearchDHIDockerfiles(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("searching dockerfiles: %w", err)
	}

	log.Printf("Found %d unique repositories", len(repos))

	// Step 2: Fetch details for each repo
	projects := make([]Project, 0, len(repos))
	i := 0
	for repoName, dockerfilePath := range repos {
		select {
		case <-ctx.Done():
			return projects, ctx.Err()
		default:
		}

		i++
		if progressFn != nil {
			progressFn("fetching_details", i, len(repos))
		}

		log.Printf("Fetching details for %s (%d/%d)", repoName, i, len(repos))

		details, err := c.GetRepoDetails(ctx, repoName)
		if err != nil {
			// Log error but continue with other repos
			log.Printf("Error fetching %s: %v", repoName, err)
			// If rate limited, wait
			if strings.Contains(err.Error(), "rate limited") {
				log.Printf("Rate limited, waiting 60s...")
				time.Sleep(60 * time.Second)
				// Retry
				details, err = c.GetRepoDetails(ctx, repoName)
				if err != nil {
					log.Printf("Retry failed for %s: %v", repoName, err)
					continue
				}
			} else {
				continue
			}
		}

		projects = append(projects, Project{
			RepoFullName:    details.FullName,
			GitHubURL:       details.HTMLURL,
			Stars:           details.StargazersCount,
			Description:     details.Description,
			PrimaryLanguage: details.Language,
			DockerfilePath:  dockerfilePath,
		})

		// Small delay to avoid hitting rate limits on repo API
		// Repo API limit is 5000/hour = ~1.4/sec, so 1s delay is safe
		time.Sleep(1 * time.Second)
	}

	return projects, nil
}
