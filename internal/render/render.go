// Package render provides a headless-Chrome fallback for pages whose real
// content only exists after client-side JavaScript runs (React/Vue/Next.js
// and similar frameworks). A plain HTTP fetch + HTML parse sees only the
// empty shell those frameworks ship; this package drives an actual browser
// so we can read the DOM after it renders.
package render

import (
	"context"
	"fmt"
	"time"

	"github.com/chromedp/cdproto/page"
	"github.com/chromedp/chromedp"
)

// Renderer wraps a single shared headless Chrome instance. Creating this is
// relatively expensive, so the crawler creates one Renderer for the life of
// the process and reuses it across every page/job that needs it, rather
// than launching a new browser per page or per crawl.
type Renderer struct {
	allocCtx context.Context
	cancel   context.CancelFunc
	sem      chan struct{}
}

// New prepares (but does not yet launch) a shared headless Chrome instance.
// Chrome only actually starts on the first call to Render, so it's safe to
// call New even in environments without Chrome installed — Render will
// simply return an error there, which callers treat as "fall back to the
// plain HTML we already have."
//
// maxConcurrent bounds how many browser tabs may render at once. Headless
// tabs are far more expensive (CPU/memory) than plain HTTP fetches, so this
// is deliberately small and kept independent of the crawler's HTTP worker
// pool (Config.MaxWorkers).
func New(maxConcurrent int) *Renderer {
	opts := append(chromedp.DefaultExecAllocatorOptions[:],
		chromedp.Flag("headless", true),
		chromedp.Flag("disable-gpu", true),
		chromedp.Flag("no-sandbox", true),
		chromedp.Flag("disable-dev-shm-usage", true),
	)
	allocCtx, cancel := chromedp.NewExecAllocator(context.Background(), opts...)
	return &Renderer{
		allocCtx: allocCtx,
		cancel:   cancel,
		sem:      make(chan struct{}, maxConcurrent),
	}
}

// Close shuts down the shared Chrome instance. Call once on server shutdown.
func (r *Renderer) Close() {
	r.cancel()
}

// Render navigates to rawURL in a fresh tab, gives client-side JS a moment
// to fetch data and populate the page, and returns the resulting rendered
// HTML. The returned HTML can be handed to the same parser used for plain
// HTTP fetches.
func (r *Renderer) Render(rawURL string, timeout time.Duration) (string, error) {
	r.sem <- struct{}{}
	defer func() { <-r.sem }()

	tabCtx, cancelTab := chromedp.NewContext(r.allocCtx)
	defer cancelTab()

	tabCtx, cancelTimeout := context.WithTimeout(tabCtx, timeout)
	defer cancelTimeout()

	var html string
	err := chromedp.Run(tabCtx,
		chromedp.Navigate(rawURL),
		// Give post-load JS (data fetches, hydration) a moment to settle.
		// chromedp has no generic "network idle" wait, so a short fixed
		// delay is the pragmatic choice here.
		chromedp.Sleep(1200*time.Millisecond),
		chromedp.OuterHTML("html", &html, chromedp.ByQuery),
	)
	if err != nil {
		return "", fmt.Errorf("render %s: %w", rawURL, err)
	}
	return html, nil
}

// PrintPDF navigates to fileURL (a "file://..." path is expected — the PDF
// export builds a self-contained report HTML file rather than driving the
// live authenticated web app, so there's no session/cookie to carry into
// the headless browser) and returns the page rendered as a PDF.
func (r *Renderer) PrintPDF(fileURL string, timeout time.Duration) ([]byte, error) {
	r.sem <- struct{}{}
	defer func() { <-r.sem }()

	tabCtx, cancelTab := chromedp.NewContext(r.allocCtx)
	defer cancelTab()

	tabCtx, cancelTimeout := context.WithTimeout(tabCtx, timeout)
	defer cancelTimeout()

	var pdfData []byte
	err := chromedp.Run(tabCtx,
		chromedp.Navigate(fileURL),
		chromedp.ActionFunc(func(ctx context.Context) error {
			data, _, err := page.PrintToPDF().
				WithPrintBackground(true).
				WithMarginTop(0.4).WithMarginBottom(0.4).
				WithMarginLeft(0.4).WithMarginRight(0.4).
				Do(ctx)
			if err != nil {
				return err
			}
			pdfData = data
			return nil
		}),
	)
	if err != nil {
		return nil, fmt.Errorf("print pdf %s: %w", fileURL, err)
	}
	return pdfData, nil
}
