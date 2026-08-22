// Package store persists completed crawl jobs to a local SQLite database so
// past reports survive process restarts and can be listed on the Reports page.
package store

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"seo-crawler/internal/models"

	_ "modernc.org/sqlite"
)

type Store struct {
	db *sql.DB
}

// Open creates (if needed) the parent directory and the SQLite file at path,
// and ensures the reports table exists.
func Open(path string) (*Store, error) {
	if dir := filepath.Dir(path); dir != "." {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return nil, fmt.Errorf("create db dir: %w", err)
		}
	}

	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, fmt.Errorf("open db: %w", err)
	}
	db.SetMaxOpenConns(1) // modernc.org/sqlite is not safe for concurrent writers

	const schema = `
	CREATE TABLE IF NOT EXISTS reports (
		job_id       TEXT PRIMARY KEY,
		domain       TEXT NOT NULL,
		created_at   DATETIME NOT NULL,
		completed_at DATETIME,
		pages_total  INTEGER NOT NULL,
		avg_score    INTEGER NOT NULL,
		sitemap_url  TEXT,
		results_json TEXT NOT NULL
	);`
	if _, err := db.Exec(schema); err != nil {
		db.Close()
		return nil, fmt.Errorf("create schema: %w", err)
	}

	return &Store{db: db}, nil
}

func (s *Store) Close() error {
	return s.db.Close()
}

// ReportSummary is one row in the Reports list table.
type ReportSummary struct {
	JobID       string     `json:"job_id"`
	Domain      string     `json:"domain"`
	CreatedAt   time.Time  `json:"created_at"`
	CompletedAt *time.Time `json:"completed_at,omitempty"`
	PagesTotal  int        `json:"pages_total"`
	AvgScore    int        `json:"avg_score"`
}

// SaveReport persists a completed job. Safe to call multiple times for the
// same job ID (e.g. re-saving) — the row is replaced.
func (s *Store) SaveReport(job *models.Job) error {
	resultsJSON, err := json.Marshal(job.Results)
	if err != nil {
		return fmt.Errorf("marshal results: %w", err)
	}

	avg := averageScore(job.Results)

	_, err = s.db.Exec(
		`INSERT INTO reports (job_id, domain, created_at, completed_at, pages_total, avg_score, sitemap_url, results_json)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?)
		 ON CONFLICT(job_id) DO UPDATE SET
			domain=excluded.domain, completed_at=excluded.completed_at, pages_total=excluded.pages_total,
			avg_score=excluded.avg_score, sitemap_url=excluded.sitemap_url, results_json=excluded.results_json`,
		job.ID, job.Domain, job.CreatedAt, job.CompletedAt, len(job.Results), avg, job.SitemapURL, string(resultsJSON),
	)
	if err != nil {
		return fmt.Errorf("save report: %w", err)
	}
	return nil
}

// ListReports returns every saved report, most recent first.
func (s *Store) ListReports() ([]ReportSummary, error) {
	rows, err := s.db.Query(`SELECT job_id, domain, created_at, completed_at, pages_total, avg_score FROM reports ORDER BY created_at DESC`)
	if err != nil {
		return nil, fmt.Errorf("list reports: %w", err)
	}
	defer rows.Close()

	var out []ReportSummary
	for rows.Next() {
		var r ReportSummary
		var completedAt sql.NullTime
		if err := rows.Scan(&r.JobID, &r.Domain, &r.CreatedAt, &completedAt, &r.PagesTotal, &r.AvgScore); err != nil {
			return nil, fmt.Errorf("scan report: %w", err)
		}
		if completedAt.Valid {
			r.CompletedAt = &completedAt.Time
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// GetResults returns the stored page results for a job ID, or (nil, false) if
// no report with that ID has been saved.
func (s *Store) GetResults(jobID string) ([]models.SEOResult, bool, error) {
	var resultsJSON string
	err := s.db.QueryRow(`SELECT results_json FROM reports WHERE job_id = ?`, jobID).Scan(&resultsJSON)
	if err == sql.ErrNoRows {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, fmt.Errorf("get results: %w", err)
	}

	var results []models.SEOResult
	if err := json.Unmarshal([]byte(resultsJSON), &results); err != nil {
		return nil, false, fmt.Errorf("unmarshal results: %w", err)
	}
	return results, true, nil
}

func averageScore(results []models.SEOResult) int {
	if len(results) == 0 {
		return 0
	}
	total := 0
	for _, r := range results {
		total += r.Score
	}
	return total / len(results)
}
