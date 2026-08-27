package controllers

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"seo-crawler/internal/models"
)

func TestCheckLinkBroken_HeadRejectedButGetWorks(t *testing.T) {
	// Reproduces the exact reported bug: a server that mishandles HEAD
	// (returns 404) but works fine over GET — which is what actually
	// happens when a person clicks the link in a browser. This must NOT be
	// reported as broken.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodHead {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("real page content"))
	}))
	defer srv.Close()

	client := &http.Client{Timeout: 3 * time.Second}
	bl := checkLinkBroken(context.Background(), client, srv.URL)
	if bl != nil {
		t.Fatalf("expected link to be reported fine (GET works), got broken: %+v", bl)
	}
}

func TestCheckLinkBroken_ActuallyBroken(t *testing.T) {
	// Both HEAD and GET 404 — this link really is broken.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	client := &http.Client{Timeout: 3 * time.Second}
	bl := checkLinkBroken(context.Background(), client, srv.URL)
	if bl == nil {
		t.Fatal("expected link to be reported broken (both HEAD and GET 404), got nil")
	}
	if bl.StatusCode != http.StatusNotFound {
		t.Errorf("expected StatusCode 404, got %d", bl.StatusCode)
	}
}

func TestCheckLinkBroken_HealthyLink(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	client := &http.Client{Timeout: 3 * time.Second}
	bl := checkLinkBroken(context.Background(), client, srv.URL)
	if bl != nil {
		t.Fatalf("expected healthy link to report nil, got: %+v", bl)
	}
}

func TestCheckLinkBroken_ExpiredBudgetNotMisreported(t *testing.T) {
	// Reproduces the other reported bug: once our own overall link-check
	// time budget has run out, an in-flight/attempted check must be
	// skipped silently, never recorded as "broken".
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // simulate the job-wide budget already having expired

	client := &http.Client{Timeout: 3 * time.Second}
	bl := checkLinkBroken(ctx, client, srv.URL)
	if bl != nil {
		t.Fatalf("expected nil when ctx already expired (must not misreport), got: %+v", bl)
	}
}

func TestEvictFinishedJobs(t *testing.T) {
	now := time.Now()
	old := now.Add(-2 * time.Hour)
	recent := now.Add(-1 * time.Minute)

	c := &Controller{Jobs: map[string]*models.Job{
		"old-complete":    {ID: "old-complete", Status: "complete", CreatedAt: old, CompletedAt: &old},
		"recent-complete": {ID: "recent-complete", Status: "complete", CreatedAt: recent, CompletedAt: &recent},
		"old-error":       {ID: "old-error", Status: "error", CreatedAt: old, CompletedAt: &old},
		"old-but-running": {ID: "old-but-running", Status: "analysing", CreatedAt: old}, // never evict in-progress jobs
		"old-no-completed-at": {ID: "old-no-completed-at", Status: "error", CreatedAt: old}, // falls back to CreatedAt
	}}

	c.evictFinishedJobs(time.Hour)

	if _, exists := c.Jobs["old-complete"]; exists {
		t.Error("expected old-complete to be evicted")
	}
	if _, exists := c.Jobs["old-error"]; exists {
		t.Error("expected old-error to be evicted")
	}
	if _, exists := c.Jobs["old-no-completed-at"]; exists {
		t.Error("expected old-no-completed-at to be evicted (falls back to CreatedAt)")
	}
	if _, exists := c.Jobs["recent-complete"]; !exists {
		t.Error("expected recent-complete to survive (not old enough yet)")
	}
	if _, exists := c.Jobs["old-but-running"]; !exists {
		t.Error("expected old-but-running to survive (still in progress, must never be evicted)")
	}
}

func TestCheckLinkBroken_ConnectionError(t *testing.T) {
	// A genuinely unreachable host (nothing listening) should still be
	// reported broken when the budget has NOT expired.
	client := &http.Client{Timeout: 1 * time.Second}
	bl := checkLinkBroken(context.Background(), client, "http://127.0.0.1:1")
	if bl == nil {
		t.Fatal("expected a genuinely unreachable link to be reported broken")
	}
}
