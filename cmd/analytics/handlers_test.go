package main

import (
	"bytes"
	"log"
	"net/http"
	"net/http/httptest"
	"io"
	"strings"
	"testing"

	"analytics.etin.dev/internal/data"
	"analytics.etin.dev/internal/privacy"
	"github.com/DATA-DOG/go-sqlmock"
)

func TestTrackerAndCollectHandlers(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock error: %v", err)
	}
	defer db.Close()

	app := &application{
		config: config{
			defaultSiteID: "etin.dev",
		},
		logger:    log.New(io.Discard, "", 0),
		store:     data.NewAnalyticsStore(db),
		hasher:    privacy.NewDailyHasher(),
		verifier:  newTokenVerifier("https://api.etin.dev", "test-secret-key", http.DefaultClient),
		trackerJS: embeddedTrackerJS,
	}

	mux := app.routes()

	// 1. Verify GET /tracker.js serves the embedded script
	req := httptest.NewRequest(http.MethodGet, "/tracker.js", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 for /tracker.js, got %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "etinAnalytics") {
		t.Fatalf("expected tracker.js to expose window.etinAnalytics")
	}

	// 2. Verify POST /v1/collect records a pageview
	mock.ExpectExec("INSERT INTO pageviews").WillReturnResult(sqlmock.NewResult(1, 1))

	body := bytes.NewBufferString(`{"pageviewId":"22222222-2222-4222-8222-222222222222","path":"/notes/hello-world","referrer":"https://github.com/obasekietinosa"}`)
	req = httptest.NewRequest(http.MethodPost, "/v1/collect", body)
	req.Header.Set("User-Agent", "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) Safari/605.1.15")
	req.Header.Set("Content-Type", "application/json")
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusAccepted {
		t.Fatalf("expected 202 for /v1/collect, got %d: %s", rec.Code, rec.Body.String())
	}

	// 3. Verify GET /v1/stats/overview requires admin token
	req = httptest.NewRequest(http.MethodGet, "/v1/stats/overview", nil)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 without token, got %d", rec.Code)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet sqlmock expectations: %v", err)
	}
}
