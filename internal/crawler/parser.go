package crawler

import (
	"encoding/json"
	"fmt"
	"net/url"
	"regexp"
	"strings"

	"seo-crawler/internal/models"

	"github.com/PuerkitoBio/goquery"
)

type Parser struct{}

func NewParser() *Parser {
	return &Parser{}
}

func (p *Parser) Parse(html string, pageURL string, fetchResult *FetchResult) *models.SEOResult {
	r := &models.SEOResult{
		URL:           pageURL,
		StatusCode:    fetchResult.StatusCode,
		LoadTimeMs:    fetchResult.LoadTimeMs,
		PageSizeBytes: len(fetchResult.Content),
		ContentType:   fetchResult.ContentType,
		Headings:      make(map[string][]string),
		OGTags:        make(map[string]string),
		HasHTTPS:      strings.HasPrefix(pageURL, "https://"),
	}

	// Security headers
	if fetchResult.Headers != nil {
		r.XFrameOptions = fetchResult.Headers.Get("X-Frame-Options")
		r.ContentSecurity = fetchResult.Headers.Get("Content-Security-Policy")
		r.StrictTransportSecurity = fetchResult.Headers.Get("Strict-Transport-Security")
		r.XContentTypeOptions = fetchResult.Headers.Get("X-Content-Type-Options")
		r.ReferrerPolicy = fetchResult.Headers.Get("Referrer-Policy")
		r.PermissionsPolicy = fetchResult.Headers.Get("Permissions-Policy")
	}

	// If the fetch itself failed (timeout, DNS failure, connection refused,
	// blocked by the SSRF guard, etc.), html is empty or meaningless —
	// goquery would happily "succeed" parsing an empty string into an empty
	// document, silently producing a page that just looks blank instead of
	// surfacing why. Report the real reason instead.
	if fetchResult.Error != nil {
		r.Error = fetchResult.Error.Error()
		return r
	}

	doc, err := goquery.NewDocumentFromReader(strings.NewReader(html))
	if err != nil {
		r.Error = "HTML parse error: " + err.Error()
		return r
	}

	p.parseTitle(doc, r)
	p.parseMetaTags(doc, r)
	p.parseCanonical(doc, r)
	p.parseHeadings(doc, r)
	p.parseImages(doc, r)
	p.parseLinks(doc, r, pageURL)
	p.parseWordCount(doc, r)
	p.parseSchema(doc, r)
	p.parseCWVHints(doc, r)
	p.parseHreflang(doc, r)

	return r
}

func (p *Parser) parseHreflang(doc *goquery.Document, r *models.SEOResult) {
	doc.Find(`link[rel="alternate"][hreflang]`).Each(func(i int, s *goquery.Selection) {
		lang, _ := s.Attr("hreflang")
		href, _ := s.Attr("href")
		if lang == "" || href == "" {
			return
		}
		r.Hreflang = append(r.Hreflang, models.HreflangTag{Lang: lang, Href: href})
	})
}

func (p *Parser) parseTitle(doc *goquery.Document, r *models.SEOResult) {
	titleText := doc.Find("head title").First().Text()
	if titleText == "" {
		titleText = doc.Find("title").First().Text()
	}
	r.Title = strings.Join(strings.Fields(titleText), " ")
	r.TitleLength = len([]rune(r.Title))
}

func (p *Parser) parseMetaTags(doc *goquery.Document, r *models.SEOResult) {
	doc.Find("meta").Each(func(i int, s *goquery.Selection) {
		name, _ := s.Attr("name")
		prop, _ := s.Attr("property")
		content, _ := s.Attr("content")
		httpEquiv, _ := s.Attr("http-equiv")

		switch strings.ToLower(name) {
		case "description":
			r.MetaDescription = content
			r.MetaDescriptionLength = len([]rune(content))
		case "keywords":
			r.MetaKeywords = content
		case "robots":
			r.RobotsMeta = content
		case "viewport":
			r.MetaViewport = content
		case "twitter:card":
			r.OGTags["twitter_card"] = content
		}

		switch strings.ToLower(prop) {
		case "og:title":
			r.OGTags["title"] = content
		case "og:description":
			r.OGTags["description"] = content
		case "og:image":
			r.OGTags["image"] = content
		case "og:type":
			r.OGTags["type"] = content
		}

		if strings.ToLower(httpEquiv) == "content-type" {
			// Store if needed
		}
	})
}

func (p *Parser) parseCanonical(doc *goquery.Document, r *models.SEOResult) {
	if canonical, exists := doc.Find("link[rel='canonical']").Attr("href"); exists {
		r.Canonical = canonical
	}
}

