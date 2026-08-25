package main

import (
	"context"
	"embed"
	"log"
	"net/http"
	"os"
	"time"

	"seo-crawler/internal/controllers"
	"seo-crawler/internal/db"
	"seo-crawler/internal/routes"
	"seo-crawler/internal/store"

	"github.com/joho/godotenv"
)

//go:embed views/*.html
var viewsFS embed.FS

func main() {
	// Load local .env file if present
	if err := godotenv.Load(); err != nil {
		log.Println("No .env file found or error loading it, using default/system environment variables")
	}

	mongoURI := os.Getenv("MONGO_URI")
	if mongoURI == "" {
		mongoURI = "mongodb://localhost:27017"
	}

	client, err := db.Connect(mongoURI)
	if err != nil {
		log.Fatalf("failed to connect to database: %v", err)
	}
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := client.Disconnect(ctx); err != nil {
			log.Printf("failed to close database client: %v", err)
		}
	}()

	st, err := store.New(client)
	if err != nil {
		log.Fatalf("failed to initialize report store: %v", err)
	}

	pKey := []byte(os.Getenv("PASETO_SYMMETRIC_KEY"))
	if len(pKey) == 0 {
		pKey = []byte("yellow-submarine-yellow-submarine")
	} else if len(pKey) < 32 {
		padded := make([]byte, 32)
		copy(padded, pKey)
		pKey = padded
	} else if len(pKey) > 32 {
		pKey = pKey[:32]
	}

	ctrl := controllers.NewController(st, pKey, viewsFS)
	router := routes.SetupRoutes(ctrl)

	log.Printf("SEO Analyser running at http://localhost:8081")
	log.Fatal(http.ListenAndServe(":8081", router))
}
