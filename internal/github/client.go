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
	FileURL         string
	SourceType      string
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

// SearchQuery represents a single search query configuration
type SearchQuery struct {
	Name  string
	Query string
}

// GetSearchQueries returns all the search queries we use to find DHI usage
// These are tuned to find actual DHI registry usage, not false positives like "siddhi.io"
func GetSearchQueries() []SearchQuery {
	return []SearchQuery{
		// FROM dhi.io in actual Dockerfiles (not docs/READMEs)
		// filename:Dockerfile is a substring match, so catches Dockerfile.dev, app.Dockerfile, etc.
		{"Dockerfiles", `"FROM dhi.io" filename:Dockerfile`},
		// image: dhi.io/ - K8s/docker-compose image references with trailing slash
		// The "image: " prefix distinguishes from URLs like siddhi.io
		{"YAML/K8s", `"image: dhi.io/" language:YAML`},
		// dhi.io/ in CI workflows - image references in GitHub Actions
		{"GitHub Actions", `"dhi.io/" path:.github/workflows`},
	}
}

// SearchResult holds a repo and the file path where dhi.io was found
type SearchResult struct {
	RepoFullName string
	FilePath     string
	FileURL      string
	SourceType   string // e.g., "Dockerfile", "YAML", "GitHub Actions"
}

// SearchDHIUsage searches for dhi.io references across multiple file types
// Returns unique repos found with their file paths
func (c *Client) SearchDHIUsage(ctx context.Context, progressFn func(queryName string, found int, page int)) (map[string]SearchResult, error) {
	repos := make(map[string]SearchResult) // repo full name -> search result
	queries := GetSearchQueries()

	for _, sq := range queries {
		log.Printf("Starting search: %s", sq.Name)
		page := 1
		perPage := 100

		for {
			select {
			case <-ctx.Done():
				return repos, ctx.Err()
			default:
			}

			query := url.QueryEscape(sq.Query)
			endpoint := fmt.Sprintf("/search/code?q=%s&per_page=%d&page=%d", query, perPage, page)

			log.Printf("[%s] Searching page %d...", sq.Name, page)
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
					fileURL := fmt.Sprintf("https://github.com/%s/blob/HEAD/%s", item.Repository.FullName, item.Path)
					repos[item.Repository.FullName] = SearchResult{
						RepoFullName: item.Repository.FullName,
						FilePath:     item.Path,
						FileURL:      fileURL,
						SourceType:   sq.Name,
					}
				}
			}

			if progressFn != nil {
				progressFn(sq.Name, len(repos), page)
			}

			log.Printf("[%s] Page %d: found %d items, total unique repos: %d", sq.Name, page, len(searchResp.Items), len(repos))

			// Check if we've got all results
			if len(searchResp.Items) < perPage || page*perPage >= searchResp.TotalCount {
				break
			}

			// GitHub only returns first 1000 results per query
			if page >= 10 {
				log.Printf("[%s] Reached GitHub's 1000 result limit", sq.Name)
				break
			}

			page++
			// Rate limit delay for code search
			time.Sleep(searchRateDelay)
		}

		// Delay between different search queries
		time.Sleep(searchRateDelay)
	}

	return repos, nil
}

// CommitInfo represents a commit from GitHub API
type CommitInfo struct {
	Commit struct {
		Author struct {
			Date time.Time `json:"date"`
		} `json:"author"`
	} `json:"commit"`
}

// GetFileFirstCommitDate gets the date of the first commit for a file
// This approximates when DHI was adopted (when the file was created or dhi.io was added)
func (c *Client) GetFileFirstCommitDate(ctx context.Context, repoFullName, filePath string) (*time.Time, error) {
	// Get commits for this file, oldest first (we want the first commit)
	// GitHub returns newest first by default, so we need to get all and take the last
	// Or we can use per_page=1 and check if there's a Link header for "last" page
	
	path := url.PathEscape(filePath)
	// First, try to get a small page to see total
	endpoint := fmt.Sprintf("/repos/%s/commits?path=%s&per_page=1", repoFullName, path)
	
	body, err := c.doRequest(ctx, "GET", endpoint)
	if err != nil {
		return nil, err
	}
	
	var commits []CommitInfo
	if err := json.Unmarshal(body, &commits); err != nil {
		return nil, err
	}
	
	if len(commits) == 0 {
		return nil, fmt.Errorf("no commits found for file %s", filePath)
	}
	
	// If only one commit, return it
	if len(commits) == 1 {
		return &commits[0].Commit.Author.Date, nil
	}
	
	// Otherwise, need to paginate to get the oldest commit
	// Get up to 100 commits and take the oldest
	endpoint = fmt.Sprintf("/repos/%s/commits?path=%s&per_page=100", repoFullName, path)
	body, err = c.doRequest(ctx, "GET", endpoint)
	if err != nil {
		return nil, err
	}
	
	if err := json.Unmarshal(body, &commits); err != nil {
		return nil, err
	}
	
	if len(commits) == 0 {
		return nil, fmt.Errorf("no commits found for file %s", filePath)
	}
	
	// Return the oldest commit (last in the array since GitHub returns newest first)
	oldest := commits[len(commits)-1]
	return &oldest.Commit.Author.Date, nil
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
	// Step 1: Search for all repos across multiple file types
	if progressFn != nil {
		progressFn("searching", 0, 0)
	}

	repos, err := c.SearchDHIUsage(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("searching for dhi.io usage: %w", err)
	}

	log.Printf("Found %d unique repositories", len(repos))

	// Step 2: Fetch details for each repo
	projects := make([]Project, 0, len(repos))
	i := 0
	for repoName, searchResult := range repos {
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
			DockerfilePath:  searchResult.FilePath,
			FileURL:         searchResult.FileURL,
			SourceType:      searchResult.SourceType,
		})

		// Small delay to avoid hitting rate limits on repo API
		// Repo API limit is 5000/hour = ~1.4/sec, so 1s delay is safe
		time.Sleep(1 * time.Second)
	}

	return projects, nil
}
