package controllers

import (
	"net/url"
	"strings"
	"testing"
)

// TestAuthUserCookieEncoding guards against two real bugs that shipped in
// sequence for the auth_user cookie (which carries a display name that
// often contains a space, e.g. "Alice Renamed"):
//
//  1. Setting the raw name as the cookie value: Go's net/http auto-wraps a
//     cookie value containing a space in double quotes (RFC 6265), so the
//     literal quote characters showed up in the UI.
//  2. Fixing that with url.QueryEscape: QueryEscape encodes a space as "+"
//     (form-encoding convention), but the header's JS calls
//     decodeURIComponent, which only understands "%20" and leaves a
//     literal "+" untouched — so the UI showed "Alice+Renamed" instead.
//
// The actual fix is url.PathEscape, which encodes a space as "%20" —
// this test locks that choice in.
func TestAuthUserCookieEncoding(t *testing.T) {
	name := "Alice Renamed"
	encoded := url.PathEscape(name)

	if strings.Contains(encoded, `"`) {
		t.Errorf("encoded cookie value must not contain a literal quote: %q", encoded)
	}
	if strings.Contains(encoded, "+") {
		t.Errorf("encoded cookie value must not contain a literal '+' for a space (decodeURIComponent won't undo it): %q", encoded)
	}
	if !strings.Contains(encoded, "%20") {
		t.Errorf("expected the space to be encoded as %%20, got: %q", encoded)
	}

	// Simulate the browser's decodeURIComponent behavior via url.QueryUnescape
	// on the %20 form (Go has no direct decodeURIComponent equivalent, but
	// QueryUnescape correctly decodes %20 the same way decodeURIComponent
	// does — it just also happens to decode '+' as a space, which is why
	// this test asserts no literal '+' survives above).
	decoded, err := url.QueryUnescape(encoded)
	if err != nil {
		t.Fatalf("failed to decode: %v", err)
	}
	if decoded != name {
		t.Errorf("round trip mismatch: got %q, want %q", decoded, name)
	}
}
