# SEO Analyser (seo-crawler)

A Go backend that crawls a website's sitemap, analyses each page for on-page SEO signals, scores them, and serves the results through an embedded HTML/JS report UI.

## Running

```bash
go run ./cmd/server      # do NOT run `go run cmd/server/main.go` — see note below
```

Serves on `http://localhost:8080`.

**Important:** always run `go run ./cmd/server` (the package directory), never `go run cmd/server/main.go` (a single file). Naming a specific file tells Go to compile only that file, silently excluding sibling files in the same package like `routes.go` — this produces a `s.setupRoutes undefined` build error.

## Routes

| Route | Serves |
|---|---|
| `/` | `cmd/server/homepage.html` (embedded) — marketing/landing page |
| `/seo-report` | `cmd/server/index.html` (embedded) — the SEO crawler/report UI |
| `/api/analyse?domain=&max_pages=` | starts a crawl job, returns `{job_id}` |
| `/api/status?job_id=` | poll job progress/status |
| `/api/results?job_id=` | fetch accumulated results |

Route registration lives in `cmd/server/routes.go` (`setupRoutes()`), handlers and the `Server` struct live in `cmd/server/main.go`.

`internal/server/` (`handlers.go`, `routes.go`, `middleware.go`) is a **dead/unused parallel implementation** — nothing in the module imports `seo-crawler/internal/server`. The real server is entirely in `cmd/server/`.

## `homepage.html` — known constraint

`cmd/server/homepage.html` is not hand-authored markup. It's an opaque, bundled export from an external page-builder tool: the initial HTML is just a loading placeholder (`__bundler_thumbnail`/`__bundler_loading`), and a large gzip+base64-compressed JavaScript blob (referenced by a random UUID `<script src="...">` tag) does the real work at runtime — it **replaces the entire document** once "unpacking" finishes (confirmed by inspecting the decompressed script as text; it explicitly handles state that "persists across replaceWith").

Practical implications:
- There is no static content in this file to read, copy, or extract a design from.
- Any static HTML injected into `homepage.html` (e.g. a shared header/footer) will render briefly and then get wiped out when the bundle finishes loading and swaps in its own content.
- As a result, the shared header/footer described below was added **only to `index.html`** (the `/seo-report` page), not to `homepage.html`. Revisit `homepage.html` only once a real, non-opaque design source is available for it (screenshot, Figma, or authored HTML).

## `/seo-report` (`index.html`) — design/content additions

### Report content — SEO & marketing signals surfaced

The crawler (`internal/crawler`, `internal/scorer`, `internal/models`) already collects far more than the original table showed. Added to the per-page detail panel and summary cards (no backend changes — pure frontend surfacing):

- **Google Search Preview** — SERP-style mockup (title, URL breadcrumb, description) built from `title`/`meta_description`.
- **Social Share Preview** — Open Graph card mockup (image, title, description) from `og_tags`, with a warning callout when OG tags or `og:image` are missing.
- **Structured Data (Schema.org)** — displays detected JSON-LD types (`schema_types`), or a warning when none are found (rich-result eligibility).
- Summary cards: **Pages w/ Structured Data** and **Missing Open Graph** counts.

(Canonical, robots meta, viewport, link stats, and security headers were already surfaced pre-existing — only the above were net-new.)

Not yet implemented (backend work, next planned pass):
- Duplicate title/meta-description detection across pages.
- Broken internal links / redirect chain checking.
- Thin-content flags, hreflang, orphan-page detection.

### Page chrome fixes

- Added a sticky **site header** (brand + `Home` / `SEO Report` nav, active link highlighted via `location.pathname`) and a **site footer** (auto-updating copyright year + same nav) to `index.html`.
- Fixed: the `xlsx` export library was being loaded **three times** from two different CDNs (leftover `<!-- BEFORE -->` / `<!-- AFTER -->` debug comments were never cleaned up after a prior fix) — reduced to a single `jsdelivr` load.
- Fixed a dead CSS selector `.#results-section` (invalid — mixes class and id syntax, so it silently never matched) → corrected to `#results-section`, restoring the intended mobile padding.

## Architecture notes

See `CLAUDE.md` for full crawl-pipeline architecture (sitemap discovery → concurrent fetch/parse → scoring → job polling) and package responsibilities.
