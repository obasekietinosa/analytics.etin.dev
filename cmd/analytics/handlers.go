package main

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"time"

	"analytics.etin.dev/internal/data"
	"analytics.etin.dev/internal/privacy"
	"github.com/google/uuid"
)

func (app *application) writeJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}

func (app *application) readJSON(w http.ResponseWriter, r *http.Request, dst any) error {
	r.Body = http.MaxBytesReader(w, r.Body, 64*1024) // 64 KB max beacon size
	dec := json.NewDecoder(r.Body)
	return dec.Decode(dst)
}

func (app *application) healthcheckHandler(w http.ResponseWriter, _ *http.Request) {
	app.writeJSON(w, http.StatusOK, map[string]string{
		"status":  "available",
		"service": "analytics.etin.dev",
	})
}

func (app *application) trackerScriptHandler(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/javascript; charset=utf-8")
	w.Header().Set("Cache-Control", "public, max-age=3600")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(app.trackerJS)
}

func (app *application) collectPageviewHandler(w http.ResponseWriter, r *http.Request) {
	ua := r.Header.Get("User-Agent")
	if privacy.IsBot(ua) {
		w.WriteHeader(http.StatusNoContent)
		return
	}

	var input struct {
		PageviewID  string `json:"pageviewId"`
		SiteID      string `json:"siteId"`
		Path        string `json:"path"`
		Referrer    string `json:"referrer"`
		UTMSource   string `json:"utmSource"`
		UTMMedium   string `json:"utmMedium"`
		UTMCampaign string `json:"utmCampaign"`
		VisitorID   string `json:"visitorId"`
	}

	if err := app.readJSON(w, r, &input); err != nil {
		app.writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid payload"})
		return
	}

	pvID := strings.TrimSpace(input.PageviewID)
	if _, err := uuid.Parse(pvID); err != nil {
		pvID = uuid.New().String()
	}

	siteID := strings.TrimSpace(input.SiteID)
	if siteID == "" {
		siteID = app.config.defaultSiteID
	}

	// Validate optional consented visitor UUID
	visitorID := strings.TrimSpace(input.VisitorID)
	if visitorID != "" {
		if _, err := uuid.Parse(visitorID); err != nil {
			visitorID = ""
		}
	}

	clientIP := privacy.ExtractClientIP(r)
	visitorHash := app.hasher.HashVisitor(siteID, clientIP, ua)
	deviceType, browser, osName := privacy.ParseUserAgent(ua)
	country := privacy.ExtractCountry(r)

	err := app.store.RecordPageview(r.Context(), data.PageviewInput{
		ID:             pvID,
		SiteID:         siteID,
		Path:           privacy.SanitizePath(input.Path),
		VisitorHash:    visitorHash,
		VisitorID:      visitorID,
		ReferrerDomain: privacy.SanitizeReferrer(input.Referrer, siteID, "etin.dev"),
		UTMSource:      truncate(input.UTMSource, 128),
		UTMMedium:      truncate(input.UTMMedium, 128),
		UTMCampaign:    truncate(input.UTMCampaign, 128),
		DeviceType:     deviceType,
		Browser:        browser,
		OS:             osName,
		Country:        country,
	})
	if err != nil {
		app.logger.Printf("collect pageview error: %v", err)
		app.writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "could not record pageview"})
		return
	}

	app.writeJSON(w, http.StatusAccepted, map[string]string{"pageviewId": pvID})
}

func (app *application) collectEngagementHandler(w http.ResponseWriter, r *http.Request) {
	if privacy.IsBot(r.Header.Get("User-Agent")) {
		w.WriteHeader(http.StatusNoContent)
		return
	}

	var input struct {
		PageviewID      string `json:"pageviewId"`
		DurationSeconds int    `json:"durationSeconds"`
		MaxScrollDepth  int    `json:"maxScrollDepth"`
	}

	if err := app.readJSON(w, r, &input); err != nil {
		app.writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid payload"})
		return
	}

	if _, err := uuid.Parse(strings.TrimSpace(input.PageviewID)); err != nil {
		app.writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid pageviewId"})
		return
	}

	if err := app.store.UpdateEngagement(r.Context(), data.EngagementUpdate{
		PageviewID:      strings.TrimSpace(input.PageviewID),
		DurationSeconds: input.DurationSeconds,
		MaxScrollDepth:  input.MaxScrollDepth,
	}); err != nil {
		app.logger.Printf("update engagement error: %v", err)
		app.writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "could not update engagement"})
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

