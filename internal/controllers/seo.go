package controllers

import (
	"context"
	"crypto/md5"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"seo-crawler/internal/crawler"
	"seo-crawler/internal/models"
	"seo-crawler/internal/netguard"
	"seo-crawler/internal/scorer"
	"seo-crawler/internal/sitemap"

	"github.com/o1egl/paseto/v2"
)

// absoluteMaxPages is a hard ceiling on max_pages regardless of auth state,
// so an authenticated (or otherwise bypassed) request can't kick off an
// unbounded crawl.
const absoluteMaxPages = 200

// randomJobSuffix returns a short random hex string appended to job IDs so
// they can't be guessed from a domain name and timestamp alone — otherwise
// anyone could construct another user's job ID and read their report via
// HandleStatus/HandleResults.
func randomJobSuffix() string {
	b := make([]byte, 6)
	if _, err := rand.Read(b); err != nil {
		return fmt.Sprintf("%d", time.Now().UnixNano())
	}
	return hex.EncodeToString(b)
}

// getUserID extracts the authenticated user's ID from the PASETO auth_token cookie.
// Returns an empty string if the user is not authenticated or the token is invalid.
func (c *Controller) getUserID(r *http.Request) string {
	cookie, err := r.Cookie("auth_token")
	if err != nil {
		return ""
	}
	v2 := paseto.NewV2()
	var claims map[string]interface{}
	if err := v2.Decrypt(cookie.Value, c.PasetoKey, &claims, nil); err != nil {
		return ""
	}
	if claimsExpired(claims) {
		return ""
	}
	if id, ok := claims["user_id"].(string); ok {
		return id
	}
	return ""
}

func (c *Controller) HandleAnalyse(w http.ResponseWriter, r *http.Request) {
	domain := r.URL.Query().Get("domain")
	if domain == "" {
		http.Error(w, `{"error":"domain required"}`, http.StatusBadRequest)
		return
	}

	maxPages := 50
	if mp := r.URL.Query().Get("max_pages"); mp != "" {
		fmt.Sscanf(mp, "%d", &maxPages)
	}
	if maxPages < 1 {
		maxPages = 1
	}
	if maxPages > absoluteMaxPages {
		maxPages = absoluteMaxPages
	}

	// Enforce auth for large crawls
	userID := c.getUserID(r)
	if maxPages > 25 && userID == "" {
		http.Error(w, `{"error":"login_required"}`, http.StatusUnauthorized)
		return
	}

	jobID := fmt.Sprintf("%s_%d_%s", domain, time.Now().Unix(), randomJobSuffix())
	job := &models.Job{
		ID:        jobID,
		UserID:    userID,
		Status:    "fetching_sitemap",
		Domain:    domain,
		CreatedAt: time.Now(),
	}

	c.JobsMu.Lock()
	c.Jobs[jobID] = job
	c.JobsMu.Unlock()

	go c.RunAnalysis(job, maxPages)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"job_id": jobID})
}

func (c *Controller) RunAnalysis(job *models.Job, maxPages int) {
	var urls []string
	var sitemapURL string
	var err error

	if maxPages == 1 {
		targetURL := job.Domain
		if !strings.HasPrefix(targetURL, "http") {
			targetURL = "https://" + targetURL
		}
		urls = []string{targetURL}
	} else {
		sf := sitemap.NewFetcher()
		urls, sitemapURL, err = sf.Discover(job.Domain)
		if err != nil {
			c.JobsMu.Lock()
			job.Status = "error"
			job.Error = err.Error()
			c.JobsMu.Unlock()
			return
		}
	}

	c.JobsMu.Lock()
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
	c.JobsMu.Unlock()

	cfg := c.CrawlerCfg
	cfg.MaxPages = maxPages
	cr := crawler.New(cfg)

	done := make(chan struct{})
	go func() {
		ticker := time.NewTicker(500 * time.Millisecond)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				c.JobsMu.Lock()
				job.Progress = int(cr.Progress())
				c.JobsMu.Unlock()
			case <-done:
				return
			}
		}
	}()

	stream := cr.AnalyzePagesStream(context.Background(), urls)

	for res := range stream {
		scorer.CalculateScore(&res)

		c.JobsMu.Lock()
		job.Results = append(job.Results, res)
		c.JobsMu.Unlock()
	}

	close(done)

	c.JobsMu.Lock()
	job.Progress = len(job.Results)
	job.Status = "checking_links"
	c.JobsMu.Unlock()

	// Post-crawl: broken link check + duplicate content detection (with 6s total timeout limit)
	linkCtx, linkCancel := context.WithTimeout(context.Background(), 6*time.Second)
	checkBrokenLinks(linkCtx, job)
	linkCancel()

	detectDuplicates(job)

	c.JobsMu.Lock()
	job.Progress = len(job.Results)
	job.Status = "complete"
	now := time.Now()
	job.CompletedAt = &now
	c.JobsMu.Unlock()

	if len(job.Results) > 0 {
		if err := c.Store.SaveReport(job); err != nil {
			log.Printf("failed to save report for job %s: %v", job.ID, err)
		}
	}
}

