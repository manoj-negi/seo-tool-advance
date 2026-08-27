package scorer

import (
	"strings"
	"testing"

	"seo-crawler/internal/models"
)

func hasIssueContaining(r *models.SEOResult, substr string) bool {
	for _, i := range r.Issues {
		if strings.Contains(i.Msg, substr) {
			return true
		}
	}
	return false
}

func hasWarningContaining(r *models.SEOResult, substr string) bool {
	for _, w := range r.Warnings {
		if strings.Contains(w.Msg, substr) {
			return true
		}
	}
	return false
}

func hasPassedContaining(r *models.SEOResult, substr string) bool {
	for _, p := range r.Passed {
		if strings.Contains(p.Msg, substr) {
			return true
		}
	}
	return false
}

func TestCalculateScore_CWVNotMeasuredSkipsChecksEntirely(t *testing.T) {
	r := &models.SEOResult{CWVMeasured: false, LCPMs: 9999, CLS: 9.9}
	CalculateScore(r)
	if hasIssueContaining(r, "LCP") || hasWarningContaining(r, "LCP") || hasPassedContaining(r, "LCP") {
		t.Error("expected no LCP-related check output when CWVMeasured is false, regardless of LCPMs value")
	}
	if hasIssueContaining(r, "CLS") || hasWarningContaining(r, "CLS") || hasPassedContaining(r, "CLS") {
		t.Error("expected no CLS-related check output when CWVMeasured is false")
	}
}

func TestCalculateScore_GoodLCPAndCLS(t *testing.T) {
	r := &models.SEOResult{CWVMeasured: true, LCPMs: 1200, CLS: 0.02}
	CalculateScore(r)
	if !hasPassedContaining(r, "Good LCP") {
		t.Error("expected a 'Good LCP' passed check for LCP under 2500ms")
	}
	if !hasPassedContaining(r, "Good CLS") {
		t.Error("expected a 'Good CLS' passed check for CLS under 0.1")
	}
}

func TestCalculateScore_NeedsImprovementLCPAndCLS(t *testing.T) {
	r := &models.SEOResult{CWVMeasured: true, LCPMs: 3000, CLS: 0.18}
	CalculateScore(r)
	if !hasWarningContaining(r, "LCP needs improvement") {
		t.Error("expected an LCP warning for 2500-4000ms range")
	}
	if !hasWarningContaining(r, "CLS needs improvement") {
		t.Error("expected a CLS warning for 0.1-0.25 range")
	}
}

func TestCalculateScore_PoorLCPAndCLS(t *testing.T) {
	r := &models.SEOResult{CWVMeasured: true, LCPMs: 5000, CLS: 0.4}
	CalculateScore(r)
	if !hasIssueContaining(r, "Poor LCP") {
		t.Error("expected a 'Poor LCP' issue for LCP over 4000ms")
	}
	if !hasIssueContaining(r, "Poor CLS") {
		t.Error("expected a 'Poor CLS' issue for CLS over 0.25")
	}
}

func TestCalculateScore_ScoreNeverExceeds100(t *testing.T) {
	r := &models.SEOResult{
		CWVMeasured: true, LCPMs: 500, CLS: 0.01,
		Title: "A perfectly good title for this page here", TitleLength: 45,
		MetaDescription: "A perfectly reasonable meta description that sits comfortably within the ideal length window for search snippets.", MetaDescriptionLength: 130,
		Headings:                map[string][]string{"h1": {"Only Heading"}, "h2": {"Sub"}},
		Canonical:               "https://example.com/",
		HasHTTPS:                true,
		WordCount:               500,
		MetaViewport:            "width=device-width",
		OGTags:                  map[string]string{"title": "x", "description": "y", "image": "z"},
		MetaKeywords:            "a,b,c",
		LoadTimeMs:              200,
		StrictTransportSecurity: "max-age=31536000",
		ContentSecurity:         "default-src 'self'",
		XFrameOptions:           "DENY",
		XContentTypeOptions:     "nosniff",
	}
	CalculateScore(r)
	if r.Score > 100 {
		t.Errorf("Score = %d, must never exceed 100", r.Score)
	}
}
