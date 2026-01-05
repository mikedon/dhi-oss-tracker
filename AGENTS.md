# AGENTS.md

## Current State

**Status:** ✅ Phases 1-6 complete, planning Phase 7-8

**Architecture:** 
- Go backend + SQLite + vanilla HTML/JS frontend
- Running on port 8000 via systemd
- Searches: Dockerfiles (filename:Dockerfile), YAML/K8s (image: dhi.io/), GitHub Actions
- 90 projects tracked, 172K+ combined stars (false positives removed)
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

**Design Considerations:**
- Refresh frequency: Daily seems reasonable (DHI adoption won't change hourly)
- Time of day: Run during off-peak hours (e.g., 3 AM UTC)
- Implementation options:
  - A) Built-in Go scheduler (ticker/cron library)
  - B) Systemd timer unit (separate from main service)
  - C) External cron job calling the API
- Rate limits: Current refresh takes ~4 minutes, well within GitHub limits
- Error handling: Log failures, don't crash the service
- Overlap prevention: Skip if refresh already running

**Proposed Approach:** Built-in Go scheduler using a cron library. Keeps everything self-contained, easy to configure via environment variable.

**Tasks:**
- Add cron scheduling library (e.g., robfig/cron)
- Add REFRESH_SCHEDULE env var (default: "0 3 * * *" = 3 AM daily)
- Add ability to disable auto-refresh (REFRESH_SCHEDULE="" or "disabled")
- Log scheduled refresh start/completion
- Ensure manual refresh still works alongside scheduled

**Verify:** Service auto-refreshes at configured time, logs show scheduled runs, manual refresh still works.

**Status:** ⬜ Not started

---

### Phase 8: Historical Tracking & Time-based View
**Goal:** Track DHI adoption over time and visualize trends.

**Design Considerations:**
- What to track:
  - When each project was first discovered (already have first_seen_at)
  - Star count history (snapshot at each refresh)
  - Total project count over time
- Storage: star_history table (project_id, stars, recorded_at)
- Views to add:
  - "New This Week/Month" section on dashboard
  - Timeline/history tab showing adoption growth
  - Per-project star history (optional, might be overkill)
- Visualization: Simple chart showing projects over time, or just a table

**Proposed Approach:** 
- Add star_history table, record snapshot on each refresh
- Add /api/history endpoint returning time-series data
- Add new "History" tab in UI with:
  - Line chart: total projects over time
  - "New projects" list grouped by week/month
  - Optional: star growth for top projects

**Tasks:**
- Create star_history table (or rename to refresh_snapshots)
- Record snapshot data on each refresh completion
- API endpoint: GET /api/history (returns time-series)
- API endpoint: GET /api/projects/new?since=7d (new projects)
- UI: Add History tab with chart (use simple library like Chart.js)
- UI: Add "New this week" badge/section on main dashboard

**Verify:** History tab shows adoption trend, new projects highlighted, data accumulates over multiple refreshes.

**Status:** ⬜ Not started

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

---

## Spec Reference

See `spec.md` for detailed requirements.
