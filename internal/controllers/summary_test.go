package controllers

import (
	"testing"

	"seo-crawler/internal/models"
)

func TestBuildSiteSummary_SitemapRobotsConflicts(t *testing.T) {
	job := &models.Job{
		SitemapURL: "https://x.com/sitemap.xml", // URLs came from a real sitemap
		Results: []models.SEOResult{
			{URL: "https://x.com/", RobotsBlocked: false},
			{URL: "https://x.com/private", RobotsBlocked: true},
			{URL: "https://x.com/admin", RobotsBlocked: true},
		},
	}

	summary := buildSiteSummary(job)

	want := map[string]bool{"https://x.com/private": true, "https://x.com/admin": true}
	if len(summary.SitemapRobotsConflicts) != 2 {
		t.Fatalf("SitemapRobotsConflicts = %v, want 2 entries", summary.SitemapRobotsConflicts)
	}
	for _, u := range summary.SitemapRobotsConflicts {
		if !want[u] {
			t.Errorf("unexpected URL in conflicts: %s", u)
		}
	}
}

func TestBuildSiteSummary_NoSitemapMeansNoConflictsReported(t *testing.T) {
	// No SitemapURL — this was a bare single-page crawl, not sitemap-driven,
	// so a robots block here isn't a "sitemap says X but robots blocks X"
	// contradiction and must not be reported as one.
	job := &models.Job{
		Results: []models.SEOResult{
			{URL: "https://x.com/", RobotsBlocked: true},
		},
	}

	summary := buildSiteSummary(job)
	if len(summary.SitemapRobotsConflicts) != 0 {
		t.Errorf("expected no conflicts without a sitemap-driven crawl, got %v", summary.SitemapRobotsConflicts)
	}
}

func TestBuildSiteSummary_BrokenLinksAndDuplicates(t *testing.T) {
	job := &models.Job{Results: []models.SEOResult{
		{
			URL: "https://x.com/",
			BrokenLinks: []models.BrokenLink{
				{Href: "https://x.com/dead", StatusCode: 404},
			},
		},
		{URL: "https://x.com/a", Title: "Same Title", MetaDescription: "Same Desc"},
		{URL: "https://x.com/b", Title: "Same Title", MetaDescription: "Same Desc", DuplicateOf: "https://x.com/a"},
		{URL: "https://x.com/c", Title: "Same Title", MetaDescription: "Same Desc", DuplicateOf: "https://x.com/a"},
	}}

	summary := buildSiteSummary(job)

	if summary.BrokenLinks.TotalBrokenLinks != 1 {
		t.Errorf("TotalBrokenLinks = %d, want 1", summary.BrokenLinks.TotalBrokenLinks)
	}
	if summary.BrokenLinks.PagesAffected != 1 {
		t.Errorf("PagesAffected = %d, want 1", summary.BrokenLinks.PagesAffected)
	}
	if summary.BrokenLinks.Items[0].PageURL != "https://x.com/" || summary.BrokenLinks.Items[0].Href != "https://x.com/dead" {
		t.Errorf("unexpected broken link item: %+v", summary.BrokenLinks.Items[0])
	}

	if summary.Duplicates.GroupCount != 1 {
		t.Errorf("GroupCount = %d, want 1", summary.Duplicates.GroupCount)
	}
	if summary.Duplicates.PageCount != 3 {
		t.Errorf("PageCount = %d, want 3 (anchor + 2 duplicates)", summary.Duplicates.PageCount)
	}
	wantURLs := map[string]bool{"https://x.com/a": true, "https://x.com/b": true, "https://x.com/c": true}
	for _, u := range summary.Duplicates.Groups[0].URLs {
		if !wantURLs[u] {
			t.Errorf("unexpected URL in duplicate group: %s", u)
		}
		delete(wantURLs, u)
	}
	if len(wantURLs) != 0 {
		t.Errorf("duplicate group missing URLs: %v", wantURLs)
	}
}

