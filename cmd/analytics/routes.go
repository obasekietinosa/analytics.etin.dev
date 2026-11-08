package main

import (
	"net/http"
	"strings"
)

func (app *application) routes() http.Handler {
	mux := http.NewServeMux()

	// Healthcheck & embedded browser collector
	mux.HandleFunc("GET /v1/healthcheck", app.healthcheckHandler)
	mux.HandleFunc("GET /tracker.js", app.trackerScriptHandler)

	// Ingestion endpoints (beacons from www.etin.dev)
	mux.HandleFunc("POST /v1/collect", app.collectPageviewHandler)
	mux.HandleFunc("POST /v1/engagement", app.collectEngagementHandler)
	mux.HandleFunc("POST /v1/events", app.collectEventHandler)

	// Partially public aggregate stats (e.g. view counts for www.etin.dev)
	mux.HandleFunc("GET /v1/public/stats", app.publicStatsHandler)

	// Admin-authenticated stats for admin.etin.dev
	mux.HandleFunc("GET /v1/stats/overview", app.requireAdmin(app.overviewStatsHandler))
	mux.HandleFunc("POST /v1/stats/rollup", app.requireAdmin(app.triggerRollupHandler))

	return app.enableCORS(mux)
}

func (app *application) requireAdmin(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		token, err := parseBearerToken(r.Header.Get("Authorization"))
		if err != nil || !app.verifier.validate(token) {
			app.writeJSON(w, http.StatusUnauthorized, map[string]string{
				"error": "unauthorized",
			})
			return
		}
		next(w, r)
	}
}

func (app *application) enableCORS(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Add("Vary", "Origin")
		w.Header().Add("Vary", "Access-Control-Request-Method")
		w.Header().Add("Vary", "Access-Control-Request-Headers")

		if origin := strings.TrimRight(strings.TrimSpace(r.Header.Get("Origin")), "/"); origin != "" {
			if app.isOriginAllowed(origin) {
				w.Header().Set("Access-Control-Allow-Origin", r.Header.Get("Origin"))
				w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
				w.Header().Set("Access-Control-Allow-Headers", "Authorization, Content-Type")
			}
		}

		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}

		next.ServeHTTP(w, r)
	})
}

func (app *application) isOriginAllowed(origin string) bool {
	lower := strings.ToLower(origin)
	for _, trusted := range app.config.cors.trustedOrigins {
		if trusted == "*" || trusted == lower {
			return true
		}
	}
	// Also allow Netlify deploy previews for www-etin-dev and admin-etin-dev
	if strings.HasSuffix(lower, "--www-etin-dev.netlify.app") || strings.HasSuffix(lower, "--admin-etin-dev.netlify.app") {
		return true
	}
	return false
}
