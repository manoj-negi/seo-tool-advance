// backend/internal/models/types.go
package models

import "time"

type SEOResult struct {
	URL                   string              `json:"url"`
	StatusCode            int                 `json:"status_code"`
	LoadTimeMs            int64               `json:"load_time_ms"`
	PageSizeBytes         int                 `json:"page_size_bytes"`
	Title                 string              `json:"title"`
	TitleLength           int                 `json:"title_length"`
	MetaDescription       string              `json:"meta_description"`
	MetaDescriptionLength int                 `json:"meta_description_length"`
	MetaKeywords          string              `json:"meta_keywords"`
	MetaViewport          string              `json:"meta_viewport"`
	Canonical             string              `json:"canonical"`
	RobotsMeta            string              `json:"robots_meta"`
	Headings              map[string][]string `json:"headings"`
	Images                []ImageData         `json:"images"`
	ImageStats            ImageStats          `json:"image_stats"`
	Links                 []LinkData          `json:"links"`
	LinkStats             LinkStats           `json:"link_stats"`
	WordCount             int                 `json:"word_count"`
	OGTags                map[string]string   `json:"og_tags"`
	SchemaTypes           []string            `json:"schema_types"`
	HasHTTPS              bool                `json:"has_https"`
	ContentType           string              `json:"content_type"`
	XFrameOptions           string              `json:"x_frame_options"`
	ContentSecurity         string              `json:"content_security"`
	StrictTransportSecurity string              `json:"strict_transport_security"`
	XContentTypeOptions     string              `json:"x_content_type_options"`
	ReferrerPolicy          string              `json:"referrer_policy"`
	PermissionsPolicy       string              `json:"permissions_policy"`
	Issues                []CheckResult       `json:"issues"`
	Warnings              []CheckResult       `json:"warnings"`
	Passed                []CheckResult       `json:"passed"`
	Score                 int                 `json:"score"`
	Error                 string              `json:"error,omitempty"`
}

type ImageData struct {
	Src            string `json:"src"`
	Alt            string `json:"alt"`
	HasAlt         bool   `json:"has_alt"`
	HasDimensions  bool   `json:"has_dimensions"`
	HasTitle       bool   `json:"has_title"`
	LazyLoaded     bool   `json:"lazy_loaded"`
	Width          string `json:"width"`
	Height         string `json:"height"`
	Filename       string `json:"filename"`
	Format         string `json:"format"`
	IsModernFormat bool   `json:"is_modern_format"`
}

type ImageStats struct {
	Total             int `json:"total"`
	MissingAlt        int `json:"missing_alt"`
	MissingDimensions int `json:"missing_dimensions"`
	NotLazy           int `json:"not_lazy"`
	NotModernFormat   int `json:"not_modern_format"`
	ModernFormat      int `json:"modern_format"`
}

type LinkData struct {
	Href     string `json:"href"`
	Rel      string `json:"rel"`
	Nofollow bool   `json:"nofollow"`
}

type LinkStats struct {
	Total    int `json:"total"`
	Internal int `json:"internal"`
	External int `json:"external"`
	Nofollow int `json:"nofollow"`
}

type CheckResult struct {
	Msg    string `json:"msg"`
	Points int    `json:"points"`
}

type Job struct {
	ID          string      `json:"job_id"`
	Status      string      `json:"status"`
	Domain      string      `json:"domain"`
	Progress    int         `json:"progress"`
	Total       int         `json:"total"`
	SitemapURL  string      `json:"sitemap_url"`
	URLs        []string    `json:"urls"`
	Results     []SEOResult `json:"results"`
	Error       string      `json:"error,omitempty"`
	CreatedAt   time.Time   `json:"created_at"`
	CompletedAt *time.Time  `json:"completed_at,omitempty"`
}
