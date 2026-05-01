package crawler

import "net/http"

// FetchResult holds the raw fetch response
type FetchResult struct {
	Content     []byte
	StatusCode  int
	Headers     http.Header
	LoadTimeMs  int64
	ContentType string
	Error       error
}
