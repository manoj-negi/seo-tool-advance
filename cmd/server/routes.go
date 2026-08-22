package main

import "net/http"

func (s *Server) setupRoutes() *http.ServeMux {
	mux := http.NewServeMux()

	// Serve embedded UI
	mux.HandleFunc("/", s.handleHomepage)
	mux.HandleFunc("/seo-report", s.handleSeoReport)
	mux.HandleFunc("/reports", s.handleReports)
	mux.HandleFunc("/api/analyse", cors(s.handleAnalyse))
	mux.HandleFunc("/api/status", cors(s.handleStatus))
	mux.HandleFunc("/api/results", cors(s.handleResults))
	mux.HandleFunc("/api/reports", cors(s.handleReportsList))

	// Optional: serve static assets
	//mux.Handle("/static/", http.FileServer(http.FS(staticFiles)))

	return mux
}
