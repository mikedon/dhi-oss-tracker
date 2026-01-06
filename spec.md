# DHI OSS Usage Tracker - Specification

## Overview

A web application to track adoption of Docker Hardened Images (DHI) Free tier by open source projects on GitHub. The goal is to understand DHI Free adoption patterns, especially among popular OSS projects.

### What is DHI?

Docker Hardened Images (DHI) are minimal, secure, production-ready container base and application images maintained by Docker. DHI Free is the Apache 2.0 licensed tier available at no cost. Projects reference DHI images via the `dhi.io` registry.

- Docs: https://docs.docker.com/dhi/
- Product page: https://www.docker.com/products/hardened-images/

## Data Source

- **GitHub Code Search API**
- **Search query:** `"dhi.io" language:Dockerfile`
- **Authentication:** GitHub Personal Access Token (PAT) with `public_repo` scope

## Core Features

### 1. Dashboard

Main view showing DHI adoption across OSS:

- **Summary Statistics**
  - Total projects using DHI
  - Combined stars across all projects
  - Count by popularity tier

- **Popular Projects Section** (1000+ stars)
  - Card-style display
  - Prominently featured at top
  - Shows: name, stars, description, link to repo

- **Notable Projects Section** (100-999 stars)
  - Highlighted but less prominent than Popular
  - Similar card or compact card display

- **Full Project List**
  - Table/list view of all projects
  - Searchable by name/description
  - Sortable by: stars, name, date discovered
  - Filterable by star count ranges

### 2. Project Information

For each project, store and display:

- Repository full name (owner/repo)
- GitHub URL
- Star count
- Description
- Primary language (if useful)
- Dockerfile path where dhi.io was found
- Date first discovered
- Date last seen/verified

### 3. Manual Refresh

- "Refresh" button on the UI
- Triggers async background job
- UI shows:
  - "Refresh in progress..." status
  - Last successful refresh timestamp
- Does not block UI - user can continue browsing
- Refresh job:
  - Paginates through GitHub code search results
  - Respects rate limits (may take several minutes)
  - Updates database incrementally
  - Marks completion when done

### 4. Historical Tracking (Future Phase)

- Track when each project was first discovered
- Show adoption trends over time
- "New this week/month" indicators
- Charts showing growth

## Popularity Tiers

| Tier | Star Range | Display Treatment |
|------|------------|-------------------|
| Popular | 1000+ | Featured cards, top of page |
| Notable | 100-999 | Highlighted section |
| Others | <100 | Standard list entries |

## Technical Architecture

### Stack

- **Backend:** Go
- **Database:** SQLite
- **Frontend:** HTML/CSS/JavaScript (vanilla or minimal framework)
- **Deployment:** Systemd service on exe.dev VM

### Database Schema (Initial)

```sql
-- Projects table
CREATE TABLE projects (
    id INTEGER PRIMARY KEY,
    repo_full_name TEXT UNIQUE NOT NULL,  -- e.g., "owner/repo"
    github_url TEXT NOT NULL,
    stars INTEGER DEFAULT 0,
    description TEXT,
    primary_language TEXT,
    dockerfile_path TEXT,                  -- path where dhi.io found
    file_url TEXT,                         -- direct link to file on GitHub
    source_type TEXT,                      -- 'Dockerfiles', 'YAML/K8s', 'GitHub Actions'
    adopted_at TIMESTAMP,                  -- when project actually adopted DHI (from git history)
    adoption_commit TEXT,                  -- URL to the commit that added DHI
    first_seen_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    last_seen_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);

-- Refresh jobs table
CREATE TABLE refresh_jobs (
    id INTEGER PRIMARY KEY,
    status TEXT NOT NULL,                  -- 'pending', 'running', 'completed', 'failed'
    started_at TIMESTAMP,
    completed_at TIMESTAMP,
    projects_found INTEGER DEFAULT 0,
    error_message TEXT,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);

-- For future historical tracking
CREATE TABLE star_history (
    id INTEGER PRIMARY KEY,
    project_id INTEGER REFERENCES projects(id),
    stars INTEGER,
    recorded_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);
```

### API Endpoints

```
GET  /api/projects          - List all projects (with filtering/sorting)
GET  /api/projects/:id      - Get single project details
GET  /api/stats             - Get summary statistics
POST /api/refresh           - Trigger a refresh job
GET  /api/refresh/status    - Get current refresh status
```

### GitHub API Considerations

- Code Search API has strict rate limits (~10 requests/minute for authenticated users)
- Must paginate through results (30 per page default, 100 max)
- Each code search result gives file info; need separate API call for repo details (stars, description)
- Strategy: 
  1. Search for code matches
  2. Extract unique repos
  3. Batch fetch repo details
  4. Respect rate limits with delays

## Configuration

