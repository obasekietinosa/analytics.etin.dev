package data

import (
	"context"
	"database/sql"
	_ "embed"
	"encoding/json"
	"fmt"
	"math"
	"time"
)

//go:embed schema.sql
var schemaSQL string

type AnalyticsStore struct {
	DB *sql.DB
}

func NewAnalyticsStore(db *sql.DB) *AnalyticsStore {
	return &AnalyticsStore{DB: db}
}

// EnsureSchema creates the pageviews, custom_events, and daily_page_stats tables if they do not exist.
func (s *AnalyticsStore) EnsureSchema(ctx context.Context) error {
	_, err := s.DB.ExecContext(ctx, schemaSQL)
	return err
}

type PageviewInput struct {
	ID             string
	SiteID         string
	Path           string
	VisitorHash    string
	VisitorID      string // Non-empty only when visitor has opted into persistent cookie consent
	ReferrerDomain string
	UTMSource      string
	UTMMedium      string
	UTMCampaign    string
	DeviceType     string
	Browser        string
	OS             string
	Country        string
}

type EngagementUpdate struct {
	PageviewID      string
	DurationSeconds int
	MaxScrollDepth  int
}

type CustomEventInput struct {
	SiteID      string
	PageviewID  string
	VisitorHash string
	EventName   string
	Path        string
	TargetURL   string
	Metadata    map[string]any
}

type PublicStats struct {
	SiteID             string  `json:"siteId"`
	Path               string  `json:"path,omitempty"`
	Views              int64   `json:"views"`
	UniqueVisitors     int64   `json:"uniqueVisitors"`
	AvgDurationSeconds float64 `json:"avgDurationSeconds"`
	AvgScrollDepth     float64 `json:"avgScrollDepth"`
}

type MetricBreakdown struct {
	Label string `json:"label"`
	Count int64  `json:"count"`
	Share string `json:"share"`
}

type PagePerformance struct {
	Path               string  `json:"path"`
	Views              int64   `json:"views"`
	UniqueVisitors     int64   `json:"uniqueVisitors"`
	AvgDurationSeconds float64 `json:"avgDurationSeconds"`
	AvgScrollDepth     float64 `json:"avgScrollDepth"`
}

type OutboundClickStat struct {
	TargetURL string `json:"targetUrl"`
	Clicks    int64  `json:"clicks"`
}

type DailyPoint struct {
	Day      string `json:"day"`
	Views    int64  `json:"views"`
	Visitors int64  `json:"visitors"`
}

type OverviewStats struct {
	SiteID             string              `json:"siteId"`
	PeriodDays         int                 `json:"periodDays"`
	Pageviews          int64               `json:"pageviews"`
	UniqueVisitors     int64               `json:"uniqueVisitors"`
	ReturningVisitors  int64               `json:"returningVisitors"`
	NotesViewed        int64               `json:"notesViewed"`
	AvgDurationSeconds float64             `json:"avgDurationSeconds"`
	AvgScrollDepth     float64             `json:"avgScrollDepth"`
	Trend              TrendSummary        `json:"trend"`
	DailySeries        []DailyPoint        `json:"dailySeries"`
	TopPages           []PagePerformance   `json:"topPages"`
	TopSources         []MetricBreakdown   `json:"topSources"`
	Devices            []MetricBreakdown   `json:"devices"`
	Browsers           []MetricBreakdown   `json:"browsers"`
	OutboundClicks     []OutboundClickStat `json:"outboundClicks"`
	LastUpdated        time.Time           `json:"lastUpdated"`
}

type TrendSummary struct {
	Direction   string `json:"direction"` // "up" | "down" | "steady"
	Value       string `json:"value"`
	Description string `json:"description"`
}

