package crawler

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"seo-crawler/internal/netguard"
)

// robotsRule represents a single Allow or Disallow directive.
type robotsRule struct {
	path  string
	allow bool // true = Allow, false = Disallow
}

// RobotsData holds parsed rules for one or more user-agent groups.
type RobotsData struct {
	rules     []robotsRule // ordered as they appear in the file
	crawlable bool         // false if the whole site is blocked
}

// IsAllowed returns true if the given path is crawlable by our bot.
// It follows the standard "longest matching prefix wins" logic.
func (rd *RobotsData) IsAllowed(path string) bool {
	if !rd.crawlable {
		return false
	}
	if path == "" {
		path = "/"
	}

	// Find the longest matching rule prefix
	bestLen := -1
	bestAllow := true // default: allow if no rule matches

	for _, r := range rd.rules {
		if !strings.HasPrefix(path, r.path) {
			continue
		}
		if len(r.path) > bestLen {
			bestLen = len(r.path)
			bestAllow = r.allow
		}
	}
	return bestAllow
}

// FetchRobots downloads and parses robots.txt for the given base URL.
// Returns a permissive RobotsData if the file is missing or unreachable.
func FetchRobots(ctx context.Context, baseURL string, userAgent string) *RobotsData {
	permissive := &RobotsData{crawlable: true}

	u, err := url.Parse(baseURL)
	if err != nil {
		return permissive
	}
	robotsURL := fmt.Sprintf("%s://%s/robots.txt", u.Scheme, u.Host)
	if err := netguard.CheckURL(robotsURL); err != nil {
		return permissive
	}

	ctx2, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx2, "GET", robotsURL, nil)
	if err != nil {
		return permissive
	}
	req.Header.Set("User-Agent", userAgent)

	client := &http.Client{CheckRedirect: netguard.CheckRedirect}
	resp, err := client.Do(req)
	if err != nil || resp.StatusCode != http.StatusOK {
		return permissive
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 512*1024)) // 512 KB limit
	if err != nil {
		return permissive
	}

	return parseRobots(string(body), userAgent)
}

// parseRobots parses the robots.txt content and extracts rules that apply
// to our user-agent or the wildcard "*" agent.
func parseRobots(content string, userAgent string) *RobotsData {
	rd := &RobotsData{crawlable: true}

	// Normalise our user-agent to lowercase for matching
	uaLower := strings.ToLower(userAgent)
	// Extract just the product token (first word before "/")
	if idx := strings.Index(uaLower, "/"); idx != -1 {
		uaLower = uaLower[:idx]
	}
	// Trim common bot suffix patterns like "(compatible; botname"
	if idx := strings.Index(uaLower, "("); idx != -1 {
		uaLower = uaLower[:idx]
	}
	uaLower = strings.TrimSpace(uaLower)

	type group struct {
		agents []string
		rules  []robotsRule
	}

	var groups []group
	var current *group

	scanner := bufio.NewScanner(strings.NewReader(content))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())

		// Strip inline comments
		if idx := strings.Index(line, "#"); idx != -1 {
			line = strings.TrimSpace(line[:idx])
		}
		if line == "" {
			// Blank line ends the current group
			if current != nil {
				groups = append(groups, *current)
				current = nil
			}
			continue
		}

		parts := strings.SplitN(line, ":", 2)
		if len(parts) != 2 {
			continue
		}
		key := strings.ToLower(strings.TrimSpace(parts[0]))
		val := strings.TrimSpace(parts[1])

		switch key {
		case "user-agent":
			if current == nil {
				current = &group{}
			}
			current.agents = append(current.agents, strings.ToLower(val))
		case "disallow":
			if current != nil && val != "" {
				current.rules = append(current.rules, robotsRule{path: val, allow: false})
			}
		case "allow":
			if current != nil && val != "" {
				current.rules = append(current.rules, robotsRule{path: val, allow: true})
			}
		}
	}
	// Flush last group
	if current != nil {
		groups = append(groups, *current)
	}

	// Select rules: prefer specific agent match over wildcard "*"
	var specificRules []robotsRule
	var wildcardRules []robotsRule

	for _, g := range groups {
		for _, agent := range g.agents {
			if agent == "*" {
				wildcardRules = append(wildcardRules, g.rules...)
			} else if strings.Contains(agent, uaLower) || strings.Contains(uaLower, agent) {
				specificRules = append(specificRules, g.rules...)
			}
		}
	}

	if len(specificRules) > 0 {
		rd.rules = specificRules
	} else {
		rd.rules = wildcardRules
	}

	// Check for full block: Disallow: /
	for _, r := range rd.rules {
		if !r.allow && r.path == "/" {
			rd.crawlable = false
			break
		}
	}

	return rd
}
