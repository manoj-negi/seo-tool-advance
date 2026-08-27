package controllers

import (
	"fmt"
	"html"
	"strings"
	"time"

	"seo-crawler/internal/models"
)

// buildPrintableReportHTML renders a self-contained, print-friendly HTML
// document for a completed job — used as the source for PDF export.
//
// This deliberately doesn't reuse the live /seo-report page: that page
// fetches its data client-side from /api/results, which is gated behind the
// requester's auth cookie. A headless-Chrome tab generating the PDF has no
// such cookie, so driving the live page would fail for any non-guest
// report. Building the HTML directly from data we already have in-process
// sidesteps that entirely — no HTTP round-trip, no auth to carry over.
func buildPrintableReportHTML(job *models.Job) string {
	var b strings.Builder

	avg := averageScore(job.Results)
	grade, gradeColor := scoreGrade(avg)

	b.WriteString(`<!doctype html><html><head><meta charset="utf-8"><title>SEO Report</title><style>`)
	b.WriteString(reportPrintCSS)
	b.WriteString(`</style></head><body>`)

	fmt.Fprintf(&b, `<div class="header">
    <div>
      <h1>SEO Audit Report</h1>
      <div class="muted">%s &middot; generated %s &middot; %d page(s) analysed</div>
    </div>
    <div class="grade" style="background:%s">%s<span>%d/100</span></div>
  </div>`,
		html.EscapeString(job.Domain), time.Now().Format("Jan 2, 2006"), len(job.Results), gradeColor, grade, avg)

	if job.Summary != nil {
		b.WriteString(buildSummaryCardsHTML(job.Summary, len(job.Results)))
		b.WriteString(buildSiteIssuesHTML(job.Summary))
	}

	b.WriteString(`<h2>Pages</h2><table class="pages"><thead><tr>
    <th>URL</th><th>Score</th><th>Status</th><th>Title</th><th>Words</th><th>Issues</th>
  </tr></thead><tbody>`)
	for i := range job.Results {
		r := &job.Results[i]
		status := fmt.Sprintf("%d", r.StatusCode)
		if r.StatusCode == 0 {
			status = "ERR"
		}
		fmt.Fprintf(&b, `<tr>
      <td class="url">%s</td><td>%d</td><td>%s</td><td>%s</td><td>%d</td><td>%d</td>
    </tr>`,
			html.EscapeString(r.URL), r.Score, status, html.EscapeString(truncate(r.Title, 60)), r.WordCount, len(r.Issues))
	}
	b.WriteString(`</tbody></table>`)

	b.WriteString(`</body></html>`)
	return b.String()
}

func buildSummaryCardsHTML(s *models.SiteSummary, pageCount int) string {
	card := func(value int, label string) string {
		return fmt.Sprintf(`<div class="card"><div class="card-value">%d</div><div class="card-label">%s</div></div>`, value, html.EscapeString(label))
	}
	var b strings.Builder
	b.WriteString(`<div class="cards">`)
	b.WriteString(card(pageCount, "Pages Analysed"))
	b.WriteString(card(s.BrokenLinks.TotalBrokenLinks, "Broken Links"))
	b.WriteString(card(s.Duplicates.GroupCount, "Duplicate Groups"))
	b.WriteString(card(len(s.OrphanPages), "Orphan Pages"))
	b.WriteString(card(s.ThinContent.Count, "Thin Content Pages"))
	b.WriteString(card(len(s.HreflangIssues), "Hreflang Issues"))
	b.WriteString(card(len(s.SitemapRobotsConflicts), "Sitemap/Robots Conflicts"))
	b.WriteString(`</div>`)
	return b.String()
}

