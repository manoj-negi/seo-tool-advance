package controllers

import (
	"fmt"
	"html"

	"seo-crawler/internal/models"
)

// emailShell wraps email body content in minimal, email-client-safe HTML —
// inline styles only, no external CSS/fonts, matching the constraints of
// how mail clients actually render markup.
func emailShell(title, bodyHTML string) string {
	return fmt.Sprintf(`<!doctype html>
<html><body style="margin:0;padding:0;background:#0f1117;font-family:-apple-system,Helvetica,Arial,sans-serif;">
  <table role="presentation" width="100%%" cellpadding="0" cellspacing="0" style="background:#0f1117;padding:32px 0;">
    <tr><td align="center">
      <table role="presentation" width="480" cellpadding="0" cellspacing="0" style="background:#161920;border-radius:12px;overflow:hidden;">
        <tr><td style="background:linear-gradient(90deg,#3b82f6,#a855f7);padding:20px 28px;">
          <span style="color:#fff;font-size:18px;font-weight:800;">Auditly</span>
        </td></tr>
        <tr><td style="padding:28px;color:#e2e5f1;font-size:14px;line-height:1.6;">
          %s
        </td></tr>
        <tr><td style="padding:16px 28px;border-top:1px solid #2a2e3d;color:#5a6080;font-size:11px;">
          %s
        </td></tr>
      </table>
    </td></tr>
  </table>
</body></html>`, bodyHTML, html.EscapeString(title))
}

func welcomeEmailHTML(name string) string {
	body := fmt.Sprintf(`
    <h2 style="margin:0 0 12px;color:#fff;font-size:20px;">Welcome to Auditly, %s! 👋</h2>
    <p style="margin:0 0 12px;">Your account is ready. Auditly crawls your sitemap, scores every page out of 100, and surfaces actionable fixes for titles, meta tags, headings, images, speed, broken links, and more.</p>
    <p style="margin:0 0 12px;">Head back to the site and run your first audit whenever you're ready — every report you run while signed in is saved to your account, and you can revisit it any time from your Reports page.</p>
    <p style="margin:0;color:#8891a8;font-size:12px;">If you didn't create this account, you can safely ignore this email.</p>`,
		html.EscapeString(name))
	return emailShell("Auditly · Free SEO Audit Tool", body)
}

func reportEmailHTML(job *models.Job) string {
	avg := averageScore(job.Results)
	grade, gradeColor := scoreGrade(avg)

	issuesLine := ""
	if job.Summary != nil {
		issuesLine = fmt.Sprintf(`<p style="margin:0 0 12px;">%d broken link(s), %d orphan page(s), and %d thin-content page(s) were also found — see the attached PDF for the full breakdown.</p>`,
			job.Summary.BrokenLinks.TotalBrokenLinks, len(job.Summary.OrphanPages), job.Summary.ThinContent.Count)
	}

	body := fmt.Sprintf(`
    <h2 style="margin:0 0 12px;color:#fff;font-size:20px;">Your SEO report is ready</h2>
    <p style="margin:0 0 16px;">The crawl of <strong>%s</strong> (%d page%s) is complete.</p>
    <table role="presentation" cellpadding="0" cellspacing="0" style="margin:0 0 16px;">
      <tr>
        <td style="background:%s;color:#fff;font-weight:800;border-radius:10px;padding:10px 16px;text-align:center;">
          <div style="font-size:20px;line-height:1;">%s</div>
          <div style="font-size:10px;font-weight:600;">%d/100</div>
        </td>
      </tr>
    </table>
    %s
    <p style="margin:0;">The full report is attached as a PDF.</p>`,
		html.EscapeString(job.Domain), len(job.Results), pluralS(len(job.Results)),
		gradeColor, grade, avg, issuesLine)
	return emailShell("Auditly · SEO Report", body)
}

func pluralS(n int) string {
	if n == 1 {
		return ""
	}
	return "s"
}
