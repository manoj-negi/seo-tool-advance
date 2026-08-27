package controllers

import (
	"embed"
	"html/template"
	"seo-crawler/internal/crawler"
	"seo-crawler/internal/models"
	"seo-crawler/internal/store"
	"sync"
)

type Controller struct {
	CrawlerCfg    crawler.Config
	Jobs          map[string]*models.Job
	JobsMu        sync.RWMutex
	Store         *store.Store
	PasetoKey     []byte
	SecureCookies bool

	HomeTpl      *template.Template
	SeoReportTpl *template.Template
	ReportsTpl   *template.Template
}

// NewController wires up the Controller. secureCookies should be true in any
// deployment served over HTTPS — it marks the auth cookies Secure so they're
// never sent over a plaintext connection.
func NewController(st *store.Store, pKey []byte, viewsFS embed.FS, secureCookies bool) *Controller {
	mustParsePage := func(contentFile string) *template.Template {
		return template.Must(template.ParseFS(viewsFS,
			"views/layout.html", "views/header.html", "views/footer.html", "views/"+contentFile))
	}

	return &Controller{
		CrawlerCfg:    crawler.DefaultConfig(),
		Jobs:          make(map[string]*models.Job),
		Store:         st,
		PasetoKey:     pKey,
		SecureCookies: secureCookies,
		HomeTpl:       mustParsePage("index_content.html"),
		SeoReportTpl:  mustParsePage("analyzer_content.html"),
		ReportsTpl:    mustParsePage("reports_content.html"),
	}
}
