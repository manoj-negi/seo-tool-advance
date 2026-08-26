package main

import (
	"context"
	"embed"
	"errors"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"seo-crawler/internal/config"
	"seo-crawler/internal/controllers"
	"seo-crawler/internal/db"
	"seo-crawler/internal/routes"
	"seo-crawler/internal/store"
)

//go:embed views/*.html
var viewsFS embed.FS

func main() {
	// 1. Centralized Configuration & Startup Validation
	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("Fatal configuration error: %v", err)
	}

	// 2. Connect to MongoDB
	client, err := db.Connect(cfg.MongoURI)
	if err != nil {
		log.Fatalf("Failed to connect to database: %v", err)
	}

	// 3. Initialize Store & Controllers
	st, err := store.New(client)
	if err != nil {
		log.Fatalf("Failed to initialize report store: %v", err)
	}

	ctrl := controllers.NewController(st, cfg.PasetoKey, viewsFS)
	router := routes.SetupRoutes(ctrl)

	// 4. Configure HTTP Server with timeouts
	server := &http.Server{
		Addr:         cfg.Port,
		Handler:      router,
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 60 * time.Second,
		IdleTimeout:  120 * time.Second,
	}

	// 5. Setup Graceful Shutdown with Signal Context
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	// Run HTTP server in background goroutine
	go func() {
		log.Printf("SEO Analyser running at http://localhost%s (env: %s)", cfg.Port, cfg.Environment)
		if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Fatalf("HTTP server error: %v", err)
		}
	}()

	// Wait for OS shutdown signal (Ctrl+C / SIGTERM)
	<-ctx.Done()
	log.Println("Received termination signal. Starting graceful shutdown...")

	// 6. Shutdown HTTP Server gracefully
	shutdownTimeout := time.Duration(cfg.ShutdownTimeoutSec) * time.Second
	shutdownCtx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
	defer cancel()

	if err := server.Shutdown(shutdownCtx); err != nil {
		log.Printf("HTTP server shutdown error: %v", err)
	} else {
		log.Println("HTTP server stopped gracefully.")
	}

	// 7. Disconnect MongoDB client
	dbCtx, dbCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer dbCancel()
	if err := client.Disconnect(dbCtx); err != nil {
		log.Printf("Database disconnect error: %v", err)
	} else {
		log.Println("Database connection closed gracefully.")
	}

	log.Println("SEO Analyser shutdown complete.")
}