// RecordPageview inserts or updates a pageview and checks if a consented visitor has visited on an earlier day.
func (s *AnalyticsStore) RecordPageview(ctx context.Context, input PageviewInput) error {
	isReturning := false
	var visitorIDPtr *string

	if input.VisitorID != "" {
		visitorIDPtr = &input.VisitorID
		var priorCount int
		err := s.DB.QueryRowContext(
			ctx,
			`SELECT COUNT(1) FROM pageviews WHERE site_id = $1 AND visitor_id = $2 AND created_at < CURRENT_DATE LIMIT 1`,
			input.SiteID,
			input.VisitorID,
		).Scan(&priorCount)
		if err == nil && priorCount > 0 {
			isReturning = true
		}
	}

	_, err := s.DB.ExecContext(
		ctx,
		`INSERT INTO pageviews (
			id, site_id, path, visitor_hash, visitor_id, is_returning,
			referrer_domain, utm_source, utm_medium, utm_campaign,
			device_type, browser, os, country
		) VALUES ($1, $2, $3, $4, $5, $6, $7, NULLIF($8, ''), NULLIF($9, ''), NULLIF($10, ''), $11, $12, $13, $14)
		ON CONFLICT (id) DO NOTHING`,
		input.ID,
		input.SiteID,
		input.Path,
		input.VisitorHash,
		visitorIDPtr,
		isReturning,
		input.ReferrerDomain,
		input.UTMSource,
		input.UTMMedium,
		input.UTMCampaign,
		input.DeviceType,
		input.Browser,
		input.OS,
		input.Country,
	)
	return err
}

// UpdateEngagement updates active reading time and maximum scroll depth in-place on an existing pageview row.
func (s *AnalyticsStore) UpdateEngagement(ctx context.Context, update EngagementUpdate) error {
	duration := update.DurationSeconds
	if duration < 0 {
		duration = 0
	}
	if duration > 7200 {
		duration = 7200 // Cap single pageview active reading time at 2 hours
	}

	scroll := update.MaxScrollDepth
	if scroll < 0 {
		scroll = 0
	}
	if scroll > 100 {
		scroll = 100
	}

	_, err := s.DB.ExecContext(
		ctx,
		`UPDATE pageviews
		 SET duration_seconds = GREATEST(duration_seconds, $2),
		     max_scroll_depth = GREATEST(max_scroll_depth, $3)
		 WHERE id = $1`,
		update.PageviewID,
		duration,
		scroll,
	)
	return err
}

// RecordEvent inserts an outbound click or custom behavioral event.
func (s *AnalyticsStore) RecordEvent(ctx context.Context, input CustomEventInput) error {
	var pageviewIDPtr *string
	if input.PageviewID != "" {
		pageviewIDPtr = &input.PageviewID
	}

	var metaBytes []byte
	if len(input.Metadata) > 0 {
		if b, err := json.Marshal(input.Metadata); err == nil {
			metaBytes = b
		}
	}

	_, err := s.DB.ExecContext(
		ctx,
		`INSERT INTO custom_events (
			site_id, pageview_id, visitor_hash, event_name, path, target_url, metadata
		) VALUES ($1, $2, $3, $4, $5, NULLIF($6, ''), $7)`,
		input.SiteID,
		pageviewIDPtr,
		input.VisitorHash,
		input.EventName,
		input.Path,
		input.TargetURL,
		metaBytes,
	)
	return err
}

// RollupDay aggregates a specific UTC date from pageviews into daily_page_stats.
func (s *AnalyticsStore) RollupDay(ctx context.Context, siteID string, day time.Time) error {
	dayStr := day.UTC().Format("2006-01-02")
	_, err := s.DB.ExecContext(
		ctx,
		`INSERT INTO daily_page_stats (
			site_id, day, path, views, unique_visitors, returning_visitors,
			total_duration_seconds, total_scroll_depth
		)
		SELECT
			site_id,
			DATE(created_at AT TIME ZONE 'UTC') AS day,
			path,
			COUNT(*) AS views,
			COUNT(DISTINCT COALESCE(visitor_id, visitor_hash)) AS unique_visitors,
			COUNT(DISTINCT CASE WHEN is_returning THEN COALESCE(visitor_id, visitor_hash) END) AS returning_visitors,
			COALESCE(SUM(duration_seconds), 0) AS total_duration_seconds,
			COALESCE(SUM(max_scroll_depth), 0) AS total_scroll_depth
		FROM pageviews
		WHERE site_id = $1
		  AND DATE(created_at AT TIME ZONE 'UTC') = $2::date
		GROUP BY site_id, DATE(created_at AT TIME ZONE 'UTC'), path
		ON CONFLICT (site_id, day, path) DO UPDATE SET
			views = EXCLUDED.views,
			unique_visitors = EXCLUDED.unique_visitors,
			returning_visitors = EXCLUDED.returning_visitors,
			total_duration_seconds = EXCLUDED.total_duration_seconds,
			total_scroll_depth = EXCLUDED.total_scroll_depth`,
		siteID,
		dayStr,
	)
	return err
}