func (p *Parser) parseHeadings(doc *goquery.Document, r *models.SEOResult) {
	for i := 1; i <= 6; i++ {
		tag := fmt.Sprintf("h%d", i)
		var headings []string
		doc.Find(tag).Each(func(idx int, s *goquery.Selection) {
			text := strings.Join(strings.Fields(s.Text()), " ")
			
			// If text is empty, check if there's an image with alt text (e.g., logo in H1)
			if text == "" {
				s.Find("img").Each(func(_ int, img *goquery.Selection) {
					if alt, exists := img.Attr("alt"); exists {
						text += " " + alt
					}
				})
				text = strings.Join(strings.Fields(text), " ")
			}

			// Even if it's completely empty, we record that the tag exists
			if text == "" {
				text = "[Empty Heading]"
			}

			headings = append(headings, text)
		})
		r.Headings[tag] = headings
	}
}

func (p *Parser) parseImages(doc *goquery.Document, r *models.SEOResult) {
	doc.Find("img").Each(func(i int, s *goquery.Selection) {
		src, _ := s.Attr("src")
		if src == "" {
			return
		}

		alt, _ := s.Attr("alt")
		width, _ := s.Attr("width")
		height, _ := s.Attr("height")
		titleAttr, _ := s.Attr("title")
		loading, _ := s.Attr("loading")

		filename := extractFilename(src)
		ext := extractExtension(src)

		img := models.ImageData{
			Src:            src,
			Alt:            alt,
			HasAlt:         strings.TrimSpace(alt) != "",
			HasDimensions:  width != "" && height != "",
			HasTitle:       titleAttr != "",
			LazyLoaded:     loading == "lazy",
			Width:          width,
			Height:         height,
			Filename:       filename,
			Format:         ext,
			IsModernFormat: ext == "webp" || ext == "avif" || ext == "svg",
		}
		r.Images = append(r.Images, img)
	})

	// Calculate image stats
	r.ImageStats.Total = len(r.Images)
	for _, img := range r.Images {
		if !img.HasAlt {
			r.ImageStats.MissingAlt++
		}
		if !img.HasDimensions {
			r.ImageStats.MissingDimensions++
		}
		if !img.LazyLoaded {
			r.ImageStats.NotLazy++
		}
		if img.IsModernFormat {
			r.ImageStats.ModernFormat++
		} else {
			r.ImageStats.NotModernFormat++
		}
	}
}

func (p *Parser) parseLinks(doc *goquery.Document, r *models.SEOResult, pageURL string) {
	baseHost := ""
	if u, err := url.Parse(pageURL); err == nil {
		baseHost = u.Host
	}

	doc.Find("a").Each(func(i int, s *goquery.Selection) {
		href, exists := s.Attr("href")
		if !exists || href == "" {
			return
		}

		rel, _ := s.Attr("rel")
		isExternal := false
		if strings.HasPrefix(href, "http") {
			if linkURL, err := url.Parse(href); err == nil {
				isExternal = linkURL.Host != "" && linkURL.Host != baseHost
			}
		}
		isNofollow := strings.Contains(strings.ToLower(rel), "nofollow")

		r.Links = append(r.Links, models.LinkData{
			Href:     href,
			Rel:      rel,
			Nofollow: isNofollow,
		})

		r.LinkStats.Total++
		if isExternal {
			r.LinkStats.External++
		} else {
			r.LinkStats.Internal++
		}
		if isNofollow {
			r.LinkStats.Nofollow++
		}
	})
}

func (p *Parser) parseWordCount(doc *goquery.Document, r *models.SEOResult) {
	// Clone document to avoid modifying original
	html, _ := doc.Html()
	tempDoc, _ := goquery.NewDocumentFromReader(strings.NewReader(html))
	tempDoc.Find("script, style, nav, footer, header, aside").Each(func(i int, s *goquery.Selection) {
		s.Remove()
	})
	text := tempDoc.Find("body").Text()
	r.WordCount = len(strings.Fields(text))
}

