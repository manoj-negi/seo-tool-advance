package controllers

import (
	"embed"
	"html/template"
	"seo-crawler/internal/crawler"
	"seo-crawler/internal/mailer"
	"seo-crawler/internal/models"
	"seo-crawler/internal/render"
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
	Renderer      *render.Renderer
	Mailer        *mailer.Mailer

	HomeTpl      *template.Template
	SeoReportTpl *template.Template
	ReportsTpl   *template.Template
}

// NewController wires up the Controller. secureCookies should be true in any
// deployment served over HTTPS — it marks the auth cookies Secure so they're
// never sent over a plaintext connection. renderer is the shared headless-
// Chrome instance used as a fallback for JS-rendered pages and for PDF
// export/email; it may be nil to disable that fallback entirely. mail may
// be nil to disable welcome/report emails entirely (e.g. no SMTP configured).
func NewController(st *store.Store, pKey []byte, viewsFS embed.FS, secureCookies bool, renderer *render.Renderer, mail *mailer.Mailer) *Controller {
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
		Renderer:      renderer,
		Mailer:        mail,
		HomeTpl:       mustParsePage("index_content.html"),
		SeoReportTpl:  mustParsePage("analyzer_content.html"),
		ReportsTpl:    mustParsePage("reports_content.html"),
	}
}
