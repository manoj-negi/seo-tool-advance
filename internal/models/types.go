// backend/internal/models/types.go
package models

import "time"

type SEOResult struct {
	URL                     string              `json:"url"                       bson:"url"`
	StatusCode              int                 `json:"status_code"               bson:"status_code"`
	LoadTimeMs              int64               `json:"load_time_ms"              bson:"load_time_ms"`
	PageSizeBytes           int                 `json:"page_size_bytes"           bson:"page_size_bytes"`
	Title                   string              `json:"title"                     bson:"title"`
	TitleLength             int                 `json:"title_length"              bson:"title_length"`
	MetaDescription         string              `json:"meta_description"          bson:"meta_description"`
	MetaDescriptionLength   int                 `json:"meta_description_length"   bson:"meta_description_length"`
	MetaKeywords            string              `json:"meta_keywords"             bson:"meta_keywords"`
	MetaViewport            string              `json:"meta_viewport"             bson:"meta_viewport"`
	Canonical               string              `json:"canonical"                 bson:"canonical"`
	RobotsMeta              string              `json:"robots_meta"               bson:"robots_meta"`
	Headings                map[string][]string `json:"headings"                  bson:"headings"`
	Images                  []ImageData         `json:"images"                    bson:"images"`
	ImageStats              ImageStats          `json:"image_stats"               bson:"image_stats"`
	Links                   []LinkData          `json:"links"                     bson:"links"`
	LinkStats               LinkStats           `json:"link_stats"                bson:"link_stats"`
	WordCount               int                 `json:"word_count"                bson:"word_count"`
	OGTags                  map[string]string   `json:"og_tags"                   bson:"og_tags"`
	SchemaTypes             []string            `json:"schema_types"              bson:"schema_types"`
	HasHTTPS                bool                `json:"has_https"                 bson:"has_https"`
	ContentType             string              `json:"content_type"              bson:"content_type"`
	XFrameOptions           string              `json:"x_frame_options"           bson:"x_frame_options"`
	ContentSecurity         string              `json:"content_security"          bson:"content_security"`
	StrictTransportSecurity string              `json:"strict_transport_security" bson:"strict_transport_security"`
	XContentTypeOptions     string              `json:"x_content_type_options"    bson:"x_content_type_options"`
	ReferrerPolicy          string              `json:"referrer_policy"           bson:"referrer_policy"`
	PermissionsPolicy       string              `json:"permissions_policy"        bson:"permissions_policy"`
	Issues                  []CheckResult       `json:"issues"                    bson:"issues"`
	Warnings                []CheckResult       `json:"warnings"                  bson:"warnings"`
	Passed                  []CheckResult       `json:"passed"                    bson:"passed"`
	Score                   int                 `json:"score"                     bson:"score"`
	RobotsBlocked           bool                `json:"robots_blocked,omitempty"  bson:"robots_blocked,omitempty"`
	BrokenLinks             []BrokenLink        `json:"broken_links,omitempty"    bson:"broken_links,omitempty"`
	CWVHints                []string            `json:"cwv_hints,omitempty"       bson:"cwv_hints,omitempty"`
	DuplicateOf             string              `json:"duplicate_of,omitempty"    bson:"duplicate_of,omitempty"`
	JSONLDIssues            []string            `json:"jsonld_issues,omitempty"   bson:"jsonld_issues,omitempty"`
	Error                   string              `json:"error,omitempty"           bson:"error,omitempty"`
}

type ImageData struct {
	Src            string `json:"src"             bson:"src"`
	Alt            string `json:"alt"             bson:"alt"`
	HasAlt         bool   `json:"has_alt"         bson:"has_alt"`
	HasDimensions  bool   `json:"has_dimensions"  bson:"has_dimensions"`
	HasTitle       bool   `json:"has_title"       bson:"has_title"`
	LazyLoaded     bool   `json:"lazy_loaded"     bson:"lazy_loaded"`
	Width          string `json:"width"           bson:"width"`
	Height         string `json:"height"          bson:"height"`
	Filename       string `json:"filename"        bson:"filename"`
	Format         string `json:"format"          bson:"format"`
	IsModernFormat bool   `json:"is_modern_format" bson:"is_modern_format"`
}

type ImageStats struct {
	Total             int `json:"total"              bson:"total"`
	MissingAlt        int `json:"missing_alt"        bson:"missing_alt"`
	MissingDimensions int `json:"missing_dimensions" bson:"missing_dimensions"`
	NotLazy           int `json:"not_lazy"           bson:"not_lazy"`
	NotModernFormat   int `json:"not_modern_format"  bson:"not_modern_format"`
	ModernFormat      int `json:"modern_format"      bson:"modern_format"`
}

type LinkData struct {
	Href     string `json:"href"     bson:"href"`
	Rel      string `json:"rel"      bson:"rel"`
	Nofollow bool   `json:"nofollow" bson:"nofollow"`
}

type LinkStats struct {
	Total    int `json:"total"    bson:"total"`
	Internal int `json:"internal" bson:"internal"`
	External int `json:"external" bson:"external"`
	Nofollow int `json:"nofollow" bson:"nofollow"`
}

type CheckResult struct {
	Msg    string `json:"msg"    bson:"msg"`
	Points int    `json:"points" bson:"points"`
}

type BrokenLink struct {
	Href       string `json:"href"                bson:"href"`
	StatusCode int    `json:"status_code"         bson:"status_code"`
	Error      string `json:"error,omitempty"     bson:"error,omitempty"`
}

type Job struct {
	ID          string      `json:"job_id"`
	UserID      string      `json:"user_id,omitempty"`
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

type User struct {
	ID           string    `bson:"_id,omitempty" json:"id"`
	Name         string    `bson:"name"          json:"name"`
	Email        string    `bson:"email"         json:"email"`
	PasswordHash string    `bson:"password_hash" json:"-"`
	CreatedAt    time.Time `bson:"created_at"    json:"created_at"`
}
