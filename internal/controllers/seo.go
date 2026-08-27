package controllers

import (
	"context"
	"crypto/md5"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"seo-crawler/internal/crawler"
	"seo-crawler/internal/mailer"
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

// StartJobJanitor periodically evicts finished jobs (status "complete" or
// "error") older than maxAge from the in-memory Jobs map. Completed jobs
// with results are already persisted to MongoDB via Store.SaveReport, so
// evicting them here just frees memory — HandleStatus/HandleResults
// transparently fall back to the stored report once a job is no longer in
// memory. Without this, Jobs grows forever on a long-running process, since
// nothing else ever removes an entry. Stops when ctx is cancelled, so the
// caller can tie its lifetime to server shutdown.
func (c *Controller) StartJobJanitor(ctx context.Context, interval, maxAge time.Duration) {
	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				c.evictFinishedJobs(maxAge)
			}
		}
	}()
}

func (c *Controller) evictFinishedJobs(maxAge time.Duration) {
	cutoff := time.Now().Add(-maxAge)

	c.JobsMu.Lock()
	defer c.JobsMu.Unlock()
	for id, job := range c.Jobs {
		if job.Status != "complete" && job.Status != "error" {
			continue // still running — never evict an in-progress job
		}
		finishedAt := job.CreatedAt
		if job.CompletedAt != nil {
			finishedAt = *job.CompletedAt
		}
		if finishedAt.Before(cutoff) {
			delete(c.Jobs, id)
		}
	}
}

// getUserID extracts the authenticated user's ID from the PASETO auth_token cookie.
// Returns an empty string if the user is not authenticated or the token is invalid.
func (c *Controller) getUserID(r *http.Request) string {
	id, _ := c.getUserIDAndEmail(r)
	return id
}

// getUserIDAndEmail decrypts the PASETO auth_token cookie and returns both
// the user ID and email from its claims, or two empty strings if the
// requester isn't authenticated. Kept as one function (rather than two
// separate cookie reads) since HandleAnalyse needs the email to email the
// finished report later, from RunAnalysis's background goroutine which has
// no HTTP request to re-derive it from.
func (c *Controller) getUserIDAndEmail(r *http.Request) (userID, email string) {
	cookie, err := r.Cookie("auth_token")
	if err != nil {
		return "", ""
	}
	v2 := paseto.NewV2()
	var claims map[string]interface{}
	if err := v2.Decrypt(cookie.Value, c.PasetoKey, &claims, nil); err != nil {
		return "", ""
	}
	if claimsExpired(claims) {
		return "", ""
	}
	id, _ := claims["user_id"].(string)
	em, _ := claims["email"].(string)
	return id, em
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
	userID, userEmail := c.getUserIDAndEmail(r)
	if maxPages > 25 && userID == "" {
		http.Error(w, `{"error":"login_required"}`, http.StatusUnauthorized)
		return
	}

	jobID := fmt.Sprintf("%s_%d_%s", domain, time.Now().Unix(), randomJobSuffix())
	job := &models.Job{
		ID:        jobID,
		UserID:    userID,
		UserEmail: userEmail,
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
			now := time.Now()
			c.JobsMu.Lock()
			job.Status = "error"
			job.Error = err.Error()
			job.CompletedAt = &now
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
	cr := crawler.New(cfg, c.Renderer)

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

	// Post-crawl: broken link check + duplicate content detection.
	// The budget scales with page count — a fixed small budget on a large
	// crawl runs out before most links are even checked, and every
	// still-in-flight check at that point would otherwise get misreported
	// as broken (see checkBrokenLinks' ctx.Err() guard) rather than simply
	// not being checked.
	linkCtx, linkCancel := context.WithTimeout(context.Background(), linkCheckBudget(len(job.Results)))
	checkBrokenLinks(linkCtx, job)
	linkCancel()

	detectDuplicates(job)

	c.JobsMu.Lock()
	// Aggregates broken links, duplicates, thin content, the internal link
	// graph, and hreflang reciprocity into one site-wide summary. Held
	// under the write lock since it also sets per-page InternalInlinks/
	// IsOrphan fields that HandleResults reads concurrently.
	job.Summary = buildSiteSummary(job)
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

	// Email the finished report to whoever kicked off the crawl, if they
	// were logged in and SMTP is configured. Best-effort and asynchronous —
	// a slow or failed send must never hold up (or fail) job completion.
	if job.UserEmail != "" && c.Mailer != nil && len(job.Results) > 0 {
		go c.emailFinishedReport(job)
	}
}

// emailFinishedReport renders job to PDF and emails it to job.UserEmail.
// Errors are logged, not surfaced anywhere else — by the time this runs,
// the crawl itself has already succeeded and been saved.
func (c *Controller) emailFinishedReport(job *models.Job) {
	pdfData, err := c.renderJobPDF(job)
	if err != nil {
		log.Printf("email report: render pdf for job %s: %v", job.ID, err)
		return
	}

	attachment := mailer.Attachment{
		Filename: fmt.Sprintf("seo-report-%s.pdf", sanitizeFilename(job.Domain)),
		MIMEType: "application/pdf",
		Data:     pdfData,
	}
	subject := fmt.Sprintf("Your SEO report for %s is ready", job.Domain)
	if err := c.Mailer.SendWithAttachment(job.UserEmail, subject, reportEmailHTML(job), attachment); err != nil {
		log.Printf("failed to email report for job %s to %s: %v", job.ID, job.UserEmail, err)
	}
}

// linkCheckBudget scales the broken-link check's total time budget with
// crawl size, capped to keep a single huge crawl from stalling completion
// for too long. A fixed small budget regardless of page count was the root
// cause of large crawls reporting most links as "broken" simply because the
// checker ran out of time before reaching them.
func linkCheckBudget(pageCount int) time.Duration {
	budget := time.Duration(pageCount) * 4 * time.Second
	if budget < 6*time.Second {
		budget = 6 * time.Second
	}
	if budget > 60*time.Second {
		budget = 60 * time.Second
	}
	return budget
}

// checkBrokenLinks concurrently checks all outgoing links on every page and
// records genuinely broken ones (4xx/5xx confirmed via GET, or a real
// connection failure) as BrokenLink entries.
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

				if bl := checkLinkBroken(ctx, httpClient, h); bl != nil {
					mu.Lock()
					res.BrokenLinks = append(res.BrokenLinks, *bl)
					mu.Unlock()
				}
			}(href)
		}
		wg.Wait()
	}
}

