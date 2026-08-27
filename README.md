# SEO Analyser (seo-crawler)

A Go backend that crawls a website's sitemap, analyses each page for on-page SEO
signals, scores it out of 100, and serves the results through a server-rendered
HTML/JS report UI. Includes account signup/login (PASETO cookie sessions) so
users can save and revisit past reports.

## Running

```bash
go run ./cmd/server
```

Requires a MongoDB instance — see `docker-compose.yml` for a local one:

```bash
docker compose up -d mongo
```

Configuration is read from environment variables (or a local `.env` file, via
`godotenv`):

| Variable | Default | Purpose |
|---|---|---|
| `PORT` | `8081` | HTTP listen port |
| `MONGO_URI` | `mongodb://localhost:27017` | MongoDB connection string |
| `DB_NAME` | `auditly` | Database name |
| `PASETO_SYMMETRIC_KEY` | *(dev fallback — set this in any real deployment)* | 32-byte key used to encrypt session tokens |
| `ENV` / `ENVIRONMENT` | `development` | Environment name (logged on startup) |
| `SHUTDOWN_TIMEOUT_SEC` | `10` | Grace period for shutting down in-flight requests on SIGTERM/SIGINT |

## Routes

| Route | Serves |
|---|---|
| `/` | Landing page (`cmd/server/views/index_content.html`) |
| `/seo-report` | The SEO crawler/report UI (`analyzer_content.html`) |
| `/reports` | Saved reports list for logged-in users (`reports_content.html`) |
| `/api/analyse?domain=&max_pages=` | Starts a crawl job, returns `{job_id}` |
| `/api/status?job_id=` | Poll job progress/status |
| `/api/results?job_id=` | Fetch accumulated/complete results |
| `/api/reports` | List the current user's saved reports |
| `/api/auth/signup` | Create an account |
| `/api/auth/login` | Log in, sets the `auth_token` session cookie (rate-limited) |
| `/api/auth/logout` | Clear the session cookie |
| `/api/auth/refresh` | Issue a fresh session cookie from a still-valid one |

Route registration lives in `internal/routes/routes.go`. Page and API handlers
are split across `internal/controllers/` (`pages.go` for HTML pages, `seo.go`
for the crawl/report API, `auth.go` for signup/login/session).

## Architecture

```
cmd/server/          entrypoint: config, DB connect, store, controllers, router, graceful shutdown
internal/config/     env var loading + startup validation
internal/db/         MongoDB client connection
internal/store/      persistence: reports (crawl results) and users
internal/controllers/ HTTP handlers (pages, crawl/report API, auth)
internal/routes/     mux wiring + CORS
internal/middleware/ per-IP login rate limiting
internal/crawler/    fetch → parse pipeline (fetcher, robots.txt, HTML parser)
internal/scorer/     100-point SEO scoring rules
internal/sitemap/    sitemap.xml / sitemap index discovery and parsing
internal/models/     shared result/job/user types
```

**Crawl pipeline:** `HandleAnalyse` creates an in-memory `Job`, then
`RunAnalysis` runs in a goroutine: discover URLs via `internal/sitemap`
(sitemap.xml, falling back to the homepage) → concurrently fetch + parse each
page via `internal/crawler` (respecting `robots.txt` and a per-host polite
delay) → score each page via `internal/scorer` → post-crawl broken-link
checking and duplicate title/description detection → persist the finished
job to MongoDB via `internal/store`. `/api/status` and `/api/results` poll
the in-memory `Job` while it's running, then fall back to the stored report
once it's no longer in memory.

**Auth:** passwords are hashed with bcrypt; sessions are PASETO v2 local
(symmetric-encryption) tokens carried in an `HttpOnly` cookie, checked for
both signature validity and expiry on every request that needs a user ID.
Crawls above 25 pages require a logged-in user.
