// backend/cmd/server/main.go — PURE GO VERSION
package main

import (
	"context"
	"embed"
	"encoding/json"
	"fmt"
	"html/template"
	"log"
	"net/http"
	"seo-crawler/internal/crawler"
	"seo-crawler/internal/models"
	"seo-crawler/internal/scorer"
	"seo-crawler/internal/sitemap"
	"seo-crawler/internal/store"
	"strings"
	"sync"
	"time"
)

//go:embed views/*.html
var viewsFS embed.FS

// PageData is the data every page template is rendered with. ActivePath
// drives the header nav's active-link styling; Year feeds the footer.
type PageData struct {
	Title       string
	Description string
	ActivePath  string
	Year        int
}

// mustParsePage builds a standalone template set for one page: the shared
// layout + header + footer, plus that page's own content file.
func mustParsePage(contentFile string) *template.Template {
	return template.Must(template.ParseFS(viewsFS,
		"views/layout.html", "views/header.html", "views/footer.html", "views/"+contentFile))
}

type Server struct {
	crawlerCfg crawler.Config
	jobs       map[string]*models.Job
	jobsMu     sync.RWMutex
	store      *store.Store

	homeTpl      *template.Template
	seoReportTpl *template.Template
	reportsTpl   *template.Template
}

func New(dbPath string) (*Server, error) {
	st, err := store.Open(dbPath)
	if err != nil {
		return nil, err
	}
	return &Server{
		crawlerCfg:   crawler.DefaultConfig(),
		jobs:         make(map[string]*models.Job),
		store:        st,
		homeTpl:      mustParsePage("index_content.html"),
		seoReportTpl: mustParsePage("analyzer_content.html"),
		reportsTpl:   mustParsePage("reports_content.html"),
	}, nil
}

func (s *Server) Start(addr string) error {
	mux := s.setupRoutes()

	log.Printf("SEO Analyser running at http://localhost%s", addr)
	return http.ListenAndServe(addr, mux)
}

func (s *Server) handleSeoReport(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	s.seoReportTpl.ExecuteTemplate(w, "layout", PageData{
		Title:      "Auditly · SEO Report",
		ActivePath: "/seo-report",
		Year:       time.Now().Year(),
	})
}

func (s *Server) handleHomepage(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	s.homeTpl.ExecuteTemplate(w, "layout", PageData{
		Title:       "Auditly · Free SEO Audit Tool",
		Description: "Crawl your sitemap, score every page, and get actionable SEO fixes for titles, meta tags, headings, images, speed, links, Open Graph and structured data — free.",
		ActivePath:  "/",
		Year:        time.Now().Year(),
	})
}

func (s *Server) handleReports(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	s.reportsTpl.ExecuteTemplate(w, "layout", PageData{
		Title:      "Auditly · Saved Reports",
		ActivePath: "/reports",
		Year:       time.Now().Year(),
	})
}

func cors(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, OPTIONS")
		if r.Method == "OPTIONS" {
			w.WriteHeader(200)
			return
		}
		next(w, r)
	}
}

func (s *Server) handleAnalyse(w http.ResponseWriter, r *http.Request) {
	domain := r.URL.Query().Get("domain")
	if domain == "" {
		http.Error(w, `{"error":"domain required"}`, http.StatusBadRequest)
		return
	}

	maxPages := 50
	if mp := r.URL.Query().Get("max_pages"); mp != "" {
		fmt.Sscanf(mp, "%d", &maxPages)
	}

	jobID := fmt.Sprintf("%s_%d", domain, time.Now().Unix())
	job := &models.Job{
		ID:        jobID,
		Status:    "fetching_sitemap",
		Domain:    domain,
		CreatedAt: time.Now(),
	}

	s.jobsMu.Lock()
	s.jobs[jobID] = job
	s.jobsMu.Unlock()

	go s.runAnalysis(job, maxPages)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"job_id": jobID})
}

