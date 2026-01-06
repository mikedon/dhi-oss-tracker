# AGENTS.md

## Current State

**Status:** ✅ Phase 8 complete

**Architecture:** 
- Go backend + SQLite + vanilla HTML/JS frontend
- Running on port 8000 via systemd
- Searches: Dockerfiles (filename:Dockerfile), YAML/K8s (image: dhi.io/), GitHub Actions
- 91 projects tracked, 172K+ combined stars
- Tracks actual adoption dates (from git history) with links to adoption commits
- Historical snapshots recorded on each refresh
- GitHub PAT stored in `.env` (not committed)
- Public URL: https://dhi-oss-usage.exe.xyz:8000/

**Key Files:**
- `spec.md` - Full specification
- `.env` - GitHub token (gitignored)
- `cmd/server/main.go` - Main server entry point
- `internal/db/db.go` - Database layer with SQLite
- `internal/github/client.go` - GitHub API client
- `internal/api/api.go` - REST API handlers
- `static/index.html` - Frontend UI
- `dhi-oss-usage.service` - Systemd service file
- `dhi-oss-usage.db` - SQLite database (gitignored)

---

## Working Rules

1. **Git Usage:** We use git locally for version control.

2. **Commit After Each Phase:** We commit after completing each phase to create reasonable rollback points.

3. **Verify Each Phase:** Every phase includes verification steps. We confirm the phase works before moving on.

4. **Ask When Unsure:** If uncertain about a plan or task, ask for clarification rather than guess.

5. **Keep AGENTS.md Updated:** Update this file after each phase:
   - Update the "Current State" section at the top
   - Mark phase completion in the phases list
   - Another agent should be able to read this and understand the project state

6. **Detailed Commit Messages:** Write clear, descriptive commit messages that explain what was done and why.

---

## Phases

### Phase 1: Project Skeleton & Database
**Goal:** Set up Go project structure, SQLite database with schema, basic server running.

**Tasks:**
- Initialize Go module
- Create database schema (projects, refresh_jobs tables)
- Basic HTTP server on port 8000
- Health check endpoint

**Verify:** Server starts, health endpoint returns 200, database file created.

**Status:** ✅ Complete

---

### Phase 2: GitHub API Integration
**Goal:** Implement GitHub code search and repo details fetching.

**Tasks:**
- GitHub client with PAT authentication
- Code search for "dhi.io" in Dockerfiles
- Fetch repo details (stars, description)
- Handle pagination and rate limits
- Store results in database

**Verify:** Can trigger search, results stored in DB, rate limits respected.

**Status:** ✅ Complete

---

### Phase 3: API Endpoints
**Goal:** REST API for frontend to consume.

**Tasks:**
- GET /api/projects - list with filtering/sorting
- GET /api/stats - summary statistics
- POST /api/refresh - trigger async refresh
- GET /api/refresh/status - check refresh status

**Verify:** All endpoints return correct data, refresh runs async.

**Status:** ✅ Complete

---

### Phase 4: Basic Frontend
**Goal:** Functional UI showing projects and stats.

**Tasks:**
- HTML page with CSS styling
- Display summary stats
- Display project list (table)
- Search box for filtering
- Sort controls
- Refresh button with status indicator

**Verify:** Can view projects, search works, sort works, refresh triggers and updates.

**Status:** ✅ Complete

---

### Phase 5: Enhanced UX - Popularity Tiers
**Goal:** Visual hierarchy based on project popularity.

**Tasks:**
- Popular projects section (1000+ stars) with cards
- Notable projects section (100-999 stars)
- Star count filter dropdown
- Visual polish and responsive design

**Verify:** Popular/notable sections display correctly, filter works, looks good on mobile.

**Status:** ✅ Complete (merged into Phase 4)

---

### Phase 6: Systemd & Production Ready
**Goal:** Persistent deployment on exe.dev.

**Tasks:**
- Create systemd service file
- Install and enable service
- Verify auto-restart behavior
- Document deployment

**Verify:** Service runs after restart, accessible at public URL.

**Status:** ✅ Complete

---

---

### Phase 7: Automated Background Refresh
**Goal:** Automatically refresh data on a schedule without manual intervention.

**Approach:** Built-in Go scheduler using robfig/cron library.

**Key Features:**
- `REFRESH_SCHEDULE` env var with cron syntax (default: `"0 3 * * *"` = 3 AM daily)
- **Startup check:** If last refresh >24h old, trigger immediate refresh (handles missed schedules, restarts)
- Skip if refresh already running
- Manual refresh still works alongside scheduled

**Tasks:**
1. Add robfig/cron dependency
2. Create scheduler that runs on configured schedule
3. Add startup check for stale data (>24h since last refresh)
4. Add REFRESH_SCHEDULE env var parsing (empty = disabled)
5. Update systemd service with default schedule
6. Test: scheduled run, startup catch-up, manual still works

