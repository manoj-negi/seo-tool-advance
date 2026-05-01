// backend/cmd/server/main.go — PURE GO VERSION
package main

import (
	"context"
	_ "embed"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"seo-crawler/internal/crawler"
	"seo-crawler/internal/models"
	"seo-crawler/internal/scorer"
	"seo-crawler/internal/sitemap"
	"strings"
	"sync"
	"time"
)

//go:embed index.html
var indexHTML string

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
	mux := http.NewServeMux()

	// Serve embedded UI
	mux.HandleFunc("/", s.handleIndex)
	mux.HandleFunc("/api/analyse", cors(s.handleAnalyse))
	mux.HandleFunc("/api/status", cors(s.handleStatus))
	mux.HandleFunc("/api/results", cors(s.handleResults))

	// Optional: serve static assets
	//mux.Handle("/static/", http.FileServer(http.FS(staticFiles)))

	log.Printf("SEO Analyser running at http://localhost%s", addr)
	return http.ListenAndServe(addr, mux)
}

func (s *Server) handleIndex(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Write([]byte(indexHTML))
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

	if !exists {
		s.jobsMu.RUnlock()
		http.Error(w, `{"error":"job not found"}`, http.StatusNotFound)
		return
	}

	resultsCopy := make([]models.SEOResult, len(job.Results))
	copy(resultsCopy, job.Results)
	status := job.Status
	s.jobsMu.RUnlock()

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"results": resultsCopy,
		"status":  status,
	})
}

func main() {
	srv := New()
	log.Fatal(srv.Start(":8080"))
}
