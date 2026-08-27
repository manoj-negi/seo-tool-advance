package crawler

import "testing"

func TestLooksLikeEmptyJSShell(t *testing.T) {
	cases := []struct {
		name      string
		title     string
		wordCount int
		rawHTML   string
		want      bool
	}{
		{
			name:      "normal server-rendered page",
			title:     "About Us",
			wordCount: 400,
			rawHTML:   `<html><body><h1>About Us</h1><p>lots of real text...</p></body></html>`,
			want:      false,
		},
		{
			name:      "react shell with empty root div",
			title:     "",
			wordCount: 0,
			rawHTML:   `<html><body><div id="root"></div><script src="/bundle.js"></script></body></html>`,
			want:      true,
		},
		{
			name:      "next.js shell",
			title:     "",
			wordCount: 2,
			rawHTML:   `<html><body><div id="__next"></div><script src="/_next/static/chunk.js"></script></body></html>`,
			want:      true,
		},
		{
			name:      "thin but genuinely server-rendered page",
			title:     "Contact",
			wordCount: 15,
			rawHTML:   `<html><body><h1>Contact</h1><p>Call us at 555-1234.</p></body></html>`,
			want:      false,
		},
		{
			name:      "no marker but empty title, near-zero words, has a script",
			title:     "",
			wordCount: 3,
			rawHTML:   `<html><body><script src="/app.js"></script></body></html>`,
			want:      true,
		},
		{
			name:      "empty page with no script at all (just broken, not JS-rendered)",
			title:     "",
			wordCount: 0,
			rawHTML:   `<html><body></body></html>`,
			want:      false,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := looksLikeEmptyJSShell(tc.title, tc.wordCount, tc.rawHTML)
			if got != tc.want {
				t.Errorf("looksLikeEmptyJSShell(%q, %d, ...) = %v, want %v", tc.title, tc.wordCount, got, tc.want)
			}
		})
	}
}