func TestBuildSiteSummary_ThinContent(t *testing.T) {
	job := &models.Job{Results: []models.SEOResult{
		{URL: "https://x.com/", WordCount: 500},
		{URL: "https://x.com/thin", WordCount: 50},
		{URL: "https://x.com/broken", WordCount: 0, Error: "timeout or connection error"}, // excluded: fetch failed
	}}

	summary := buildSiteSummary(job)

	if summary.ThinContent.Count != 1 {
		t.Errorf("Count = %d, want 1", summary.ThinContent.Count)
	}
	if len(summary.ThinContent.Pages) != 1 || summary.ThinContent.Pages[0] != "https://x.com/thin" {
		t.Errorf("Pages = %v, want [https://x.com/thin]", summary.ThinContent.Pages)
	}
}

func TestBuildSiteSummary_OrphanPages(t *testing.T) {
	// Home links to /a, /a links to home. /orphan is reachable by URL but
	// nothing in the crawl links to it.
	job := &models.Job{Results: []models.SEOResult{
		{URL: "https://x.com/", Links: []models.LinkData{{Href: "https://x.com/a"}}},
		{URL: "https://x.com/a", Links: []models.LinkData{{Href: "https://x.com/"}}},
		{URL: "https://x.com/orphan"},
	}}

	summary := buildSiteSummary(job)

	if len(summary.OrphanPages) != 1 || summary.OrphanPages[0] != "https://x.com/orphan" {
		t.Errorf("OrphanPages = %v, want [https://x.com/orphan]", summary.OrphanPages)
	}

	// Verify per-page fields were set too.
	byURL := map[string]models.SEOResult{}
	for _, r := range job.Results {
		byURL[r.URL] = r
	}
	if byURL["https://x.com/"].IsOrphan {
		t.Error("home page must never be flagged orphan, even with 0 inlinks")
	}
	if byURL["https://x.com/a"].InternalInlinks != 1 {
		t.Errorf("/a InternalInlinks = %d, want 1", byURL["https://x.com/a"].InternalInlinks)
	}
	if !byURL["https://x.com/orphan"].IsOrphan {
		t.Error("expected /orphan to be flagged IsOrphan")
	}
}

func TestBuildSiteSummary_SinglePageCrawlNeverOrphaned(t *testing.T) {
	job := &models.Job{Results: []models.SEOResult{
		{URL: "https://x.com/"},
	}}
	summary := buildSiteSummary(job)
	if len(summary.OrphanPages) != 0 {
		t.Errorf("a single-page crawl must never report orphans, got %v", summary.OrphanPages)
	}
}

func TestBuildSiteSummary_HreflangReciprocity(t *testing.T) {
	job := &models.Job{Results: []models.SEOResult{
		{
			URL: "https://x.com/en",
			Hreflang: []models.HreflangTag{
				{Lang: "fr", Href: "https://x.com/fr"},   // reciprocal — fine
				{Lang: "de", Href: "https://x.com/de"},   // NOT reciprocal — should be flagged
			},
		},
		{
			URL: "https://x.com/fr",
			Hreflang: []models.HreflangTag{
				{Lang: "en", Href: "https://x.com/en"},
			},
		},
		{
			URL: "https://x.com/de",
			// no hreflang tags back to /en at all
		},
	}}

	summary := buildSiteSummary(job)

	if len(summary.HreflangIssues) != 1 {
		t.Fatalf("HreflangIssues = %+v, want exactly 1 issue", summary.HreflangIssues)
	}
	issue := summary.HreflangIssues[0]
	if issue.PageURL != "https://x.com/en" || issue.Href != "https://x.com/de" {
		t.Errorf("unexpected issue: %+v", issue)
	}
}

func TestBuildSiteSummary_HreflangTargetNotCrawledIsNotFlagged(t *testing.T) {
	// The target of the hreflang tag was never crawled, so we can't verify
	// reciprocity either way — must not guess and flag it.
	job := &models.Job{Results: []models.SEOResult{
		{
			URL: "https://x.com/en",
			Hreflang: []models.HreflangTag{
				{Lang: "ja", Href: "https://x.com/ja"}, // not in job.Results
			},
		},
	}}

	summary := buildSiteSummary(job)
	if len(summary.HreflangIssues) != 0 {
		t.Errorf("expected no issues for an uncrawled hreflang target, got %+v", summary.HreflangIssues)
	}
}
