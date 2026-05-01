package sitemap

import (
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/PuerkitoBio/goquery"
)

// Fetcher handles sitemap.xml discovery
type Fetcher struct {
	client *http.Client
}

func NewFetcher() *Fetcher {
	return &Fetcher{
		client: &http.Client{Timeout: 10 * time.Second},
	}
}

func (sf *Fetcher) Discover(baseURL string) ([]string, string, error) {
	baseURL = strings.TrimRight(baseURL, "/")
	if !strings.HasPrefix(baseURL, "http") {
		baseURL = "https://" + baseURL
	}

	candidates := []string{
		baseURL + "/sitemap.xml",
		baseURL + "/sitemap_index.xml",
		baseURL + "/sitemap-index.xml",
		baseURL + "/sitemaps.xml",
		baseURL + "/sitemap/sitemap.xml",
	}

	// Check robots.txt first
	robotsURL := baseURL + "/robots.txt"
	if resp, err := sf.client.Get(robotsURL); err == nil {
		if body, err := io.ReadAll(resp.Body); err == nil {
			resp.Body.Close()
			lines := strings.Split(string(body), "\n")
			for _, line := range lines {
				line = strings.TrimSpace(line)
				if strings.HasPrefix(strings.ToLower(line), "sitemap:") {
					parts := strings.SplitN(line, ":", 2)
					if len(parts) == 2 {
						sm := strings.TrimSpace(parts[1])
						// Insert at front
						candidates = append([]string{sm}, candidates...)
					}
				}
			}
		}
	}

	for _, smURL := range candidates {
		urls, err := sf.fetchSitemap(smURL)
		if err == nil && len(urls) > 0 {
			return urls, smURL, nil
		}
	}

	// Fallback: just homepage
	return []string{baseURL}, "", nil
}

func (sf *Fetcher) fetchSitemap(smURL string) ([]string, error) {
	resp, err := sf.client.Get(smURL)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	text := string(body)
	var urls []string

	// Check if sitemap index
	if strings.Contains(text, "<sitemapindex") {
		// Parse sitemapindex and fetch child sitemaps
		doc, err := goquery.NewDocumentFromReader(strings.NewReader(text))
		if err != nil {
			return nil, err
		}
		doc.Find("sitemap loc").Each(func(i int, s *goquery.Selection) {
			if childURL := strings.TrimSpace(s.Text()); childURL != "" {
				if childURLs, err := sf.fetchSitemap(childURL); err == nil {
					urls = append(urls, childURLs...)
				}
			}
		})
	} else {
		// Parse urlset
		doc, err := goquery.NewDocumentFromReader(strings.NewReader(text))
		if err != nil {
			// Fallback to regex
			re := regexp.MustCompile(`<loc>([^<]+)</loc>`)
			matches := re.FindAllStringSubmatch(text, -1)
			for _, match := range matches {
				if len(match) > 1 {
					urls = append(urls, strings.TrimSpace(match[1]))
				}
			}
		} else {
			doc.Find("url loc").Each(func(i int, s *goquery.Selection) {
				if u := strings.TrimSpace(s.Text()); u != "" {
					urls = append(urls, u)
				}
			})
		}
	}

	return urls, nil
}