// GetPublicStats returns safe, read-only aggregate view and reading metrics for a specific path or the whole site.
func (s *AnalyticsStore) GetPublicStats(ctx context.Context, siteID, path string) (PublicStats, error) {
	result := PublicStats{
		SiteID: siteID,
		Path:   path,
	}

	if path != "" {
		err := s.DB.QueryRowContext(
			ctx,
			`SELECT
				COUNT(*),
				COUNT(DISTINCT COALESCE(visitor_id, visitor_hash)),
				COALESCE(AVG(duration_seconds), 0),
				COALESCE(AVG(max_scroll_depth), 0)
			 FROM pageviews
			 WHERE site_id = $1 AND path = $2`,
			siteID,
			path,
		).Scan(&result.Views, &result.UniqueVisitors, &result.AvgDurationSeconds, &result.AvgScrollDepth)
		if err != nil {
			return result, err
		}
	} else {
		err := s.DB.QueryRowContext(
			ctx,
			`SELECT
				COUNT(*),
				COUNT(DISTINCT COALESCE(visitor_id, visitor_hash)),
				COALESCE(AVG(duration_seconds), 0),
				COALESCE(AVG(max_scroll_depth), 0)
			 FROM pageviews
			 WHERE site_id = $1`,
			siteID,
		).Scan(&result.Views, &result.UniqueVisitors, &result.AvgDurationSeconds, &result.AvgScrollDepth)
		if err != nil {
			return result, err
		}
	}

	result.AvgDurationSeconds = math.Round(result.AvgDurationSeconds*10) / 10
	result.AvgScrollDepth = math.Round(result.AvgScrollDepth*10) / 10
	return result, nil
}

// GetOverviewStats computes the full administrative analytics summary for admin.etin.dev.
func (s *AnalyticsStore) GetOverviewStats(ctx context.Context, siteID string, days int) (OverviewStats, error) {
	if days <= 0 {
		days = 7
	}
	if days > 365 {
		days = 365
	}

	now := time.Now().UTC()
	since := now.AddDate(0, 0, -days)
	prevSince := since.AddDate(0, 0, -days)

	stats := OverviewStats{
		SiteID:        siteID,
		PeriodDays:    days,
		DailySeries:   []DailyPoint{},
		TopPages:      []PagePerformance{},
		TopSources:    []MetricBreakdown{},
		Devices:       []MetricBreakdown{},
		Browsers:      []MetricBreakdown{},
		OutboundClicks: []OutboundClickStat{},
		LastUpdated:   now,
	}

	// 1. Headline metrics for current window
	err := s.DB.QueryRowContext(
		ctx,
		`SELECT
			COUNT(*),
			COUNT(DISTINCT COALESCE(visitor_id, visitor_hash)),
			COUNT(DISTINCT CASE WHEN is_returning THEN COALESCE(visitor_id, visitor_hash) END),
			COUNT(CASE WHEN path LIKE '/notes/%' THEN 1 END),
			COALESCE(AVG(duration_seconds), 0),
			COALESCE(AVG(max_scroll_depth), 0)
		 FROM pageviews
		 WHERE site_id = $1 AND created_at >= $2`,
		siteID,
		since,
	).Scan(
		&stats.Pageviews,
		&stats.UniqueVisitors,
		&stats.ReturningVisitors,
		&stats.NotesViewed,
		&stats.AvgDurationSeconds,
		&stats.AvgScrollDepth,
	)
	if err != nil {
		return stats, err
	}

	stats.AvgDurationSeconds = math.Round(stats.AvgDurationSeconds*10) / 10
	stats.AvgScrollDepth = math.Round(stats.AvgScrollDepth*10) / 10

	// 2. Previous window visitor count for trend calculation
	var prevVisitors int64
	_ = s.DB.QueryRowContext(
		ctx,
		`SELECT COUNT(DISTINCT COALESCE(visitor_id, visitor_hash))
		 FROM pageviews
		 WHERE site_id = $1 AND created_at >= $2 AND created_at < $3`,
		siteID,
		prevSince,
		since,
	).Scan(&prevVisitors)

	stats.Trend = computeTrend(stats.UniqueVisitors, prevVisitors, days)

	// 3. Top pages / notes with engagement metrics
	pageRows, err := s.DB.QueryContext(
		ctx,
		`SELECT
			path,
			COUNT(*) AS views,
			COUNT(DISTINCT COALESCE(visitor_id, visitor_hash)) AS visitors,
			COALESCE(AVG(duration_seconds), 0) AS avg_duration,
			COALESCE(AVG(max_scroll_depth), 0) AS avg_scroll
		 FROM pageviews
		 WHERE site_id = $1 AND created_at >= $2
		 GROUP BY path
		 ORDER BY views DESC
		 LIMIT 10`,
		siteID,
		since,
	)
	if err == nil {
		defer pageRows.Close()
		for pageRows.Next() {
			var p PagePerformance
			if err := pageRows.Scan(&p.Path, &p.Views, &p.UniqueVisitors, &p.AvgDurationSeconds, &p.AvgScrollDepth); err == nil {
				p.AvgDurationSeconds = math.Round(p.AvgDurationSeconds*10) / 10
				p.AvgScrollDepth = math.Round(p.AvgScrollDepth*10) / 10
				stats.TopPages = append(stats.TopPages, p)
			}
		}
	}

	// 4. Top discovery sources (referrer_domain or utm_source)
	stats.TopSources = s.queryBreakdown(
		ctx,
		`SELECT COALESCE(NULLIF(utm_source, ''), referrer_domain) AS source, COUNT(*) AS cnt
		 FROM pageviews
		 WHERE site_id = $1 AND created_at >= $2
		 GROUP BY source
		 ORDER BY cnt DESC
		 LIMIT 6`,
		siteID,
		since,
		stats.Pageviews,
	)

	// 5. Device breakdown
	stats.Devices = s.queryBreakdown(
		ctx,
		`SELECT device_type, COUNT(*) AS cnt
		 FROM pageviews
		 WHERE site_id = $1 AND created_at >= $2
		 GROUP BY device_type
		 ORDER BY cnt DESC`,
		siteID,
		since,
		stats.Pageviews,
	)

	// 6. Browser breakdown
	stats.Browsers = s.queryBreakdown(
		ctx,
		`SELECT browser, COUNT(*) AS cnt
		 FROM pageviews
		 WHERE site_id = $1 AND created_at >= $2
		 GROUP BY browser
		 ORDER BY cnt DESC
		 LIMIT 5`,
		siteID,
		since,
		stats.Pageviews,
	)

	// 7. Outbound link clicks
	clickRows, err := s.DB.QueryContext(
		ctx,
		`SELECT target_url, COUNT(*) AS clicks
		 FROM custom_events
		 WHERE site_id = $1 AND created_at >= $2 AND event_name = 'outbound_click' AND target_url IS NOT NULL
		 GROUP BY target_url
		 ORDER BY clicks DESC
		 LIMIT 8`,
		siteID,
		since,
	)
	if err == nil {
		defer clickRows.Close()
		for clickRows.Next() {
			var oc OutboundClickStat
			if err := clickRows.Scan(&oc.TargetURL, &oc.Clicks); err == nil {
				stats.OutboundClicks = append(stats.OutboundClicks, oc)
			}
		}
	}

	return stats, nil
}

