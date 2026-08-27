package controllers

import (
	"fmt"
	"net/url"
	"strings"

	"seo-crawler/internal/models"
)

// thinContentWordThreshold matches the scorer's own "good content length"
// bar (internal/scorer/seo_scorer.go) — pages below it are flagged here.
const thinContentWordThreshold = 300

// normalizeURLForMatching produces a comparison key so links between
// crawled pages match even when they differ in trailing slash or fragment —
// e.g. "https://x.com/about/" and "https://x.com/about#top" refer to the
// same page for link-graph/hreflang purposes.
func normalizeURLForMatching(raw string) string {
	u, err := url.Parse(raw)
	if err != nil {
		return strings.TrimSuffix(raw, "/")
	}
	u.Fragment = ""
	u.Scheme = strings.ToLower(u.Scheme)
	u.Host = strings.ToLower(u.Host)
	u.Path = strings.TrimSuffix(u.Path, "/")
	return u.String()
}

// buildSiteSummary aggregates per-page signals already computed during the
// crawl (broken links, duplicate content, hreflang tags, word count) into
// report-wide rollups, and sets the per-page InternalInlinks/IsOrphan
// fields from the site-wide internal link graph. Must be called with
// Controller.JobsMu held for writing, since it mutates job.Results entries
// that other goroutines may be reading concurrently via HandleResults.
func buildSiteSummary(job *models.Job) *models.SiteSummary {
	summary := &models.SiteSummary{}

	summary.BrokenLinks = aggregateBrokenLinks(job)
	summary.Duplicates = aggregateDuplicates(job)
	summary.ThinContent = aggregateThinContent(job)

	crawled := buildCrawledIndex(job)
	summary.OrphanPages = applyLinkGraph(job, crawled)
	summary.HreflangIssues = aggregateHreflangIssues(job, crawled)

	return summary
}

func aggregateBrokenLinks(job *models.Job) models.BrokenLinksSummary {
	var out models.BrokenLinksSummary
	pagesAffected := 0
	for i := range job.Results {
		res := &job.Results[i]
		if len(res.BrokenLinks) == 0 {
			continue
		}
		pagesAffected++
		for _, bl := range res.BrokenLinks {
			out.Items = append(out.Items, models.BrokenLinkSiteItem{
				PageURL:    res.URL,
				Href:       bl.Href,
				StatusCode: bl.StatusCode,
				Error:      bl.Error,
			})
		}
	}
	out.TotalBrokenLinks = len(out.Items)
	out.PagesAffected = pagesAffected
	return out
}

func aggregateDuplicates(job *models.Job) models.DuplicatesSummary {
	// detectDuplicates (seo.go) already stamped res.DuplicateOf with the URL
	// of the first page sharing the same title+description. Group by that
	// anchor URL to turn per-page flags into "these N pages are duplicates
	// of each other."
	var out models.DuplicatesSummary
	anchors := make([]string, 0)
	dupesByAnchor := make(map[string][]string)
	titleByAnchor := make(map[string]string)
	descByAnchor := make(map[string]string)

	for i := range job.Results {
		res := &job.Results[i]
		if res.DuplicateOf == "" {
			continue
		}
		if _, seen := dupesByAnchor[res.DuplicateOf]; !seen {
			anchors = append(anchors, res.DuplicateOf) // preserve first-seen order
		}
		dupesByAnchor[res.DuplicateOf] = append(dupesByAnchor[res.DuplicateOf], res.URL)
		titleByAnchor[res.DuplicateOf] = res.Title
		descByAnchor[res.DuplicateOf] = res.MetaDescription
	}

	pageCount := 0
	for _, anchor := range anchors {
		urls := append([]string{anchor}, dupesByAnchor[anchor]...)
		pageCount += len(urls)
		out.Groups = append(out.Groups, models.DuplicateGroup{
			Title:       titleByAnchor[anchor],
			Description: descByAnchor[anchor],
			URLs:        urls,
		})
	}
	out.GroupCount = len(out.Groups)
	out.PageCount = pageCount
	return out
}

func aggregateThinContent(job *models.Job) models.ThinContentSummary {
	var out models.ThinContentSummary
	for i := range job.Results {
		res := &job.Results[i]
		if res.Error != "" {
			continue // couldn't fetch — not a content-quality signal
		}
		if res.WordCount < thinContentWordThreshold {
			out.Pages = append(out.Pages, res.URL)
		}
	}
	out.Count = len(out.Pages)
	return out
}

