package crawler

import (
	"context"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"seo-crawler/internal/models"
	"seo-crawler/internal/netguard"
	"seo-crawler/internal/render"
)

// renderTimeout bounds a single headless-Chrome render, which is far slower
// than a plain HTTP fetch — this must stay generous but finite so one
// JS-heavy page can't stall a whole crawl.
const renderTimeout = 12 * time.Second

type Crawler struct {
	config    Config
	fetcher   *Fetcher
	parser    *Parser
	renderer  *render.Renderer // optional; nil disables the JS-render fallback
	semaphore chan struct{}
	progress  int32
}

// New builds a Crawler. renderer may be nil, in which case pages that look
// like empty client-rendered shells are left as-is instead of being
// re-fetched through a headless browser.
func New(cfg Config, renderer *render.Renderer) *Crawler {
	return &Crawler{
		config:    cfg,
		fetcher:   NewFetcher(cfg),
		parser:    NewParser(),
		renderer:  renderer,
		semaphore: make(chan struct{}, cfg.MaxWorkers),
	}
}

// AnalyzePagesStream crawls URLs and streams results through a channel.
// It fetches robots.txt once per domain and skips disallowed URLs.
func (c *Crawler) AnalyzePagesStream(ctx context.Context, urls []string) <-chan models.SEOResult {
	if len(urls) > c.config.MaxPages {
		urls = urls[:c.config.MaxPages]
	}

	// Fetch robots.txt once using the first URL's base
	var robots *RobotsData
	if len(urls) > 0 {
		baseURL := urls[0]
		if u, err := url.Parse(baseURL); err == nil {
			base := u.Scheme + "://" + u.Host
			robots = FetchRobots(ctx, base, c.config.UserAgent)
		}
	}
	if robots == nil {
		robots = &RobotsData{crawlable: true}
	}

	resultCh := make(chan models.SEOResult, len(urls))

	go func() {
		defer close(resultCh)

		var wg sync.WaitGroup

		for _, rawURL := range urls {
			select {
			case <-ctx.Done():
				return
			default:
			}

			// Check robots.txt before crawling
			if u, err := url.Parse(rawURL); err == nil && !robots.IsAllowed(u.RequestURI()) {
				resultCh <- models.SEOResult{
					URL:           rawURL,
					RobotsBlocked: true,
					Error:         "blocked by robots.txt",
				}
				atomic.AddInt32(&c.progress, 1)
				continue
			}

			wg.Add(1)
			go func(u string) {
				defer wg.Done()

				select {
				case c.semaphore <- struct{}{}:
					defer func() { <-c.semaphore }()
				case <-ctx.Done():
					return
				}

				result := c.analyzeURL(ctx, u)
				atomic.AddInt32(&c.progress, 1)

				select {
				case resultCh <- result:
				case <-ctx.Done():
					return
				}
			}(rawURL)
		}

		wg.Wait()
	}()

	return resultCh
}

// AnalyzePages (batch method for compatibility)
func (c *Crawler) AnalyzePages(ctx context.Context, urls []string) []models.SEOResult {
	var results []models.SEOResult
	for r := range c.AnalyzePagesStream(ctx, urls) {
		results = append(results, r)
	}
	return results
}

func (c *Crawler) analyzeURL(ctx context.Context, rawURL string) models.SEOResult {
	if !strings.HasPrefix(rawURL, "http") {
		rawURL = "https://" + rawURL
	}

	// Fetch the page
	fetchResult := c.fetcher.Fetch(ctx, rawURL)

	// Parse HTML using your Parser struct
	result := c.parser.Parse(string(fetchResult.Content), rawURL, fetchResult)

	// Fallback: if this looks like an empty client-rendered shell (React/
	// Vue/Next.js etc.), re-fetch it through headless Chrome and re-parse
	// the fully rendered HTML. Only pages that need it pay this cost —
	// everything else stays on the fast plain-HTTP path above.
	if c.renderer != nil && looksLikeEmptyJSShell(result.Title, result.WordCount, string(fetchResult.Content)) {
		if err := netguard.CheckURL(rawURL); err == nil {
			// RenderWithMetrics captures real Core Web Vitals (LCP, CLS)
			// from the browser's own Performance API during this same
			// render — since we're already paying for a real Chrome tab
			// here, actual measurements cost nothing extra, unlike running
			// this for every page in the crawl.
			if html, metrics, err := c.renderer.RenderWithMetrics(rawURL, renderTimeout); err == nil {
				rendered := c.parser.Parse(html, rawURL, fetchResult)
				rendered.RenderedWithJS = true
				rendered.CWVMeasured = true
				rendered.LCPMs = metrics.LCPMs
				rendered.CLS = metrics.CLS
				result = rendered
			}
		}
	}

	// Apply scoring
	// Note: scorer is called in server layer, not here

	return *result
}

func (c *Crawler) Progress() int32 {
	return atomic.LoadInt32(&c.progress)
}
