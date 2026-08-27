package controllers

import (
	"fmt"
	"log"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"seo-crawler/internal/models"
)

// pdfRenderTimeout bounds how long headless Chrome gets to load the report
// file and print it. This is a local file with no network calls, so it
// should be fast, but a very large crawl's report can still be a sizeable
// document to lay out and paginate.
const pdfRenderTimeout = 20 * time.Second

// HandleExportPDF renders a job's report to PDF via the shared headless-
// Chrome renderer and returns it as a downloadable file.
func (c *Controller) HandleExportPDF(w http.ResponseWriter, r *http.Request) {
	jobID := r.URL.Query().Get("job_id")
	requesterID := c.getUserID(r)

	job, ok := c.resolveJobForExport(jobID, requesterID)
	if !ok {
		http.Error(w, `{"error":"job not found"}`, http.StatusNotFound)
		return
	}

	pdfData, err := c.renderJobPDF(job)
	if err != nil {
		log.Printf("export pdf job %s: %v", jobID, err)
		if c.Renderer == nil {
			http.Error(w, `{"error":"PDF export is unavailable on this server"}`, http.StatusServiceUnavailable)
		} else {
			http.Error(w, `{"error":"failed to generate report"}`, http.StatusInternalServerError)
		}
		return
	}

	filename := fmt.Sprintf("seo-report-%s.pdf", sanitizeFilename(job.Domain))
	w.Header().Set("Content-Type", "application/pdf")
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s"`, filename))
	w.Write(pdfData)
}

// renderJobPDF builds the printable report HTML for job and renders it to
// PDF via the shared headless-Chrome renderer. Shared by HandleExportPDF
// and the automatic "email the finished report" path (seo.go) so both
// go through the exact same temp-file-plus-PrintPDF flow.
func (c *Controller) renderJobPDF(job *models.Job) ([]byte, error) {
	if c.Renderer == nil {
		return nil, fmt.Errorf("PDF rendering is unavailable (no renderer configured)")
	}

	tmpFile, err := os.CreateTemp("", "seo-report-*.html")
	if err != nil {
		return nil, fmt.Errorf("create temp file: %w", err)
	}
	tmpPath := tmpFile.Name()
	defer os.Remove(tmpPath)

	if _, err := tmpFile.WriteString(buildPrintableReportHTML(job)); err != nil {
		tmpFile.Close()
		return nil, fmt.Errorf("write temp file: %w", err)
	}
	tmpFile.Close()

	absPath, err := filepath.Abs(tmpPath)
	if err != nil {
		return nil, fmt.Errorf("resolve path: %w", err)
	}
	fileURL := (&url.URL{Scheme: "file", Path: absPath}).String()

	pdfData, err := c.Renderer.PrintPDF(fileURL, pdfRenderTimeout)
	if err != nil {
		return nil, fmt.Errorf("print pdf: %w", err)
	}
	return pdfData, nil
}

// resolveJobForExport finds a job (in-memory, falling back to the stored
// report) and enforces the same ownership rule as HandleResults: guest jobs
// are open to anyone with the ID, an owned job only to its owner.
func (c *Controller) resolveJobForExport(jobID, requesterID string) (*models.Job, bool) {
	c.JobsMu.RLock()
	job, exists := c.Jobs[jobID]
	if exists {
		if !jobAccessAllowed(requesterID, job.UserID) {
			c.JobsMu.RUnlock()
			return nil, false
		}
		jobCopy := *job
		jobCopy.Results = append([]models.SEOResult(nil), job.Results...)
		c.JobsMu.RUnlock()
		return &jobCopy, true
	}
	c.JobsMu.RUnlock()

	results, summary, ownerID, found, err := c.Store.GetResults(jobID)
	if err != nil || !found || !jobAccessAllowed(requesterID, ownerID) {
		return nil, false
	}

	return &models.Job{ID: jobID, Domain: domainFromJobID(jobID), Results: results, Summary: summary}, true
}

// domainFromJobID recovers the original domain from a job ID of the form
// "domain_<unix-timestamp>_<random-suffix>" (see HandleAnalyse) — used only
// to name the exported file, since the stored report itself doesn't carry
// the domain back through GetResults.
func domainFromJobID(jobID string) string {
	parts := strings.Split(jobID, "_")
	if len(parts) >= 3 {
		return strings.Join(parts[:len(parts)-2], "_")
	}
	return jobID
}

// sanitizeFilename keeps a Content-Disposition filename simple and safe —
// domains are the only user-influenced input here, but browsers and
// filesystems both dislike spaces/slashes/quotes in a filename.
func sanitizeFilename(s string) string {
	out := make([]rune, 0, len(s))
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '-', r == '.':
			out = append(out, r)
		default:
			out = append(out, '-')
		}
	}
	if len(out) == 0 {
		return "report"
	}
	return string(out)
}