// checkBrokenLinks concurrently HEAD-checks all outgoing links on every page
// and records 4xx/5xx responses or timeouts as BrokenLink entries.
func checkBrokenLinks(ctx context.Context, job *models.Job) {
	httpClient := &http.Client{
		Timeout: 3 * time.Second,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) > 5 {
				return http.ErrUseLastResponse
			}
			return netguard.CheckURL(req.URL.String())
		},
	}

	sem := make(chan struct{}, 20) // max 20 concurrent link checks

	for i := range job.Results {
		res := &job.Results[i]
		if len(res.Links) == 0 {
			continue
		}

		// Resolve base for relative URLs
		base, _ := url.Parse(res.URL)

		var mu sync.Mutex
		var wg sync.WaitGroup

		for _, link := range res.Links {
			href := link.Href
			if href == "" || strings.HasPrefix(href, "#") ||
				strings.HasPrefix(href, "mailto:") || strings.HasPrefix(href, "tel:") {
				continue
			}

			// Resolve relative URLs
			if !strings.HasPrefix(href, "http") && base != nil {
				if ref, err := url.Parse(href); err == nil {
					href = base.ResolveReference(ref).String()
				}
			}
			if !strings.HasPrefix(href, "http") {
				continue
			}

			if err := netguard.CheckURL(href); err != nil {
				continue
			}

			wg.Add(1)
			go func(h string) {
				defer wg.Done()
				sem <- struct{}{}
				defer func() { <-sem }()

				req, err := http.NewRequestWithContext(ctx, http.MethodHead, h, nil)
				if err != nil {
					return
				}
				req.Header.Set("User-Agent", "SEOAnalyser/2.0")

				resp, err := httpClient.Do(req)
				if err != nil {
					mu.Lock()
					res.BrokenLinks = append(res.BrokenLinks, models.BrokenLink{
						Href:  h,
						Error: "timeout or connection error",
					})
					mu.Unlock()
					return
				}
				resp.Body.Close()
				if resp.StatusCode == 404 || resp.StatusCode >= 500 {
					mu.Lock()
					res.BrokenLinks = append(res.BrokenLinks, models.BrokenLink{
						Href:       h,
						StatusCode: resp.StatusCode,
					})
					mu.Unlock()
				}
			}(href)
		}
		wg.Wait()
	}
}

// detectDuplicates hashes each page's title+description and flags duplicates.
func detectDuplicates(job *models.Job) {
	seen := make(map[string]string) // hash -> first URL
	for i := range job.Results {
		res := &job.Results[i]
		key := strings.TrimSpace(res.Title) + "||" + strings.TrimSpace(res.MetaDescription)
		if key == "||" {
			continue // both empty — skip
		}
		hash := fmt.Sprintf("%x", md5.Sum([]byte(key)))
		if first, exists := seen[hash]; exists {
			res.DuplicateOf = first
		} else {
			seen[hash] = res.URL
		}
	}
}

// jobAccessAllowed reports whether requesterID may view a job/report owned
// by ownerID. Guest jobs (ownerID == "") stay open to anyone with the job
// ID, matching the pre-existing no-login crawl flow; an owned job is only
// visible to the matching authenticated user.
func jobAccessAllowed(requesterID, ownerID string) bool {
	return ownerID == "" || requesterID == ownerID
}

func (c *Controller) HandleStatus(w http.ResponseWriter, r *http.Request) {
	jobID := r.URL.Query().Get("job_id")
	c.JobsMu.RLock()
	job, exists := c.Jobs[jobID]

	if !exists {
		c.JobsMu.RUnlock()
		http.Error(w, `{"error":"job not found"}`, http.StatusNotFound)
		return
	}
	if !jobAccessAllowed(c.getUserID(r), job.UserID) {
		c.JobsMu.RUnlock()
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
	c.JobsMu.RUnlock()

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

func (c *Controller) HandleResults(w http.ResponseWriter, r *http.Request) {
	jobID := r.URL.Query().Get("job_id")
	requesterID := c.getUserID(r)

	c.JobsMu.RLock()
	job, exists := c.Jobs[jobID]

	if exists {
		if !jobAccessAllowed(requesterID, job.UserID) {
			c.JobsMu.RUnlock()
			http.Error(w, `{"error":"job not found"}`, http.StatusNotFound)
			return
		}
		resultsCopy := make([]models.SEOResult, len(job.Results))
		copy(resultsCopy, job.Results)
		status := job.Status
		c.JobsMu.RUnlock()

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"results": resultsCopy,
			"status":  status,
		})
		return
	}
	c.JobsMu.RUnlock()

	results, ownerID, found, err := c.Store.GetResults(jobID)
	if err != nil {
		log.Printf("failed to load report %s: %v", jobID, err)
		http.Error(w, `{"error":"failed to load report"}`, http.StatusInternalServerError)
		return
	}
	if !found {
		http.Error(w, `{"error":"job not found"}`, http.StatusNotFound)
		return
	}
	if !jobAccessAllowed(requesterID, ownerID) {
		http.Error(w, `{"error":"job not found"}`, http.StatusNotFound)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"results": results,
		"status":  "complete",
	})
}

func (c *Controller) HandleReportsList(w http.ResponseWriter, r *http.Request) {
	userID := c.getUserID(r)
	reports, err := c.Store.ListReports(userID)
	if err != nil {
		log.Printf("failed to list reports: %v", err)
		http.Error(w, `{"error":"failed to list reports"}`, http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"reports":      reports,
		"is_logged_in": userID != "",
	})
}
