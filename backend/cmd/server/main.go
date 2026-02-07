package main

import (
	"bufio"
	"context"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"envirostation/backend/internal/server"
)

func main() {
	loadLocalEnvFiles(".env.local", ".env")

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
		if insightsCacheSeconds < 30 {
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
		// Keep write timeout disabled so long-lived SSE streams can stay open.
		WriteTimeout: 0,
		IdleTimeout:  60 * time.Second,
	}

	log.Printf("ingest service listening on :%s", port)
	if err = httpServer.ListenAndServe(); err != nil {
		log.Fatal(err)
	}
}

func withCORS(allowedOrigin string, next http.Handler) http.Handler {
	allowedOrigins, allowAny := parseAllowedOrigins(allowedOrigin)

	return http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		origin := strings.TrimSpace(request.Header.Get("Origin"))
		if allowAny {
			response.Header().Set("Access-Control-Allow-Origin", "*")
		} else if origin != "" && originAllowed(origin, allowedOrigins) {
			response.Header().Set("Access-Control-Allow-Origin", origin)
			response.Header().Set("Vary", "Origin")
		}

		response.Header().Set("Access-Control-Allow-Methods", "GET,POST,OPTIONS")
		response.Header().Set("Access-Control-Allow-Headers", "Content-Type,X-API-Key")

		if request.Method == http.MethodOptions {
			response.WriteHeader(http.StatusNoContent)
			return
		}

		next.ServeHTTP(response, request)
	})
}

func parseAllowedOrigins(raw string) ([]string, bool) {
	value := strings.TrimSpace(raw)
	if value == "" || value == "*" {
		return nil, true
	}

	parts := strings.Split(value, ",")
	origins := make([]string, 0, len(parts))
	for _, part := range parts {
		trimmed := strings.TrimSpace(part)
		if trimmed == "" {
			continue
		}
		if trimmed == "*" {
			return nil, true
		}
		origins = append(origins, trimmed)
	}

	if len(origins) == 0 {
		return nil, true
	}
	return origins, false
}

func originAllowed(origin string, allowedOrigins []string) bool {
	for _, allowedOrigin := range allowedOrigins {
		if origin == allowedOrigin {
			return true
		}
	}
	return false
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

func loadLocalEnvFiles(paths ...string) {
	for _, path := range paths {
		if err := loadLocalEnvFile(path); err != nil {
			log.Printf("warning: failed to load %s: %v", path, err)
		}
	}
}

func loadLocalEnvFile(path string) error {
	fileHandle, err := os.Open(filepath.Clean(path))
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	defer fileHandle.Close()

	scanner := bufio.NewScanner(fileHandle)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		if strings.HasPrefix(line, "export ") {
			line = strings.TrimSpace(strings.TrimPrefix(line, "export "))
		}

		key, value, found := strings.Cut(line, "=")
		if !found {
			continue
		}

		key = strings.TrimSpace(key)
		if key == "" {
			continue
		}

		if _, alreadySet := os.LookupEnv(key); alreadySet {
			continue
		}

		value = strings.TrimSpace(value)
		value = strings.Trim(value, "\"'")
		if err = os.Setenv(key, value); err != nil {
			return err
		}
	}

	return scanner.Err()
}
