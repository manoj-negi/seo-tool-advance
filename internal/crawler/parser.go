package crawler

import (
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

	return r
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
		jsonText := s.Text()
		re := regexp.MustCompile(`"@type"\s*:\s*"([^"]+)"`)
		matches := re.FindAllStringSubmatch(jsonText, -1)
		for _, match := range matches {
			if len(match) > 1 {
				r.SchemaTypes = append(r.SchemaTypes, match[1])
			}
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
