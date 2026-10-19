package main

import (
	"errors"
	"log"
	"net/http"
	"os"
	"time"

	"enitime-api/internal/config"
	"enitime-api/internal/database"
	"enitime-api/internal/handlers"

	"github.com/joho/godotenv"
)

func main() {
	if err := godotenv.Load(); err != nil && !errors.Is(err, os.ErrNotExist) {
		log.Fatalf("load environment: %v", err)
	}

	cfg := config.Load()

	db, err := database.ConnectDB()
	if err != nil {
		log.Fatalf("database connection failed: %v", err)
	}
	defer db.Close()
	log.Print("database connection established")

	mux := http.NewServeMux()
	mux.HandleFunc("GET /health", handlers.Health)
	mux.Handle("/", handlers.New(db, cfg))

	server := &http.Server{
		Addr:              ":" + cfg.Port,
		Handler:           handlers.JSONResponses(mux),
		ReadHeaderTimeout: 5 * time.Second,
	}

	log.Printf("API listening on port %s", cfg.Port)
	if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		log.Fatalf("server error: %v", err)
	}
}
