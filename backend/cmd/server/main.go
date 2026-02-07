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
	ingestAPIKey := strings.TrimSpace(os.Getenv("INGEST_API_KEY"))
	if ingestAPIKey == "" {
		log.Fatal("INGEST_API_KEY is required")
	}

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

	options := make([]server.APIOption, 0, 1)
	openAIAPIKey := strings.TrimSpace(os.Getenv("OPENAI_API_KEY"))
	if openAIAPIKey != "" {
		insightsModel := envOrDefault("OPENAI_INSIGHTS_MODEL", "gpt-5-mini")
		insightsBaseURL := envOrDefault("OPENAI_BASE_URL", "https://api.openai.com/v1")
		insightsMax := intOrDefault("OPENAI_INSIGHTS_MAX", 4)
		insightsCacheSeconds := intOrDefault("OPENAI_INSIGHTS_CACHE_SECONDS", 30)
		if insightsCacheSeconds < 0 {
			insightsCacheSeconds = 30
		}

		alertAnalyzer := server.NewCachedAlertAnalyzer(
			server.NewOpenAIAlertAnalyzer(openAIAPIKey, insightsModel, insightsBaseURL, insightsMax),
			time.Duration(insightsCacheSeconds)*time.Second,
		)
		options = append(options, server.WithAlertAnalyzer(alertAnalyzer))
		log.Printf("ai insights enabled model=%s", insightsModel)
	} else {
		log.Printf("ai insights disabled (set OPENAI_API_KEY to enable)")
	}

	api := server.NewAPI(store, ingestAPIKey, options...)

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
		response.Header().Set("Access-Control-Allow-Headers", "Content-Type,X-API-Key")

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