func (s *AnalyticsStore) queryBreakdown(ctx context.Context, query, siteID string, since time.Time, total int64) []MetricBreakdown {
	rows, err := s.DB.QueryContext(ctx, query, siteID, since)
	if err != nil {
		return []MetricBreakdown{}
	}
	defer rows.Close()

	var list []MetricBreakdown
	for rows.Next() {
		var item MetricBreakdown
		if err := rows.Scan(&item.Label, &item.Count); err == nil {
			pct := 0.0
			if total > 0 {
				pct = (float64(item.Count) / float64(total)) * 100.0
			}
			item.Share = fmt.Sprintf("%.0f%%", math.Round(pct))
			list = append(list, item)
		}
	}
	return list
}

func computeTrend(current, previous int64, days int) TrendSummary {
	windowDesc := fmt.Sprintf("Compared to the previous %d-day window.", days)
	if previous == 0 {
		if current > 0 {
			return TrendSummary{
				Direction:   "up",
				Value:       "+100%",
				Description: windowDesc,
			}
		}
		return TrendSummary{
			Direction:   "steady",
			Value:       "Stable",
			Description: windowDesc,
		}
	}

	diff := float64(current-previous) / float64(previous) * 100.0
	if math.Abs(diff) < 1.0 {
		return TrendSummary{
			Direction:   "steady",
			Value:       "0.0%",
			Description: windowDesc,
		}
	}
	if diff > 0 {
		return TrendSummary{
			Direction:   "up",
			Value:       fmt.Sprintf("+%.1f%%", diff),
			Description: windowDesc,
		}
	}
	return TrendSummary{
		Direction:   "down",
		Value:       fmt.Sprintf("%.1f%%", diff),
		Description: windowDesc,
	}
}
