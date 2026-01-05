# AGENTS.md

## Current State

**Status:** ✅ All phases complete - production ready

**Architecture:** 
- Go backend + SQLite + vanilla HTML/JS frontend
- Running on port 8000 via systemd
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

### Future Phases (Not Yet Planned in Detail)
- Historical tracking (star count over time)
- Trend visualization (charts)
- "New this week" indicators
- Automated scheduled refreshes

---

## Decision Log

| Date | Decision | Rationale |
|------|----------|----------|
| 2026-01-05 | Use 6s delay between code search pages, 1s between repo fetches | GitHub code search limit is ~10/min; repo API is 5000/hr. Conservative delays avoid rate limits. |
| 2026-01-05 | Cap at 1000 results (10 pages) per query | GitHub code search API hard limit. |
| 2026-01-05 | Search multiple file types: Dockerfile, YAML, GitHub Actions | Expands coverage from 66 to 124 projects. Catches k8s manifests, docker-compose, CI configs. |

---

## Spec Reference

See `spec.md` for detailed requirements.
