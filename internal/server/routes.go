// backend/internal/server/routes.go
package server

import "net/http"

func (s *Server) setupRoutes() {
	http.HandleFunc("/", chain(s.handleIndex, recovery, logging))
	http.HandleFunc("/api/analyse", chain(s.handleAnalyse, recovery, cors, logging))
	http.HandleFunc("/api/status", chain(s.handleStatus, recovery, cors, logging))
	http.HandleFunc("/api/results", chain(s.handleResults, recovery, cors, logging))
}
