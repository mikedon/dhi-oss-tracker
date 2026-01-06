# DHI OSS Usage Tracker

A web application to track adoption of [Docker Hardened Images (DHI)](https://www.docker.com/products/hardened-images/) by open source projects on GitHub.

**Live:** https://dhi-oss-usage.exe.xyz:8000/

![Dashboard Screenshot](https://img.shields.io/badge/projects-91-blue) ![Stars](https://img.shields.io/badge/combined%20stars-172K-yellow)

## Features

### Dashboard
- **Summary Statistics:** Total projects, combined stars, popular (1K+) and notable (100+) project counts
- **New This Week:** Projects that adopted DHI in the current calendar week with clickable links to adoption commits
- **Popular Projects:** Featured cards for projects with 1000+ stars
- **Notable Projects:** Highlighted section for projects with 100-999 stars
- **Full Project List:** Searchable, sortable, filterable table of all projects

### History Tab
- **Adoption Trend Chart:** Line chart showing cumulative project count and stars over time
- **New Projects by Week:** Projects grouped by adoption week with links to GitHub and adoption commits

### Automatic Refresh
- Scheduled daily refresh at 3 AM UTC (configurable)
- Manual refresh button available
- Shows "Last updated" and "Next scheduled" times

### Notifications Tab
- **Alert System:** Get notified when new projects adopt DHI
- **Email Notifications:** Uses SendGrid for email delivery (simplified configuration)
- **Slack Notifications:** Post to Slack channels via webhooks
- **Manage Notifications:** Add, edit, enable/disable, delete, and test notifications
- **Auto-trigger:** Notifications fire automatically when new projects are detected during refresh
- **Test Functionality:** Verify notification configuration with test messages

## How It Works

1. **GitHub Code Search:** Searches for `dhi.io` references in:
   - Dockerfiles (`FROM dhi.io/...`)
   - YAML/K8s manifests (`image: dhi.io/...`)
   - GitHub Actions workflows

2. **Repository Details:** Fetches stars, description, and language for each unique repository

3. **Adoption Date Tracking:** Uses GitHub Commits API to find when each project first added DHI (the actual adoption date, not when we discovered it)

4. **Historical Snapshots:** Records adoption trends over time for visualization

## Tech Stack

- **Backend:** Go
- **Database:** SQLite
- **Frontend:** Vanilla HTML/CSS/JavaScript + Chart.js
- **Deployment:** Systemd service on exe.dev VM

## API Endpoints

| Endpoint | Description |
|----------|-------------|
| `GET /api/projects` | List projects with filtering/sorting |
| `GET /api/projects/new?since=thisweek` | Projects adopted since start of week |
| `GET /api/stats` | Summary statistics |
| `GET /api/history?days=14` | Adoption history by date |
| `GET /api/refresh/status` | Current refresh status and next scheduled time |
| `POST /api/refresh` | Trigger manual refresh |
| `GET /api/source-types` | List of source types (Dockerfile, YAML, etc.) |
| `GET /api/notifications` | List all notification configurations |
| `POST /api/notifications` | Create new notification configuration |
| `GET /api/notifications/:id` | Get single notification configuration |
| `PUT /api/notifications/:id` | Update notification configuration |
| `DELETE /api/notifications/:id` | Delete notification configuration |
| `POST /api/notifications/:id/test` | Send test notification |

## Project Structure

```
dhi-oss-usage/
├── cmd/server/main.go      # Entry point, scheduler setup
├── internal/
│   ├── api/api.go               # REST API handlers
│   ├── db/db.go                 # SQLite database layer
│   ├── github/client.go         # GitHub API client
│   └── notifications/           # Notification system
│       ├── notifications.go     # Service layer
│       ├── slack.go             # Slack webhook provider
│       └── email.go             # SendGrid email provider
├── static/index.html       # Frontend UI
├── spec.md                 # Detailed specification
├── AGENTS.md               # Development notes and decisions
└── dhi-oss-usage.service   # Systemd service file
```

## Documentation for AI Agents

This project is designed to be worked on by AI coding agents. Two key files provide context:

### spec.md
The **product specification** - describes what the application should do:
- Feature requirements and user stories
- API endpoint specifications with request/response examples
- Database schema
- UI/UX guidelines
- Phased development plan

### AGENTS.md
The **development state and working rules** - describes how to work on this project:
- Current project state and architecture overview
- Working rules (git usage, commit practices, verification steps)
- Phase completion status
- Bug tracking with status
- Decision log explaining key technical choices

### Keeping Docs Updated

**Anyone working on this project (human or AI) is responsible for keeping `spec.md` and `AGENTS.md` up to date.** This ensures that any agent can quickly get full context on the project state, understand what's been built, and continue development seamlessly.

After making changes:
1. Update `AGENTS.md` with current state, completed work, and any new decisions
2. Update `spec.md` if requirements, APIs, or schemas changed
3. Commit documentation updates alongside code changes

## Configuration

Environment variables:

| Variable | Default | Description |
|----------|---------|-------------|
| `PORT` | `8000` | HTTP server port |
| `DB_PATH` | `dhi-oss-usage.db` | SQLite database path |
| `GITHUB_TOKEN` | (required) | GitHub PAT with `public_repo` scope |
| `REFRESH_SCHEDULE` | `0 3 * * *` | Cron schedule for auto-refresh |
| `STATIC_DIR` | `static` | Static files directory |
| `SENDGRID_API_KEY` | (required for email) | SendGrid API key for email notifications |
| `SENDGRID_FROM_EMAIL` | (required for email) | Default sender email address |
| `SENDGRID_SMTP_HOST` | `smtp.sendgrid.net` | SendGrid SMTP host |
| `SENDGRID_SMTP_PORT` | `587` | SendGrid SMTP port |
| `SENDGRID_USERNAME` | `apikey` | SendGrid SMTP username |

## Local Development

```bash
# Clone and enter directory
cd dhi-oss-usage

# Set GitHub token
export GITHUB_TOKEN=your_token_here

# Build and run
go build -o server ./cmd/server
./server

# Open http://localhost:8000
```

## Setting Up Notifications

### Email Notifications (SendGrid)

1. **Get SendGrid API Key:**
   - Sign up at [sendgrid.com](https://sendgrid.com) (free tier: 100 emails/day)
   - Create an API key with "Mail Send" permissions

2. **Configure environment:**
   ```bash
   # In .env file
   SENDGRID_API_KEY=SG.your_api_key_here
   SENDGRID_FROM_EMAIL=noreply@yourdomain.com
   ```

3. **Verify sender:** SendGrid requires sender verification - verify your email or domain

4. **Create notification in UI:**
   - Open Notifications tab
   - Click "Add Notification"
   - Select "Email" type
   - Enter recipient email
   - Test and enable

### Slack Notifications

1. **Create Slack Webhook:**
   - Go to [api.slack.com/apps](https://api.slack.com/apps)
   - Create an app and enable Incoming Webhooks
   - Add webhook to desired channel

2. **Create notification in UI:**
   - Open Notifications tab
   - Click "Add Notification"
   - Select "Slack" type
   - Paste webhook URL
   - Test and enable

### How Notifications Work

- **Trigger:** Automatic after each successful refresh when new projects detected
- **Scope:** New projects adopted in the current calendar week (Monday-Sunday)
- **Content:** Project name, stars, description, link to adoption commit
- **Management:** Enable/disable, test, or delete notifications anytime

See [SENDGRID_SETUP.md](SENDGRID_SETUP.md) for detailed email configuration instructions.

## Deployment

The service runs on exe.dev with systemd:

```bash
# Install service
sudo cp dhi-oss-usage.service /etc/systemd/system/
sudo systemctl daemon-reload
sudo systemctl enable dhi-oss-usage
sudo systemctl start dhi-oss-usage

# View logs
journalctl -u dhi-oss-usage -f
```

## Database Schema

```sql
CREATE TABLE projects (
    id INTEGER PRIMARY KEY,
    repo_full_name TEXT UNIQUE NOT NULL,
    github_url TEXT NOT NULL,
    stars INTEGER DEFAULT 0,
    description TEXT,
    primary_language TEXT,
    dockerfile_path TEXT,
    file_url TEXT,
    source_type TEXT,
    adopted_at TIMESTAMP,        -- When project adopted DHI
    adoption_commit TEXT,        -- Link to adoption commit
    first_seen_at TIMESTAMP,
    last_seen_at TIMESTAMP,
    created_at TIMESTAMP,
    updated_at TIMESTAMP
);

CREATE TABLE refresh_snapshots (
    id INTEGER PRIMARY KEY,
    recorded_at TIMESTAMP,
    total_projects INTEGER,
    total_stars INTEGER,
    popular_count INTEGER,
    notable_count INTEGER
);

CREATE TABLE notifications (
    id INTEGER PRIMARY KEY,
    name TEXT NOT NULL,
    type TEXT NOT NULL,              -- 'slack' or 'email'
    enabled BOOLEAN DEFAULT 1,
    config_json TEXT NOT NULL,       -- JSON config specific to type
    last_triggered_at TIMESTAMP,
    created_at TIMESTAMP,
    updated_at TIMESTAMP
);
```

## Rate Limits

GitHub API rate limits are handled conservatively:
- Code search: 6 second delay between pages (~10 req/min limit)
- Repository details: 1 second delay between requests
- Commits API (for adoption dates): 0.5 second delay

## What is DHI?

Docker Hardened Images (DHI) are minimal, secure, production-ready container base and application images maintained by Docker. DHI Free is the Apache 2.0 licensed tier available at no cost.

- **Registry:** `dhi.io`
- **Docs:** https://docs.docker.com/dhi/
- **Product page:** https://www.docker.com/products/hardened-images/

## License

Internal tool - not licensed for external use.
