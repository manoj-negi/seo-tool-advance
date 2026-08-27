package routes

import (
	"net/http"
	"seo-crawler/internal/controllers"
	"seo-crawler/internal/middleware"
)

// corsMiddleware builds a CORS wrapper restricted to an explicit origin
// allowlist. Reflecting "*" back with credentialed (cookie) requests is
// unsafe practice even though browsers won't actually attach credentials to
// a wildcard response — so instead we only ever echo back a specific Origin
// that's on the allowlist, alongside Allow-Credentials. With no allowlist
// configured (the default), no CORS headers are sent at all, which is
// correct for this app since the UI is served same-origin.
func corsMiddleware(allowedOrigins []string) func(http.HandlerFunc) http.HandlerFunc {
	allowed := make(map[string]bool, len(allowedOrigins))
	for _, o := range allowedOrigins {
		allowed[o] = true
	}

	return func(next http.HandlerFunc) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			origin := r.Header.Get("Origin")
			if origin != "" && allowed[origin] {
				w.Header().Set("Access-Control-Allow-Origin", origin)
				w.Header().Set("Access-Control-Allow-Credentials", "true")
				w.Header().Set("Vary", "Origin")
			}
			w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
			w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
			if r.Method == "OPTIONS" {
				w.WriteHeader(200)
				return
			}
			next(w, r)
		}
	}
}

func SetupRoutes(ctrl *controllers.Controller, allowedOrigins []string) *http.ServeMux {
	mux := http.NewServeMux()

	loginLimiter := middleware.NewLoginRateLimiter()
	cors := corsMiddleware(allowedOrigins)

	// Serve embedded UI
	mux.HandleFunc("/", ctrl.HandleHomepage)
	mux.HandleFunc("/seo-report", ctrl.HandleSeoReport)
	mux.HandleFunc("/reports", ctrl.HandleReports)

	// API endpoints
	mux.HandleFunc("/api/analyse", cors(ctrl.HandleAnalyse))
	mux.HandleFunc("/api/status", cors(ctrl.HandleStatus))
	mux.HandleFunc("/api/results", cors(ctrl.HandleResults))
	mux.HandleFunc("/api/reports", cors(ctrl.HandleReportsList))
	mux.HandleFunc("/api/auth/signup", cors(ctrl.HandleSignup))
	mux.HandleFunc("/api/auth/login", cors(loginLimiter.Middleware(ctrl.HandleLogin)))
	mux.HandleFunc("/api/auth/logout", cors(ctrl.HandleLogout))
	mux.HandleFunc("/api/auth/refresh", cors(ctrl.HandleRefresh))

	return mux
}