func buildSiteIssuesHTML(s *models.SiteSummary) string {
	var b strings.Builder

	if len(s.BrokenLinks.Items) > 0 {
		b.WriteString(`<h2>Broken Links</h2><table class="issues"><thead><tr><th>Found on</th><th>Broken link</th><th>Reason</th></tr></thead><tbody>`)
		for _, item := range s.BrokenLinks.Items {
			reason := item.Error
			if item.StatusCode != 0 {
				reason = fmt.Sprintf("HTTP %d", item.StatusCode)
			}
			fmt.Fprintf(&b, `<tr><td class="url">%s</td><td class="url">%s</td><td>%s</td></tr>`,
				html.EscapeString(item.PageURL), html.EscapeString(item.Href), html.EscapeString(reason))
		}
		b.WriteString(`</tbody></table>`)
	}

	if len(s.Duplicates.Groups) > 0 {
		b.WriteString(`<h2>Duplicate Content</h2>`)
		for _, g := range s.Duplicates.Groups {
			fmt.Fprintf(&b, `<div class="group"><div class="group-title">%s</div><ul>`, html.EscapeString(g.Title))
			for _, u := range g.URLs {
				fmt.Fprintf(&b, `<li class="url">%s</li>`, html.EscapeString(u))
			}
			b.WriteString(`</ul></div>`)
		}
	}

	if len(s.OrphanPages) > 0 {
		b.WriteString(`<h2>Orphan Pages <span class="muted">(no internal links point to these)</span></h2><ul>`)
		for _, u := range s.OrphanPages {
			fmt.Fprintf(&b, `<li class="url">%s</li>`, html.EscapeString(u))
		}
		b.WriteString(`</ul>`)
	}

	if len(s.HreflangIssues) > 0 {
		b.WriteString(`<h2>Hreflang Issues</h2><table class="issues"><thead><tr><th>Page</th><th>Lang</th><th>Target</th><th>Issue</th></tr></thead><tbody>`)
		for _, h := range s.HreflangIssues {
			fmt.Fprintf(&b, `<tr><td class="url">%s</td><td>%s</td><td class="url">%s</td><td>%s</td></tr>`,
				html.EscapeString(h.PageURL), html.EscapeString(h.Lang), html.EscapeString(h.Href), html.EscapeString(h.Issue))
		}
		b.WriteString(`</tbody></table>`)
	}

	if len(s.ThinContent.Pages) > 0 {
		b.WriteString(`<h2>Thin Content Pages <span class="muted">(under 300 words)</span></h2><ul>`)
		for _, u := range s.ThinContent.Pages {
			fmt.Fprintf(&b, `<li class="url">%s</li>`, html.EscapeString(u))
		}
		b.WriteString(`</ul>`)
	}

	if len(s.SitemapRobotsConflicts) > 0 {
		b.WriteString(`<h2>Sitemap / Robots.txt Conflicts <span class="muted">(listed in sitemap.xml but blocked by robots.txt)</span></h2><ul>`)
		for _, u := range s.SitemapRobotsConflicts {
			fmt.Fprintf(&b, `<li class="url">%s</li>`, html.EscapeString(u))
		}
		b.WriteString(`</ul>`)
	}

	return b.String()
}

func averageScore(results []models.SEOResult) int {
	if len(results) == 0 {
		return 0
	}
	total := 0
	for _, r := range results {
		total += r.Score
	}
	return total / len(results)
}

func truncate(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n]) + "…"
}

func scoreGrade(score int) (grade, color string) {
	switch {
	case score >= 90:
		return "A+", "#059669"
	case score >= 80:
		return "A", "#059669"
	case score >= 70:
		return "B", "#0891b2"
	case score >= 60:
		return "C", "#d97706"
	case score >= 50:
		return "D", "#d97706"
	default:
		return "F", "#dc2626"
	}
}

const reportPrintCSS = `
  * { box-sizing: border-box; }
  body { font-family: -apple-system, Helvetica, Arial, sans-serif; color: #1a1a2e; margin: 32px; font-size: 12px; }
  h1 { font-size: 20px; margin: 0; }
  h2 { font-size: 14px; margin: 24px 0 8px; border-bottom: 2px solid #e5e7eb; padding-bottom: 4px; }
  .muted { color: #6b7280; font-size: 11px; }
  .header { display: flex; justify-content: space-between; align-items: center; border-bottom: 3px solid #1a1a2e; padding-bottom: 16px; }
  .grade { color: #fff; border-radius: 12px; padding: 10px 18px; font-size: 22px; font-weight: 800; display: flex; flex-direction: column; align-items: center; }
  .grade span { font-size: 10px; font-weight: 600; }
  .cards { display: flex; gap: 10px; margin-top: 20px; flex-wrap: wrap; }
  .card { border: 1px solid #e5e7eb; border-radius: 8px; padding: 10px 14px; min-width: 100px; }
  .card-value { font-size: 20px; font-weight: 800; }
  .card-label { font-size: 10px; color: #6b7280; }
  table { width: 100%; border-collapse: collapse; margin-top: 6px; }
  th, td { text-align: left; padding: 5px 8px; border-bottom: 1px solid #eee; font-size: 11px; }
  th { background: #f3f4f6; font-size: 10px; text-transform: uppercase; color: #6b7280; }
  .url { font-family: monospace; word-break: break-all; max-width: 320px; }
  .group { border: 1px solid #eee; border-radius: 6px; padding: 8px 12px; margin-bottom: 8px; }
  .group-title { font-weight: 700; margin-bottom: 4px; }
  ul { margin: 4px 0; padding-left: 18px; }
  li { margin-bottom: 2px; }
`
