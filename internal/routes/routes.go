package routes

import (
	"net/http"
	"seo-crawler/internal/controllers"
)

func cors(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
		if r.Method == "OPTIONS" {
			w.WriteHeader(200)
			return
		}
		next(w, r)
	}
}

func SetupRoutes(ctrl *controllers.Controller) *http.ServeMux {
	mux := http.NewServeMux()

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
	mux.HandleFunc("/api/auth/login", cors(ctrl.HandleLogin))
	mux.HandleFunc("/api/auth/logout", cors(ctrl.HandleLogout))

	return mux
}
