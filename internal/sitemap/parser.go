// backend/internal/sitemap/parser.go
package sitemap

import (
	"regexp"
	"strings"

	"github.com/PuerkitoBio/goquery"
)

func (sf *Fetcher) parseSitemap(text string) ([]string, error) {
	var urls []string

	// Check if sitemap index
	if strings.Contains(text, "<sitemapindex") {
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
