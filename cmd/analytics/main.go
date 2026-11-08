package main

import (
	"context"
	"database/sql"
	_ "embed"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"strings"
	"time"

	"analytics.etin.dev/internal/data"
	"analytics.etin.dev/internal/privacy"
	_ "github.com/lib/pq"
)

//go:embed tracker.js
var embeddedTrackerJS []byte

type config struct {
	port          int
	env           string
	dsn           string
	defaultSiteID string
	apiBaseURL    string
	staticAPIKey  string
	cors          struct {
		trustedOrigins []string
	}
}

type application struct {
	config    config
	logger    *log.Logger
	store     *data.AnalyticsStore
	hasher    *privacy.DailyHasher
	verifier  *tokenVerifier
	trackerJS []byte
}

func main() {
	var cfg config
	var corsTrustedOrigins string

	flag.IntVar(&cfg.port, "port", 4001, "Analytics server port")
	flag.StringVar(&cfg.env, "env", "dev", "Environment (dev|stage|prod)")
	flag.StringVar(&cfg.dsn, "dsn", envOrDefault("ANALYTICS_DB_DSN", os.Getenv("WEBSITE_DB_DSN")), "PostgreSQL DSN")
	flag.StringVar(&cfg.defaultSiteID, "site-id", envOrDefault("ANALYTICS_SITE_ID", "etin.dev"), "Default site identifier")
	flag.StringVar(&cfg.apiBaseURL, "api-base-url", envOrDefault("ANALYTICS_AUTH_API_URL", "https://api.etin.dev"), "Base URL of api.etin.dev for admin token verification")
	flag.StringVar(&cfg.staticAPIKey, "api-key", os.Getenv("ANALYTICS_API_KEY"), "Optional static API key for reading stats")
	flag.StringVar(&corsTrustedOrigins, "cors-trusted-origins", os.Getenv("ANALYTICS_CORS_TRUSTED_ORIGINS"), "Space or comma separated list of trusted CORS origins")
	flag.Parse()

	logger := log.New(os.Stdout, "", log.Ldate|log.Ltime)

	cfg.cors.trustedOrigins = parseTrustedOrigins(corsTrustedOrigins)
	if len(cfg.cors.trustedOrigins) == 0 {
		cfg.cors.trustedOrigins = []string{
			"https://etin.dev",
			"https://www.etin.dev",
			"https://admin.etin.dev",
			"http://localhost:4321",
			"http://localhost:5173",
		}
	}

	if cfg.dsn == "" {
		logger.Fatal("PostgreSQL DSN must be provided via -dsn or ANALYTICS_DB_DSN")
	}

	db, err := openDB(cfg.dsn)
	if err != nil {
		logger.Fatal(err)
	}
	defer db.Close()

	store := data.NewAnalyticsStore(db)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	if err := store.EnsureSchema(ctx); err != nil {
		cancel()
		logger.Fatalf("failed to ensure analytics schema: %v", err)
	}
	cancel()

	logger.Printf("analytics database connection pool and schema ready")

	app := &application{
		config:    cfg,
		logger:    logger,
		store:     store,
		hasher:    privacy.NewDailyHasher(),
		verifier:  newTokenVerifier(cfg.apiBaseURL, cfg.staticAPIKey, &http.Client{Timeout: 5 * time.Second}),
		trackerJS: embeddedTrackerJS,
	}

	// Background daily rollup ticker (runs every 6 hours to keep daily_page_stats fresh)
	go func() {
		ticker := time.NewTicker(6 * time.Hour)
		defer ticker.Stop()
		for range ticker.C {
			rollupCtx, rollupCancel := context.WithTimeout(context.Background(), 30*time.Second)
			_ = app.store.RollupDay(rollupCtx, app.config.defaultSiteID, time.Now().UTC().AddDate(0, 0, -1))
			_ = app.store.RollupDay(rollupCtx, app.config.defaultSiteID, time.Now().UTC())
			rollupCancel()
		}
	}()

	addr := fmt.Sprintf(":%d", cfg.port)
	srv := &http.Server{
		Addr:         addr,
		Handler:      app.routes(),
		IdleTimeout:  time.Minute,
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 15 * time.Second,
	}

	logger.Printf("starting %s analytics server on %s", cfg.env, addr)
	if err := srv.ListenAndServe(); err != nil {
		logger.Fatal(err)
	}
}

func openDB(dsn string) (*sql.DB, error) {
	db, err := sql.Open("postgres", dsn)
	if err != nil {
		return nil, err
	}
	if err = db.Ping(); err != nil {
		return nil, err
	}
	return db, nil
}

func envOrDefault(key, fallback string) string {
	if val := strings.TrimSpace(os.Getenv(key)); val != "" {
		return val
	}
	return fallback
}

func parseTrustedOrigins(input string) []string {
	if input == "" {
		return nil
	}
	fields := strings.FieldsFunc(input, func(r rune) bool {
		return r == ',' || r == ' ' || r == '\n' || r == '\t'
	})
	var origins []string
	for _, f := range fields {
		if trimmed := strings.TrimRight(strings.TrimSpace(f), "/"); trimmed != "" {
			origins = append(origins, strings.ToLower(trimmed))
		}
	}
	return origins
}
