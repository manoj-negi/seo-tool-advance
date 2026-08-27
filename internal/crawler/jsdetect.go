package crawler

import "strings"

// spaMarkers are DOM fragments commonly left behind by client-side rendering
// frameworks in their otherwise-empty initial HTML shell.
var spaMarkers = []string{
	`id="root"`, `id="app"`, `id="__next"`, `data-reactroot`,
	`ng-version`, `<next-route-announcer`, `id="___gatsby"`,
}

// looksLikeEmptyJSShell heuristically detects a client-rendered page whose
// real content never appears in the raw HTML fetched over plain HTTP — a
// near-empty page plus markers common to SPA frameworks (React/Vue/Next.js/
// Angular/Gatsby mount points). wordCount and title come from the initial
// goquery parse of the raw fetch; rawHTML is that same raw response body.
func looksLikeEmptyJSShell(title string, wordCount int, rawHTML string) bool {
	if wordCount > 20 {
		return false // clearly has real, server-rendered text content
	}

	lower := strings.ToLower(rawHTML)
	for _, marker := range spaMarkers {
		if strings.Contains(lower, marker) {
			return true
		}
	}

	// No recognized framework marker, but a near-empty body plus a script
	// tag is still a reasonable signal that content is injected by JS.
	return title == "" && wordCount < 10 && strings.Contains(lower, "<script")
}
