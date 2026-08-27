package scorer

import (
	"fmt"
	"strings"

	"seo-crawler/internal/models"
)

// CalculateScore implements the 100-point SEO scoring system
func CalculateScore(r *models.SEOResult) {
	score := 0

	// TITLE (20 pts)
	if r.Title == "" {
		r.Issues = append(r.Issues, models.CheckResult{Msg: "Missing page title — critical for SEO"})
	} else if r.TitleLength < 10 {
		r.Issues = append(r.Issues, models.CheckResult{
			Msg:    fmt.Sprintf("Title too short (%d chars) — aim for 50–60", r.TitleLength),
			Points: 5,
		})
		score += 5
	} else if r.TitleLength > 70 {
		r.Warnings = append(r.Warnings, models.CheckResult{
			Msg:    fmt.Sprintf("Title too long (%d chars) — Google truncates at ~60", r.TitleLength),
			Points: 12,
		})
		score += 12
	} else {
		r.Passed = append(r.Passed, models.CheckResult{
			Msg:    fmt.Sprintf("Title length ideal (%d chars)", r.TitleLength),
			Points: 20,
		})
		score += 20
	}

	// META DESCRIPTION (20 pts)
	if r.MetaDescription == "" {
		r.Issues = append(r.Issues, models.CheckResult{Msg: "Missing meta description — reduces CTR in search results"})
	} else if r.MetaDescriptionLength < 50 {
		r.Issues = append(r.Issues, models.CheckResult{
			Msg:    fmt.Sprintf("Meta description too short (%d chars) — aim for 120–160", r.MetaDescriptionLength),
			Points: 5,
		})
		score += 5
	} else if r.MetaDescriptionLength > 165 {
		r.Warnings = append(r.Warnings, models.CheckResult{
			Msg:    fmt.Sprintf("Meta description too long (%d chars) — Google truncates at ~160", r.MetaDescriptionLength),
			Points: 12,
		})
		score += 12
	} else {
		r.Passed = append(r.Passed, models.CheckResult{
			Msg:    fmt.Sprintf("Meta description length ideal (%d chars)", r.MetaDescriptionLength),
			Points: 20,
		})
		score += 20
	}

	// H1 (15 pts)
	h1s := r.Headings["h1"]
	if len(h1s) == 0 {
		r.Issues = append(r.Issues, models.CheckResult{Msg: "No H1 tag found — every page should have exactly one H1"})
	} else if len(h1s) > 1 {
		r.Warnings = append(r.Warnings, models.CheckResult{
			Msg:    fmt.Sprintf("Multiple H1 tags found (%d) — should have exactly one", len(h1s)),
			Points: 8,
		})
		score += 8
	} else {
		r.Passed = append(r.Passed, models.CheckResult{
			Msg:    "Exactly one H1 tag found",
			Points: 15,
		})
		score += 15
	}

	// H2 structure (5 pts)
	if len(r.Headings["h2"]) > 0 {
		r.Passed = append(r.Passed, models.CheckResult{
			Msg:    fmt.Sprintf("%d H2 heading(s) found — good structure", len(r.Headings["h2"])),
			Points: 5,
		})
		score += 5
	} else {
		r.Warnings = append(r.Warnings, models.CheckResult{Msg: "No H2 headings — consider adding subheadings for structure"})
	}

	// Canonical (5 pts)
	if r.Canonical != "" {
		r.Passed = append(r.Passed, models.CheckResult{
			Msg:    "Canonical URL tag present",
			Points: 5,
		})
		score += 5
	} else {
		r.Warnings = append(r.Warnings, models.CheckResult{Msg: "No canonical URL tag — may cause duplicate content issues"})
	}

	// HTTPS (5 pts)
	if r.HasHTTPS {
		r.Passed = append(r.Passed, models.CheckResult{
			Msg:    "Page served over HTTPS",
			Points: 5,
		})
		score += 5
	} else {
		r.Issues = append(r.Issues, models.CheckResult{Msg: "Page not served over HTTPS — Google prefers HTTPS"})
	}

	// Images (10 pts)
	if r.ImageStats.Total == 0 {
		r.Warnings = append(r.Warnings, models.CheckResult{Msg: "No images found on page"})
		score += 5
	} else if r.ImageStats.MissingAlt == 0 {
		r.Passed = append(r.Passed, models.CheckResult{
			Msg:    fmt.Sprintf("All %d images have alt text", r.ImageStats.Total),
			Points: 10,
		})
		score += 10
	} else if r.ImageStats.MissingAlt < r.ImageStats.Total {
		r.Warnings = append(r.Warnings, models.CheckResult{
			Msg:    fmt.Sprintf("%d image(s) missing alt text (%d total)", r.ImageStats.MissingAlt, r.ImageStats.Total),
			Points: 5,
		})
		score += 5
	} else {
		r.Issues = append(r.Issues, models.CheckResult{
			Msg: fmt.Sprintf("All %d image(s) missing alt text — hurts accessibility + SEO", r.ImageStats.Total),
		})
	}

	// Word count (5 pts)
	if r.WordCount >= 300 {
		r.Passed = append(r.Passed, models.CheckResult{
			Msg:    fmt.Sprintf("Good content length (%d words)", r.WordCount),
			Points: 5,
		})
		score += 5
	} else if r.WordCount >= 100 {
		r.Warnings = append(r.Warnings, models.CheckResult{
			Msg:    fmt.Sprintf("Thin content (%d words) — aim for 300+ words", r.WordCount),
			Points: 2,
		})
		score += 2
	} else {
		r.Issues = append(r.Issues, models.CheckResult{
			Msg: fmt.Sprintf("Very thin content (%d words) — consider adding more content", r.WordCount),
		})
	}

	// Viewport (3 pts)
	if r.MetaViewport != "" {
		r.Passed = append(r.Passed, models.CheckResult{
			Msg:    "Viewport meta tag present — mobile-friendly",
			Points: 3,
		})
		score += 3
	} else {
		r.Issues = append(r.Issues, models.CheckResult{Msg: "Missing viewport meta tag — page may not be mobile-friendly"})
	}

	// OG Tags (4 pts)
	ogCount := len(r.OGTags)
	if ogCount >= 3 {
		r.Passed = append(r.Passed, models.CheckResult{
			Msg:    fmt.Sprintf("Open Graph tags present (%d tags) — good for social sharing", ogCount),
			Points: 4,
		})
		score += 4
	} else if ogCount > 0 {
		r.Warnings = append(r.Warnings, models.CheckResult{
			Msg:    fmt.Sprintf("Partial Open Graph tags (%d) — add og:title, og:description, og:image", ogCount),
			Points: 2,
		})
		score += 2
	} else {
		r.Warnings = append(r.Warnings, models.CheckResult{Msg: "No Open Graph tags — social media previews won't be optimised"})
	}

	// Keywords meta (bonus)
	if r.MetaKeywords != "" {
		r.Passed = append(r.Passed, models.CheckResult{
			Msg:    "Meta keywords tag present (low SEO value but present)",
			Points: 1,
		})
		score += 1
	}

	// Page speed (4 pts)
	if r.LoadTimeMs < 1000 {
		r.Passed = append(r.Passed, models.CheckResult{
			Msg:    fmt.Sprintf("Fast page load (%dms)", r.LoadTimeMs),
			Points: 4,
		})
		score += 4
	} else if r.LoadTimeMs < 2500 {
		r.Warnings = append(r.Warnings, models.CheckResult{
			Msg:    fmt.Sprintf("Moderate load time (%dms) — aim for <1000ms", r.LoadTimeMs),
			Points: 2,
		})
		score += 2
	} else {
		r.Issues = append(r.Issues, models.CheckResult{
			Msg: fmt.Sprintf("Slow load time (%dms) — impacts Core Web Vitals & ranking", r.LoadTimeMs),
		})
	}

	// Page size
	pageKB := float64(r.PageSizeBytes) / 1024
	if pageKB > 500 {
		r.Warnings = append(r.Warnings, models.CheckResult{
			Msg: fmt.Sprintf("Large page size (%.0f KB) — consider optimising resources", pageKB),
		})
	} else if pageKB > 100 {
		r.Passed = append(r.Passed, models.CheckResult{
			Msg: fmt.Sprintf("Page size acceptable (%.0f KB)", pageKB),
		})
	}

	// Robots meta
	if strings.Contains(strings.ToLower(r.RobotsMeta), "noindex") {
		r.Issues = append(r.Issues, models.CheckResult{Msg: "Page has noindex directive — will NOT be indexed by Google"})
	} else if r.RobotsMeta != "" {
		r.Passed = append(r.Passed, models.CheckResult{Msg: "Robots meta tag present and allows indexing"})
	}

	// Security Headers (Bonus & Warnings)
	if r.StrictTransportSecurity != "" {
		r.Passed = append(r.Passed, models.CheckResult{Msg: "Strict-Transport-Security (HSTS) header is present", Points: 1})
		score += 1
	} else if r.HasHTTPS {
		r.Warnings = append(r.Warnings, models.CheckResult{Msg: "Missing Strict-Transport-Security (HSTS) header on HTTPS site"})
	}

	if r.ContentSecurity != "" {
		r.Passed = append(r.Passed, models.CheckResult{Msg: "Content-Security-Policy (CSP) header is present", Points: 1})
		score += 1
	} else {
		r.Warnings = append(r.Warnings, models.CheckResult{Msg: "Missing Content-Security-Policy (CSP) header"})
	}

	if r.XFrameOptions != "" {
		r.Passed = append(r.Passed, models.CheckResult{Msg: "X-Frame-Options header is present (prevents clickjacking)", Points: 1})
		score += 1
	} else {
		r.Warnings = append(r.Warnings, models.CheckResult{Msg: "Missing X-Frame-Options header"})
	}

	if r.XContentTypeOptions != "" {
		r.Passed = append(r.Passed, models.CheckResult{Msg: "X-Content-Type-Options header is present (prevents MIME-sniffing)"})
	} else {
		r.Warnings = append(r.Warnings, models.CheckResult{Msg: "Missing X-Content-Type-Options header"})
	}

	// Real Core Web Vitals (Bonus & Warnings) — only available for pages
	// that went through the headless-Chrome render fallback, where the
	// browser's own Performance API gives actual measurements instead of
	// the static heuristics above. Thresholds match Google's official
	// "good" / "needs improvement" / "poor" boundaries.
	if r.CWVMeasured {
		switch {
		case r.LCPMs <= 2500:
			r.Passed = append(r.Passed, models.CheckResult{Msg: fmt.Sprintf("Good LCP (%.0fms) — meets Core Web Vitals threshold", r.LCPMs), Points: 3})
			score += 3
		case r.LCPMs <= 4000:
			r.Warnings = append(r.Warnings, models.CheckResult{Msg: fmt.Sprintf("LCP needs improvement (%.0fms) — Google's \"good\" threshold is 2500ms", r.LCPMs)})
		default:
			r.Issues = append(r.Issues, models.CheckResult{Msg: fmt.Sprintf("Poor LCP (%.0fms) — largest content takes too long to render, hurts ranking", r.LCPMs)})
		}

		switch {
		case r.CLS <= 0.1:
			r.Passed = append(r.Passed, models.CheckResult{Msg: fmt.Sprintf("Good CLS (%.2f) — page layout is stable", r.CLS), Points: 3})
			score += 3
		case r.CLS <= 0.25:
			r.Warnings = append(r.Warnings, models.CheckResult{Msg: fmt.Sprintf("CLS needs improvement (%.2f) — Google's \"good\" threshold is 0.1", r.CLS)})
		default:
			r.Issues = append(r.Issues, models.CheckResult{Msg: fmt.Sprintf("Poor CLS (%.2f) — visible content shifts during load, hurts UX and ranking", r.CLS)})
		}
	}

	if score > 100 {
		score = 100
	}
	r.Score = score
}
