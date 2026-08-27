package crawler

import (
	"context"
	"crypto/tls"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"sync"
	"time"

	"seo-crawler/internal/netguard"
)

type Fetcher struct {
	client     *http.Client
	config     Config
	hostDelays map[string]time.Time
	delayMu    sync.RWMutex
}

func NewFetcher(cfg Config) *Fetcher {
	transport := &http.Transport{
		MaxIdleConns:        100,
		MaxIdleConnsPerHost: 20,
		MaxConnsPerHost:     50,
		IdleConnTimeout:     90 * time.Second,
		TLSHandshakeTimeout: 10 * time.Second,
		DisableCompression:  false,
		ForceAttemptHTTP2:   false, // Disabled to bypass HTTP/2 WAF fingerprinting
		TLSClientConfig:     &tls.Config{MinVersion: tls.VersionTLS12},
	}

	jar, _ := cookiejar.New(nil)

	return &Fetcher{
		client: &http.Client{
			Transport:     transport,
			Timeout:       cfg.RequestTimeout,
			Jar:           jar, // Maintain cookies across requests
			CheckRedirect: netguard.CheckRedirect,
		},
		config:     cfg,
		hostDelays: make(map[string]time.Time),
	}
}

// Fetch returns *FetchResult (single return value)
func (f *Fetcher) Fetch(ctx context.Context, rawURL string) *FetchResult {
	result := &FetchResult{}

	if err := netguard.CheckURL(rawURL); err != nil {
		result.Error = err
		return result
	}

	f.waitForHost(rawURL)

	var lastErr error
	for attempt := 0; attempt <= f.config.MaxRetries; attempt++ {
		if attempt > 0 {
			backoff := f.config.RetryDelay * time.Duration(1<<uint(attempt-1))
			select {
			case <-time.After(backoff):
			case <-ctx.Done():
				result.Error = ctx.Err()
				return result
			}
		}

		req, err := http.NewRequestWithContext(ctx, "GET", rawURL, nil)
		if err != nil {
			lastErr = err
			continue
		}

		req.Header.Set("User-Agent", f.config.UserAgent)
		req.Header.Set("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8")
		req.Header.Set("Accept-Language", "en-US,en;q=0.5")
		req.Header.Set("Connection", "keep-alive")

		start := time.Now()
		resp, err := f.client.Do(req)
		if err != nil {
			lastErr = err
			continue
		}

		result.LoadTimeMs = time.Since(start).Milliseconds()
		result.StatusCode = resp.StatusCode
		result.Headers = resp.Header
		result.ContentType = resp.Header.Get("Content-Type")

		// Read with 10MB limit
		body, err := io.ReadAll(io.LimitReader(resp.Body, 10*1024*1024))
		resp.Body.Close()
		if err != nil {
			lastErr = err
			continue
		}

		result.Content = body
		result.Error = nil
		return result // Success
	}

	result.Error = lastErr
	return result
}

// waitForHost enforces PoliteDelay spacing between consecutive requests to
// the same host. Each caller reserves its slot (a strictly increasing
// "earliest start time" per host) under the lock, then sleeps outside the
// lock — so requests to different hosts never block on each other, and
// concurrent requests to the same host still queue up correctly.
func (f *Fetcher) waitForHost(rawURL string) {
	u, err := url.Parse(rawURL)
	if err != nil {
		return
	}
	host := u.Host

	f.delayMu.Lock()
	now := time.Now()
	start := now
	if next, exists := f.hostDelays[host]; exists && next.After(start) {
		start = next
	}
	f.hostDelays[host] = start.Add(f.config.PoliteDelay)
	f.delayMu.Unlock()

	if wait := start.Sub(now); wait > 0 {
		time.Sleep(wait)
	}
}
