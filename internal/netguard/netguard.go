// Package netguard protects outbound crawler requests from SSRF: a user
// supplies an arbitrary "domain" to crawl, and without this check the server
// would happily fetch (and reflect back the contents of) internal services,
// loopback addresses, or cloud metadata endpoints like 169.254.169.254.
package netguard

import (
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strings"
)

// maxRedirects mirrors net/http's own default, applied explicitly so every
// caller that sets CheckRedirect gets the same bound.
const maxRedirects = 10

// CheckURL parses rawURL, requires an http/https scheme, resolves its host,
// and rejects it unless every resolved address is a public, routable IP.
func CheckURL(rawURL string) error {
	u, err := url.Parse(rawURL)
	if err != nil {
		return fmt.Errorf("invalid URL: %w", err)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return fmt.Errorf("unsupported URL scheme %q", u.Scheme)
	}

	host := u.Hostname()
	if host == "" {
		return errors.New("URL has no host")
	}
	if strings.EqualFold(host, "localhost") {
		return errors.New("localhost is not an allowed target")
	}

	ips, err := net.LookupIP(host)
	if err != nil {
		return fmt.Errorf("resolve host %q: %w", host, err)
	}
	if len(ips) == 0 {
		return fmt.Errorf("host %q did not resolve to any address", host)
	}
	for _, ip := range ips {
		if !isPublic(ip) {
			return fmt.Errorf("host %q resolves to a non-public address (%s)", host, ip)
		}
	}
	return nil
}

// isPublic excludes loopback, RFC1918/ULA private ranges, link-local
// (which covers the 169.254.169.254 cloud metadata address), and multicast.
func isPublic(ip net.IP) bool {
	switch {
	case ip.IsLoopback(),
		ip.IsPrivate(),
		ip.IsLinkLocalUnicast(),
		ip.IsLinkLocalMulticast(),
		ip.IsUnspecified(),
		ip.IsMulticast():
		return false
	default:
		return true
	}
}

// CheckRedirect is an http.Client.CheckRedirect implementation that applies
// CheckURL to every hop of a redirect chain, so a validated starting URL
// can't be used to redirect the crawler into an internal address afterward.
func CheckRedirect(req *http.Request, via []*http.Request) error {
	if len(via) >= maxRedirects {
		return fmt.Errorf("stopped after %d redirects", maxRedirects)
	}
	return CheckURL(req.URL.String())
}
