// backend/internal/crawler/config.go
package crawler

import "time"

type Config struct {
	MaxWorkers     int
	RequestTimeout time.Duration
	MaxRetries     int
	RetryDelay     time.Duration
	PoliteDelay    time.Duration
	MaxPages       int
	UserAgent      string
}

func DefaultConfig() Config {
	return Config{
		MaxWorkers:     20,
		RequestTimeout: 15 * time.Second,
		MaxRetries:     3,
		RetryDelay:     500 * time.Millisecond,
		PoliteDelay:    500 * time.Millisecond, // Increased to avoid rate limits/500s
		MaxPages:       50,
		UserAgent:      "Mozilla/5.0 (compatible; SEOAnalyser/2.0; +https://seoanalyser.io)",
	}
}