- GitHub PAT stored in environment variable `GITHUB_TOKEN`
- Not committed to git
- Server port configurable (default 8000)

## UI/UX Guidelines

- Clean, simple interface
- Mobile-friendly (responsive)
- Fast initial load (data from SQLite cache)
- Visual distinction between popularity tiers
- Clear feedback during refresh operations
- Internal tool aesthetic (functional over flashy)

## Non-Goals (Out of Scope)

- User authentication (internal tool)
- Multi-user support
- Tracking private repositories
- Email/notification alerts (future consideration)

---

## Phase 7: Automated Background Refresh

### Overview
Automatically refresh data on a schedule without manual intervention.

### Implementation

**Approach:** Built-in Go scheduler using robfig/cron library.

**Configuration:**
- `REFRESH_SCHEDULE` env var with cron syntax
- Default: `"0 3 * * *"` (3 AM UTC daily)
- Set to empty string or `"disabled"` to disable auto-refresh

**Startup Check:**
- On server startup, check if last successful refresh is >24 hours old
- If so, trigger an immediate refresh
- This handles: server restarts, missed scheduled times, first-time setup

**Behavior:**
- Scheduled refresh runs in background (same as manual)
- Skip if refresh already running (no overlap)
- Log scheduled refresh start/completion
- Manual refresh button still works alongside scheduled

### API Changes
None - uses existing `/api/refresh` infrastructure internally.

### Configuration Example
```bash
# In .env or systemd environment
REFRESH_SCHEDULE="0 3 * * *"   # Daily at 3 AM UTC
REFRESH_SCHEDULE="0 */6 * * *" # Every 6 hours
REFRESH_SCHEDULE=""            # Disabled
```

---

## Phase 8: Historical Tracking & Time-based View

### Overview
Track DHI adoption over time and visualize trends.

### Data Model

**New table: `refresh_snapshots`**
```sql
CREATE TABLE refresh_snapshots (
    id INTEGER PRIMARY KEY,
    recorded_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    total_projects INTEGER,
    total_stars INTEGER,
    popular_count INTEGER,   -- 1000+ stars
    notable_count INTEGER    -- 100-999 stars
);
```

**Tracking approach:**
- Record one snapshot per successful refresh
- Aggregate data only (not per-project star history)
- Keep daily granularity for now

**Why aggregate only:**
- Answers the key question: "Is DHI adoption growing?"
- Simpler data model and queries
- Per-project history can be added later if needed

### API Endpoints

**GET /api/history**
```json
{
  "snapshots": [
    {"date": "2026-01-05", "total_projects": 90, "total_stars": 172400},
    {"date": "2026-01-06", "total_projects": 92, "total_stars": 173100},
    ...
  ]
}
```

**GET /api/projects/new?since=7d**
```json
[
  {
    "repo_full_name": "new/project",
    "stars": 50,
    "adopted_at": "2026-01-04T...",
    "adoption_commit": "https://github.com/owner/repo/commit/abc123"
  }
]
```

### Adoption Date Tracking

**Approach:**
- Use GitHub Commits API to get the first commit of each file containing dhi.io
- Store as `adopted_at` (actual adoption date) distinct from `first_seen_at` (when we discovered it)
- Also store `adoption_commit` URL linking to the exact commit
- This gives accurate adoption timelines based on when projects actually merged DHI changes

### UI Changes

**Main Dashboard:**
- Stats bar: Add "+N new this week" indicator (based on `adopted_at`)
- New collapsible section: "New This Week" showing recently adopted project cards
- "Adopted" dates are clickable links to the adoption commit

**New "History" Tab:**
- Line chart showing total projects over time (using Chart.js)
- Second line showing total stars
- New projects grouped by week with clickable commit links (🔗 icon)

### UI Mockup (History Tab)
```
[Dashboard] [History]

📈 DHI Adoption Over Time
┌─────────────────────────────────────┐
│     ╭─────────────────────╮         │
│    ╱                       ╲        │  <- Line chart
│   ╱                         ───     │
│  ╱                                  │
└─────────────────────────────────────┘
  Jan    Feb    Mar    Apr    May

📅 New Projects by Week
┌─────────────────────────────────────┐
│ Week of Jan 1, 2026 (3 new)         │
│   • new/repo1 - 45 ⭐               │
│   • another/repo - 12 ⭐            │
│   • third/one - 5 ⭐                │
├─────────────────────────────────────┤
│ Week of Dec 25, 2025 (1 new)        │
│   • holiday/project - 8 ⭐          │
└─────────────────────────────────────┘
```

## Success Criteria

1. Can see all OSS projects using DHI at a glance
2. Popular projects are immediately visible
3. Can search and filter the full list
4. Can trigger refresh and see updated data
5. Page loads quickly from cached data
