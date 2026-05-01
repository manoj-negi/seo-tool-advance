// backend/internal/server/handlers.go
package server

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"seo-crawler/internal/crawler"
	"seo-crawler/internal/models"
	"seo-crawler/internal/scorer"
	"seo-crawler/internal/sitemap"
	"sync"
	"time"
)

type Server struct {
	crawlerCfg crawler.Config
	jobs       map[string]*models.Job
	jobsMu     sync.RWMutex
}

func New() *Server {
	return &Server{
		crawlerCfg: crawler.DefaultConfig(),
		jobs:       make(map[string]*models.Job),
	}
}

func (s *Server) Start(addr string) error {
	s.setupRoutes()
	return http.ListenAndServe(addr, nil)
}

func (s *Server) handleIndex(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html")
	w.Write([]byte(`<!DOCTYPE html>
<html><head><title>SEO Analyser Go</title></head>
<body>
<h1>SEO Analyser — Go Crawler Backend</h1>
<p>API Endpoints:</p>
<ul>
<li>GET /api/analyse?domain=example.com&max_pages=50</li>
<li>GET /api/status?job_id=xxx</li>
<li>GET /api/results?job_id=xxx</li>
</ul>
</body></html>`))
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
	ctx := context.Background()

	sf := sitemap.NewFetcher()
	urls, sitemapURL, err := sf.Discover(job.Domain)
	if err != nil {
		job.Status = "error"
		job.Error = err.Error()
		return
	}

	job.SitemapURL = sitemapURL
	if len(urls) == 0 {
		urls = []string{"https://" + job.Domain}
	}
	if len(urls) > maxPages {
		urls = urls[:maxPages]
	}
	job.URLs = urls
	job.Total = len(urls)
	job.Status = "analysing"

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
				job.Progress = int(c.Progress())
			case <-done:
				return
			}
		}
	}()

	results := c.AnalyzePages(ctx, urls)
	close(done)

	job.Results = results
	job.Progress = len(results)
	job.Status = "complete"
	now := time.Now()
	job.CompletedAt = &now
}

func (s *Server) handleStatus(w http.ResponseWriter, r *http.Request) {
	jobID := r.URL.Query().Get("job_id")

	s.jobsMu.RLock()
	job, exists := s.jobs[jobID]
	s.jobsMu.RUnlock()

	if !exists {
		http.Error(w, `{"error":"job not found"}`, http.StatusNotFound)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"job_id":        job.ID,
		"status":        job.Status,
		"domain":        job.Domain,
		"progress":      job.Progress,
		"total":         job.Total,
		"sitemap_url":   job.SitemapURL,
		"error":         job.Error,
		"results_count": len(job.Results),
	})
}

func (s *Server) handleResults(w http.ResponseWriter, r *http.Request) {
	jobID := r.URL.Query().Get("job_id")

	s.jobsMu.RLock()
	job, exists := s.jobs[jobID]
	s.jobsMu.RUnlock()

	if !exists {
		http.Error(w, `{"error":"job not found"}`, http.StatusNotFound)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"results": job.Results,
		"status":  job.Status,
	})
}

// handleStream is the NEW SSE endpoint for real-time results
func (s *Server) handleStream(w http.ResponseWriter, r *http.Request) {
	domain := r.URL.Query().Get("domain")
	if domain == "" {
		http.Error(w, `{"error":"domain required"}`, http.StatusBadRequest)
		return
	}

	maxPages := 50
	if mp := r.URL.Query().Get("max_pages"); mp != "" {
		fmt.Sscanf(mp, "%d", &maxPages)
	}

	// Set SSE headers
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("Access-Control-Allow-Origin", "*")

	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "Streaming not supported", http.StatusInternalServerError)
		return
	}

	// Send initial event
	sendSSE(w, flusher, "start", map[string]interface{}{
		"domain":    domain,
		"max_pages": maxPages,
		"message":   "Starting analysis...",
	})

	// Discover sitemap
	sf := sitemap.NewFetcher()
	urls, sitemapURL, err := sf.Discover(domain)
	if err != nil {
		sendSSE(w, flusher, "error", map[string]string{"error": err.Error()})
		return
	}

	sendSSE(w, flusher, "sitemap", map[string]interface{}{
		"url":         sitemapURL,
		"pages_found": len(urls),
	})

	if len(urls) == 0 {
		urls = []string{"https://" + domain}
	}
	if len(urls) > maxPages {
		urls = urls[:maxPages]
	}

	// Start streaming crawl
	cfg := s.crawlerCfg
	cfg.MaxPages = maxPages
	c := crawler.New(cfg)

	ctx, cancel := context.WithCancel(r.Context())
	defer cancel()

	resultCh := c.AnalyzePagesStream(ctx, urls)

	completed := 0
	total := len(urls)

	// Stream each result as it completes
	for result := range resultCh {
		completed++

		// Apply scoring
		scorer.CalculateScore(&result)

		sendSSE(w, flusher, "result", map[string]interface{}{
			"completed": completed,
			"total":     total,
			"progress":  float64(completed) / float64(total) * 100,
			"data":      result,
		})
	}

	// Send completion event
	sendSSE(w, flusher, "complete", map[string]interface{}{
		"completed": completed,
		"total":     total,
		"message":   fmt.Sprintf("Analysis complete — %d pages analyzed", completed),
	})
}

// sendSSE sends a Server-Sent Event
func sendSSE(w http.ResponseWriter, flusher http.Flusher, event string, data interface{}) {
	jsonData, err := json.Marshal(data)
	if err != nil {
		return
	}

	fmt.Fprintf(w, "event: %s\n", event)
	fmt.Fprintf(w, "data: %s\n\n", jsonData)
	flusher.Flush()
}