func (p *Parser) parseSchema(doc *goquery.Document, r *models.SEOResult) {
	doc.Find("script[type='application/ld+json']").Each(func(i int, s *goquery.Selection) {
		jsonText := strings.TrimSpace(s.Text())
		if jsonText == "" {
			return
		}

		// Extract @type values
		re := regexp.MustCompile(`"@type"\s*:\s*"([^"]+)"`)
		matches := re.FindAllStringSubmatch(jsonText, -1)
		for _, match := range matches {
			if len(match) > 1 {
				r.SchemaTypes = append(r.SchemaTypes, match[1])
			}
		}

		// Validate JSON-LD: check required fields per @type
		var data map[string]interface{}
		if err := json.Unmarshal([]byte(jsonText), &data); err != nil {
			r.JSONLDIssues = append(r.JSONLDIssues, fmt.Sprintf("Invalid JSON-LD (block %d): %s", i+1, err.Error()))
			return
		}

		schemaType, _ := data["@type"].(string)
		switch schemaType {
		case "Article", "NewsArticle", "BlogPosting":
			for _, field := range []string{"headline", "author", "datePublished"} {
				if _, ok := data[field]; !ok {
					r.JSONLDIssues = append(r.JSONLDIssues,
						fmt.Sprintf("JSON-LD %s missing required field: %s", schemaType, field))
				}
			}
		case "Product":
			for _, field := range []string{"name", "offers"} {
				if _, ok := data[field]; !ok {
					r.JSONLDIssues = append(r.JSONLDIssues,
						fmt.Sprintf("JSON-LD Product missing required field: %s", field))
				}
			}
		case "Organization":
			for _, field := range []string{"name", "url"} {
				if _, ok := data[field]; !ok {
					r.JSONLDIssues = append(r.JSONLDIssues,
						fmt.Sprintf("JSON-LD Organization missing required field: %s", field))
				}
			}
		case "LocalBusiness":
			for _, field := range []string{"name", "address", "telephone"} {
				if _, ok := data[field]; !ok {
					r.JSONLDIssues = append(r.JSONLDIssues,
						fmt.Sprintf("JSON-LD LocalBusiness missing recommended field: %s", field))
				}
			}
		case "BreadcrumbList":
			if _, ok := data["itemListElement"]; !ok {
				r.JSONLDIssues = append(r.JSONLDIssues, "JSON-LD BreadcrumbList missing itemListElement")
			}
		case "FAQPage":
			if _, ok := data["mainEntity"]; !ok {
				r.JSONLDIssues = append(r.JSONLDIssues, "JSON-LD FAQPage missing mainEntity")
			}
		}
	})
}

// parseCWVHints detects common Core Web Vitals performance issues:
//   - Render-blocking <script> tags in <head> without async/defer
//   - Missing <link rel="preload"> for critical resources
//   - @font-face rules without font-display (causes layout shift / FOIT)
func (p *Parser) parseCWVHints(doc *goquery.Document, r *models.SEOResult) {
	// 1. Render-blocking scripts in <head>
	blockingScripts := 0
	doc.Find("head script[src]").Each(func(_ int, s *goquery.Selection) {
		_, hasAsync := s.Attr("async")
		_, hasDefer := s.Attr("defer")
		_, hasModule := s.Attr("type") // type=module is deferred by default
		if !hasAsync && !hasDefer && !hasModule {
			blockingScripts++
		}
	})
	if blockingScripts > 0 {
		r.CWVHints = append(r.CWVHints,
			fmt.Sprintf("%d render-blocking <script> tag(s) in <head> — add async or defer", blockingScripts))
	}

	// 2. Check for preload hints on LCP candidates (large images, hero fonts)
	hasPreload := false
	doc.Find("link[rel='preload']").Each(func(_ int, _ *goquery.Selection) {
		hasPreload = true
	})
	if !hasPreload {
		r.CWVHints = append(r.CWVHints,
			"No <link rel=\"preload\"> tags — consider preloading critical fonts, LCP images, or key CSS")
	}

	// 3. font-display check inside inline <style> blocks
	doc.Find("style").Each(func(_ int, s *goquery.Selection) {
		css := s.Text()
		if strings.Contains(css, "@font-face") && !strings.Contains(css, "font-display") {
			r.CWVHints = append(r.CWVHints,
				"@font-face rule found without font-display — add font-display:swap to prevent FOIT")
			return // flag once per page, not per block
		}
	})
}

func extractFilename(src string) string {
	filename := src
	if idx := strings.LastIndex(src, "/"); idx != -1 {
		filename = src[idx+1:]
	}
	if idx := strings.Index(filename, "?"); idx != -1 {
		filename = filename[:idx]
	}
	return filename
}

func extractExtension(src string) string {
	filename := extractFilename(src)
	if idx := strings.LastIndex(filename, "."); idx != -1 {
		ext := strings.ToLower(filename[idx+1:])
		if idx2 := strings.Index(ext, "?"); idx2 != -1 {
			ext = ext[:idx2]
		}
		return ext
	}
	return "unknown"
}