// buildCrawledIndex maps every crawled page's normalized URL to its index
// in job.Results, so links can be checked against "is this one of our own
// crawled pages" in O(1).
func buildCrawledIndex(job *models.Job) map[string]int {
	crawled := make(map[string]int, len(job.Results))
	for i := range job.Results {
		crawled[normalizeURLForMatching(job.Results[i].URL)] = i
	}
	return crawled
}

// applyLinkGraph counts, for every crawled page, how many *other* crawled
// pages link to it (internal inlinks), sets InternalInlinks/IsOrphan on
// each result, and returns the URLs of pages nothing else links to — a page
// with zero inlinks is invisible to both users navigating the site and
// search engines following links, unless it's the crawl's own starting page.
func applyLinkGraph(job *models.Job, crawled map[string]int) []string {
	inlinks := make(map[string]int, len(job.Results))

	for i := range job.Results {
		res := &job.Results[i]
		base, _ := url.Parse(res.URL)
		selfKey := normalizeURLForMatching(res.URL)
		countedAlready := make(map[string]bool) // one page linking to a target counts once

		for _, link := range res.Links {
			href := link.Href
			if href == "" || strings.HasPrefix(href, "#") {
				continue
			}
			if !strings.HasPrefix(href, "http") && base != nil {
				if ref, err := url.Parse(href); err == nil {
					href = base.ResolveReference(ref).String()
				}
			}
			key := normalizeURLForMatching(href)
			if _, isCrawled := crawled[key]; !isCrawled {
				continue
			}
			if key == selfKey || countedAlready[key] {
				continue
			}
			countedAlready[key] = true
			inlinks[key]++
		}
	}

	homeKey := ""
	if len(job.Results) > 0 {
		homeKey = normalizeURLForMatching(job.Results[0].URL)
	}

	var orphans []string
	for i := range job.Results {
		res := &job.Results[i]
		key := normalizeURLForMatching(res.URL)
		count := inlinks[key]
		res.InternalInlinks = count
		// A single-page crawl has nothing else to link from it, so it's not
		// meaningfully "orphaned"; and the crawl's own starting page is
		// expected to have no internal inlinks within the crawl itself.
		res.IsOrphan = count == 0 && key != homeKey && len(job.Results) > 1
		if res.IsOrphan {
			orphans = append(orphans, res.URL)
		}
	}
	return orphans
}

// aggregateHreflangIssues flags hreflang tags whose target — when that
// target is also one of the pages we crawled — doesn't link back with a
// matching hreflang tag. Google requires this reciprocity for hreflang
// annotations to be honored at all; a one-way tag is silently ignored.
func aggregateHreflangIssues(job *models.Job, crawled map[string]int) []models.HreflangIssue {
	hreflangByPage := make(map[string]map[string]string, len(job.Results))
	for i := range job.Results {
		res := &job.Results[i]
		if len(res.Hreflang) == 0 {
			continue
		}
		m := make(map[string]string, len(res.Hreflang))
		for _, h := range res.Hreflang {
			m[h.Lang] = h.Href
		}
		hreflangByPage[normalizeURLForMatching(res.URL)] = m
	}

	var issues []models.HreflangIssue
	for i := range job.Results {
		res := &job.Results[i]
		for _, h := range res.Hreflang {
			targetKey := normalizeURLForMatching(h.Href)
			targetIdx, isCrawled := crawled[targetKey]
			if !isCrawled {
				continue // target wasn't part of this crawl — can't verify reciprocity
			}

			reciprocal := false
			for _, targetHref := range hreflangByPage[targetKey] {
				if normalizeURLForMatching(targetHref) == normalizeURLForMatching(res.URL) {
					reciprocal = true
					break
				}
			}
			if !reciprocal {
				issues = append(issues, models.HreflangIssue{
					PageURL: res.URL,
					Lang:    h.Lang,
					Href:    h.Href,
					Issue:   fmt.Sprintf("%s does not link back to this page — hreflang requires reciprocal tags", job.Results[targetIdx].URL),
				})
			}
		}
	}
	return issues
}
