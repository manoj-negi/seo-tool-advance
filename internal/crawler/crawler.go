package crawler

import (
	"context"
	"strings"
	"sync"
	"sync/atomic"

	"seo-crawler/internal/models"
)

type Crawler struct {
	config    Config
	fetcher   *Fetcher
	parser    *Parser
	semaphore chan struct{}
	progress  int32
}

func New(cfg Config) *Crawler {
	return &Crawler{
		config:    cfg,
		fetcher:   NewFetcher(cfg),
		parser:    NewParser(),
		semaphore: make(chan struct{}, cfg.MaxWorkers),
	}
}

// AnalyzePagesStream crawls URLs and streams results through a channel
func (c *Crawler) AnalyzePagesStream(ctx context.Context, urls []string) <-chan models.SEOResult {
	if len(urls) > c.config.MaxPages {
		urls = urls[:c.config.MaxPages]
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

	// Apply scoring
	// Note: scorer is called in server layer, not here

	return *result
}

func (c *Crawler) Progress() int32 {
	return atomic.LoadInt32(&c.progress)
}
