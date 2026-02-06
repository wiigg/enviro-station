package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"envirostation/backend/internal/server"
)

func main() {
	port := envOrDefault("PORT", "8080")
	databaseURL := strings.TrimSpace(os.Getenv("DATABASE_URL"))
	if databaseURL == "" {
		log.Fatal("DATABASE_URL is required")
	}

	setupCtx, cancelSetup := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancelSetup()

	store, err := server.NewPostgresStore(
		setupCtx,
		databaseURL,
		int32(intOrDefault("PG_MAX_CONNS", 10)),
	)
	if err != nil {
		log.Fatalf("create postgres store: %v", err)
	}
	defer store.Close()

	api := server.NewAPI(store)

	handler := withCORS(envOrDefault("CORS_ALLOW_ORIGIN", "*"), api.Handler())

	httpServer := &http.Server{
		Addr:              ":" + port,
		Handler:           handler,
		ReadTimeout:       15 * time.Second,
		ReadHeaderTimeout: 5 * time.Second,
		WriteTimeout:      15 * time.Second,
		IdleTimeout:       60 * time.Second,
	}

	log.Printf("ingest service listening on :%s", port)
	if err = httpServer.ListenAndServe(); err != nil {
		log.Fatal(err)
	}
}

func withCORS(allowedOrigin string, next http.Handler) http.Handler {
	return http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		response.Header().Set("Access-Control-Allow-Origin", allowedOrigin)
		response.Header().Set("Access-Control-Allow-Methods", "GET,POST,OPTIONS")
		response.Header().Set("Access-Control-Allow-Headers", "Content-Type")

		if request.Method == http.MethodOptions {
			response.WriteHeader(http.StatusNoContent)
			return
		}

		next.ServeHTTP(response, request)
	})
}

func envOrDefault(key string, fallback string) string {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return fallback
	}
	return value
}

func intOrDefault(key string, fallback int) int {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return fallback
	}

	parsedValue, err := strconv.Atoi(value)
	if err != nil {
		return fallback
	}
	return parsedValue
}
