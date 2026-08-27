package render

import (
	"encoding/json"
	"testing"
)

// TestCWVMetricsJSONMapping guards against a real bug that shipped once:
// CWVMetrics had no json struct tags, so encoding/json's case-insensitive
// field matching mapped "cls" -> CLS correctly but silently failed to map
// "lcp" -> LCPMs (the extra "Ms" breaks the match) — LCPMs stayed at its
// zero value with no error at all. Caught only by manually driving a real
// browser and noticing LCP was always exactly 0.
func TestCWVMetricsJSONMapping(t *testing.T) {
	raw := `{"lcp":328.5,"cls":0.0551}`
	var m CWVMetrics
	if err := json.Unmarshal([]byte(raw), &m); err != nil {
		t.Fatalf("unmarshal failed: %v", err)
	}
	if m.LCPMs != 328.5 {
		t.Errorf("LCPMs = %v, want 328.5", m.LCPMs)
	}
	if m.CLS != 0.0551 {
		t.Errorf("CLS = %v, want 0.0551", m.CLS)
	}
}