// checkLinkBroken checks a single URL with HEAD, and returns a BrokenLink
// describing why it's broken — or nil if it's fine. Separated out from
// checkBrokenLinks so it can be unit-tested directly against a local test
// server (checkBrokenLinks itself pre-filters every URL through netguard,
// which would reject a loopback test server before this logic ever ran).
func checkLinkBroken(ctx context.Context, client *http.Client, href string) *models.BrokenLink {
	req, err := http.NewRequestWithContext(ctx, http.MethodHead, href, nil)
	if err != nil {
		return nil
	}
	req.Header.Set("User-Agent", "SEOAnalyser/2.0")

	resp, err := client.Do(req)
	if err != nil {
		// If our own overall link-check budget already ran out, this
		// failure means "we didn't get to finish checking this link," not
		// "this link is broken" — reporting it as broken would be a false
		// positive. Skip silently.
		if ctx.Err() != nil {
			return nil
		}
		return &models.BrokenLink{Href: href, Error: "timeout or connection error"}
	}
	io.Copy(io.Discard, io.LimitReader(resp.Body, 1024))
	resp.Body.Close()

	if resp.StatusCode != 404 && resp.StatusCode < 500 {
		return nil
	}

	// Some servers don't implement HEAD correctly (or block it outright)
	// and return a 404/5xx even though the same URL works fine over GET —
	// which is what actually happens when a person clicks the link in a
	// browser. Confirm with a GET before reporting it as broken, so we
	// don't flag links that are only "broken" for HEAD requests.
	statusCode := resp.StatusCode
	getReq, err := http.NewRequestWithContext(ctx, http.MethodGet, href, nil)
	if err == nil {
		getReq.Header.Set("User-Agent", "SEOAnalyser/2.0")
		if getResp, err := client.Do(getReq); err == nil {
			io.Copy(io.Discard, io.LimitReader(getResp.Body, 1024))
			getResp.Body.Close()
			if getResp.StatusCode != 404 && getResp.StatusCode < 500 {
				return nil // GET succeeds — the link isn't actually broken
			}
			statusCode = getResp.StatusCode
		} else if ctx.Err() != nil {
			return nil // budget ran out mid-retry — don't misreport
		}
	}

	return &models.BrokenLink{Href: href, StatusCode: statusCode}
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
		summary := job.Summary
		c.JobsMu.RUnlock()

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"results": resultsCopy,
			"status":  status,
			"summary": summary,
		})
		return
	}
	c.JobsMu.RUnlock()

	results, summary, ownerID, found, err := c.Store.GetResults(jobID)
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
		"summary": summary,
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

// HandleScoreTrend returns a domain's average-score history for the
// logged-in user, so repeated crawls of the same domain can be charted as a
// trend rather than viewed as disconnected one-off reports.
func (c *Controller) HandleScoreTrend(w http.ResponseWriter, r *http.Request) {
	domain := r.URL.Query().Get("domain")
	if domain == "" {
		http.Error(w, `{"error":"domain required"}`, http.StatusBadRequest)
		return
	}

	userID := c.getUserID(r)
	if userID == "" {
		http.Error(w, `{"error":"login_required"}`, http.StatusUnauthorized)
		return
	}

	points, err := c.Store.GetScoreTrend(userID, domain)
	if err != nil {
		log.Printf("failed to get score trend for %s: %v", domain, err)
		http.Error(w, `{"error":"failed to load trend"}`, http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"domain": domain,
		"points": points,
	})
}
