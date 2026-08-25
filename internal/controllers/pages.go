package controllers

import (
	"net/http"
	"time"
)

// PageData is the data every page template is rendered with. ActivePath
// drives the header nav's active-link styling; Year feeds the footer.
type PageData struct {
	Title       string
	Description string
	ActivePath  string
	Year        int
}

func (c *Controller) HandleHomepage(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	c.HomeTpl.ExecuteTemplate(w, "layout", PageData{
		Title:       "Auditly · Free SEO Audit Tool",
		Description: "Crawl your sitemap, score every page, and get actionable SEO fixes for titles, meta tags, headings, images, speed, links, Open Graph and structured data — free.",
		ActivePath:  "/",
		Year:        time.Now().Year(),
	})
}

func (c *Controller) HandleSeoReport(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	c.SeoReportTpl.ExecuteTemplate(w, "layout", PageData{
		Title:      "Auditly · SEO Report",
		ActivePath: "/seo-report",
		Year:       time.Now().Year(),
	})
}

func (c *Controller) HandleReports(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	c.ReportsTpl.ExecuteTemplate(w, "layout", PageData{
		Title:      "Auditly · Saved Reports",
		ActivePath: "/reports",
		Year:       time.Now().Year(),
	})
}