**Verify:** 
- Server auto-refreshes at configured time
- Server refreshes on startup if data is stale
- Logs show scheduled vs manual runs
- Manual refresh still works

**Status:** ✅ Complete (2026-01-05)

---

### Phase 8: Historical Tracking & Time-based View
**Goal:** Track DHI adoption over time and visualize trends.

**Approach:** Aggregate snapshots only (not per-project star history).

**Data Model:**
- New `refresh_snapshots` table: total_projects, total_stars, popular_count, notable_count, recorded_at
- Record one snapshot per successful refresh
- Daily granularity

**Why aggregate only:** Answers "Is DHI adoption growing?" without complexity of per-project tracking. Can add per-project later if needed.

**API Endpoints:**
- `GET /api/history` - time-series of snapshots
- `GET /api/projects/new?since=7d` - projects first seen in last N days

**UI Changes:**
- Main dashboard: "+N new this week" in stats bar + collapsible "New This Week" section
- New "History" tab:
  - Line chart (Chart.js): total projects over time
  - Optional second line: total stars
  - New projects grouped by week/month

**Tasks:**
1. Create refresh_snapshots table
2. Record snapshot on each refresh completion
3. API: GET /api/history endpoint
4. API: GET /api/projects/new?since=N endpoint
5. UI: Add "+N new this week" to stats bar
6. UI: Add collapsible "New This Week" section on dashboard
7. UI: Add "History" tab with Chart.js line chart
8. UI: Add new projects by week list in History tab

**Verify:** 
- Snapshots accumulate over multiple refreshes
- History tab shows trend chart
- New projects highlighted on dashboard
- Data persists across restarts

**Status:** ✅ Complete (2026-01-06)
- Backend: refresh_snapshots table, /api/history, /api/projects/new, new_this_week in stats
- Frontend: "New This Week" section, Dashboard/History tabs, Chart.js trend chart, projects by week list
- Adoption dates: Fetches actual adoption date from GitHub Commits API (first commit of file)
- Uses adopted_at instead of first_seen_at for accurate adoption timelines
- Adoption commit links: Stores and displays clickable links to the exact commit that added DHI

---

### Future Ideas (Not Yet Planned)
- Email/Slack notifications for new popular projects
- Export data as CSV/JSON
- Compare DHI adoption vs other hardened image solutions

---

## Decision Log

| Date | Decision | Rationale |
|------|----------|----------|
| 2026-01-05 | Use 6s delay between code search pages, 1s between repo fetches | GitHub code search limit is ~10/min; repo API is 5000/hr. Conservative delays avoid rate limits. |
| 2026-01-05 | Cap at 1000 results (10 pages) per query | GitHub code search API hard limit. |
| 2026-01-05 | Search multiple file types: Dockerfile, YAML, GitHub Actions | Expands coverage. Catches k8s manifests, docker-compose, CI configs. |
| 2026-01-05 | Use precise search patterns to exclude siddhi.io false positives | "FROM dhi.io" for Dockerfiles, "image: dhi.io/" for YAML. Siddhi.io is unrelated stream processing platform. |
| 2026-01-05 | Add filename:Dockerfile filter | Excludes documentation/README files that contain DHI examples but aren't actual usage. |
| 2026-01-06 | Track adopted_at from git history instead of first_seen_at | Shows when projects actually adopted DHI, not when we discovered them. More accurate adoption timelines. |
| 2026-01-06 | Store adoption_commit URL | Allows users to click through to see the exact commit that added DHI to a project. |

---

## Spec Reference

See `spec.md` for detailed requirements.

---

## Bugs

### Bug 1: "New This Week" count is incorrect
**Status:** ✅ Fixed (2026-01-06)
**Reported:** 2026-01-06
**Description:** Stats show "+33 New This Week" but only 12 projects were actually adopted in the last 7 days. The count appears inflated.
**Fix:** Changed from "last 7 days" to actual calendar week (Monday-Sunday). Added `startOfWeek()` helper and "thisweek" option to API.

### Bug 2: Add "Next update scheduled" to header
**Status:** ✅ Fixed (2026-01-06)
**Reported:** 2026-01-06
**Description:** Header shows "Last updated" but should also show when the next scheduled refresh will occur.
**Fix:** Added `SetNextRefreshFunc` to API, expose `next_refresh` in status endpoint, display in header as "Next: {time}".

### Bug 3: History chart x-axis shows repeated dates
**Status:** ✅ Fixed (2026-01-06)
**Reported:** 2026-01-06
**Description:** History tab chart shows "1/5" repeated multiple times on x-axis. Should show adoption trends over time (e.g., last 2 weeks) based on when projects adopted DHI, not when snapshots were recorded.
**Fix:** Changed /api/history to return adoption by date (based on adopted_at) instead of refresh snapshots. Shows last 14 days of adoption with cumulative counts.