func (app *application) collectEventHandler(w http.ResponseWriter, r *http.Request) {
	ua := r.Header.Get("User-Agent")
	if privacy.IsBot(ua) {
		w.WriteHeader(http.StatusNoContent)
		return
	}

	var input struct {
		SiteID     string         `json:"siteId"`
		PageviewID string         `json:"pageviewId"`
		EventName  string         `json:"eventName"`
		Path       string         `json:"path"`
		TargetURL  string         `json:"targetUrl"`
		Metadata   map[string]any `json:"metadata"`
	}

	if err := app.readJSON(w, r, &input); err != nil {
		app.writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid payload"})
		return
	}

	eventName := truncate(strings.TrimSpace(input.EventName), 128)
	if eventName == "" {
		app.writeJSON(w, http.StatusBadRequest, map[string]string{"error": "eventName is required"})
		return
	}

	siteID := strings.TrimSpace(input.SiteID)
	if siteID == "" {
		siteID = app.config.defaultSiteID
	}

	pvID := strings.TrimSpace(input.PageviewID)
	if _, err := uuid.Parse(pvID); err != nil {
		pvID = ""
	}

	clientIP := privacy.ExtractClientIP(r)
	visitorHash := app.hasher.HashVisitor(siteID, clientIP, ua)

	if err := app.store.RecordEvent(r.Context(), data.CustomEventInput{
		SiteID:      siteID,
		PageviewID:  pvID,
		VisitorHash: visitorHash,
		EventName:   eventName,
		Path:        privacy.SanitizePath(input.Path),
		TargetURL:   truncate(strings.TrimSpace(input.TargetURL), 2048),
		Metadata:    input.Metadata,
	}); err != nil {
		app.logger.Printf("record event error: %v", err)
		app.writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "could not record event"})
		return
	}

	w.WriteHeader(http.StatusAccepted)
}

func (app *application) publicStatsHandler(w http.ResponseWriter, r *http.Request) {
	siteID := strings.TrimSpace(r.URL.Query().Get("siteId"))
	if siteID == "" {
		siteID = app.config.defaultSiteID
	}

	rawPath := strings.TrimSpace(r.URL.Query().Get("path"))
	cleanPath := ""
	if rawPath != "" {
		cleanPath = privacy.SanitizePath(rawPath)
	}

	stats, err := app.store.GetPublicStats(r.Context(), siteID, cleanPath)
	if err != nil {
		app.logger.Printf("public stats error: %v", err)
		app.writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "could not load stats"})
		return
	}

	app.writeJSON(w, http.StatusOK, stats)
}

func (app *application) overviewStatsHandler(w http.ResponseWriter, r *http.Request) {
	siteID := strings.TrimSpace(r.URL.Query().Get("siteId"))
	if siteID == "" {
		siteID = app.config.defaultSiteID
	}

	days := 7
	if rawDays := strings.TrimSpace(r.URL.Query().Get("days")); rawDays != "" {
		if parsed, err := strconv.Atoi(rawDays); err == nil && parsed > 0 {
			days = parsed
		}
	}

	stats, err := app.store.GetOverviewStats(r.Context(), siteID, days)
	if err != nil {
		app.logger.Printf("overview stats error: %v", err)
		app.writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "could not load overview stats"})
		return
	}

	app.writeJSON(w, http.StatusOK, stats)
}

func (app *application) triggerRollupHandler(w http.ResponseWriter, r *http.Request) {
	siteID := strings.TrimSpace(r.URL.Query().Get("siteId"))
	if siteID == "" {
		siteID = app.config.defaultSiteID
	}

	if err := app.store.RollupDay(r.Context(), siteID, time.Now().UTC()); err != nil {
		app.logger.Printf("rollup error: %v", err)
		app.writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "rollup failed"})
		return
	}

	app.writeJSON(w, http.StatusOK, map[string]string{"status": "rolled up"})
}

func truncate(s string, max int) string {
	s = strings.TrimSpace(s)
	if len(s) > max {
		return s[:max]
	}
	return s
}