func (s *Server) runAnalysis(job *models.Job, maxPages int) {
	var urls []string
	var sitemapURL string
	var err error

	if maxPages == 1 {
		// Single page mode: skip sitemap discovery and analyze exact URL
		targetURL := job.Domain
		if !strings.HasPrefix(targetURL, "http") {
			targetURL = "https://" + targetURL
		}
		urls = []string{targetURL}
	} else {
		// Multi-page mode: discover URLs via sitemap
		sf := sitemap.NewFetcher()
		urls, sitemapURL, err = sf.Discover(job.Domain)
		if err != nil {
			s.jobsMu.Lock()
			job.Status = "error"
			job.Error = err.Error()
			s.jobsMu.Unlock()
			return
		}
	}

	s.jobsMu.Lock()
	job.SitemapURL = sitemapURL
	if len(urls) == 0 {
		targetURL := job.Domain
		if !strings.HasPrefix(targetURL, "http") {
			targetURL = "https://" + targetURL
		}
		urls = []string{targetURL}
	}
	if len(urls) > maxPages {
		urls = urls[:maxPages]
	}
	job.URLs = urls
	job.Total = len(urls)
	job.Status = "analysing"
	s.jobsMu.Unlock()

	cfg := s.crawlerCfg
	cfg.MaxPages = maxPages
	c := crawler.New(cfg)

	done := make(chan struct{})
	go func() {
		ticker := time.NewTicker(500 * time.Millisecond)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				s.jobsMu.Lock()
				job.Progress = int(c.Progress())
				s.jobsMu.Unlock()
			case <-done:
				return
			}
		}
	}()

	stream := c.AnalyzePagesStream(context.Background(), urls)

	for res := range stream {
		scorer.CalculateScore(&res)

		s.jobsMu.Lock()
		job.Results = append(job.Results, res)
		s.jobsMu.Unlock()
	}

	close(done)

	s.jobsMu.Lock()
	job.Progress = len(job.Results)
	job.Status = "complete"
	now := time.Now()
	job.CompletedAt = &now
	s.jobsMu.Unlock()

	if len(job.Results) > 0 {
		if err := s.store.SaveReport(job); err != nil {
			log.Printf("failed to save report for job %s: %v", job.ID, err)
		}
	}
}

func (s *Server) handleStatus(w http.ResponseWriter, r *http.Request) {
	jobID := r.URL.Query().Get("job_id")
	s.jobsMu.RLock()
	job, exists := s.jobs[jobID]

	if !exists {
		s.jobsMu.RUnlock()
		http.Error(w, `{"error":"job not found"}`, http.StatusNotFound)
		return
	}

	response := map[string]interface{}{
		"job_id":        job.ID,
		"status":        job.Status,
		"domain":        job.Domain,
		"progress":      job.Progress,
		"total":         job.Total,
		"sitemap_url":   job.SitemapURL,
		"error":         job.Error,
		"results_count": len(job.Results),
	}
	s.jobsMu.RUnlock()

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

func (s *Server) handleResults(w http.ResponseWriter, r *http.Request) {
	jobID := r.URL.Query().Get("job_id")
	s.jobsMu.RLock()
	job, exists := s.jobs[jobID]

	if exists {
		resultsCopy := make([]models.SEOResult, len(job.Results))
		copy(resultsCopy, job.Results)
		status := job.Status
		s.jobsMu.RUnlock()

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"results": resultsCopy,
			"status":  status,
		})
		return
	}
	s.jobsMu.RUnlock()

	// Not an in-memory (in-progress) job — fall back to a saved report.
	results, found, err := s.store.GetResults(jobID)
	if err != nil {
		log.Printf("failed to load report %s: %v", jobID, err)
		http.Error(w, `{"error":"failed to load report"}`, http.StatusInternalServerError)
		return
	}
	if !found {
		http.Error(w, `{"error":"job not found"}`, http.StatusNotFound)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"results": results,
		"status":  "complete",
	})
}

func (s *Server) handleReportsList(w http.ResponseWriter, r *http.Request) {
	reports, err := s.store.ListReports()
	if err != nil {
		log.Printf("failed to list reports: %v", err)
		http.Error(w, `{"error":"failed to list reports"}`, http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"reports": reports,
	})
}

func main() {
	srv, err := New("data/auditly.db")
	if err != nil {
		log.Fatalf("failed to open report store: %v", err)
	}
	log.Fatal(srv.Start(":8081"))
}
