# analytics.etin.dev

A lightweight, self-hosted, GDPR-compliant web analytics platform built in Go and PostgreSQL to replace Matomo on `etin.dev`.

## Architecture & Pipeline

```mermaid
flowchart LR
    A["Browser Collector\n(/tracker.js on www.etin.dev)"] -->|"POST /v1/collect\nPOST /v1/engagement\nPOST /v1/events"| B["Ingestion & Privacy Engine\n(analytics.etin.dev)"]
    B -->|"Upsert pageviews\n& daily rollups"| C[("PostgreSQL\npageviews / custom_events / daily_page_stats")]
    C -->|"GET /v1/public/stats"| D["Public View Counters\n(www.etin.dev)"]
    C -->|"GET /v1/stats/overview\n(SSO via api.etin.dev)"| E["Admin Dashboard\n(admin.etin.dev)"]
```

## Key Features

1. **Hybrid GDPR Privacy Model**:
   - **Default (Pre-consent / Declined)**: 100% cookieless and anonymous. Never stores raw IP addresses or sets cookies. Computes an irreversible daily visitor hash in memory (`SHA-256(daily_rotating_salt + site_id + ip + user_agent)`) whose random salt is discarded every 24 hours.
   - **Opt-In (Accepted Consent Banner)**: Upgrades to a first-party `etin_vid` UUID cookie (`SameSite=Lax; Secure`) so multi-day returning visitors can be recognized, with instant opt-out via `window.etinAnalytics.optOut()`.
2. **Full Behavioral Suite**:
   - Pageviews, Unique Visitors, Returning Visitors, Referrers, and UTM parameters (`utm_source`, `utm_medium`, `utm_campaign`).
   - **Active Reading Time**: Counts seconds only while the tab is visible and recently active, updating `duration_seconds` in-place via `navigator.sendBeacon`.
   - **Scroll Depth Milestones**: Tracks `25%`, `50%`, `75%`, and `100%` scroll depth on long-form notes.
   - **Outbound Link Clicks**: Automatically records clicks on external links.
3. **Compact Hybrid Storage (Option C)**:
   - Updates `duration_seconds` and `max_scroll_depth` in-place on the `pageviews` row (`GREATEST(...)`) instead of flooding the database with heartbeat rows.
   - Automatically rolls up daily totals into `daily_page_stats` so raw rows can be pruned in the future without losing lifetime view counts.
4. **Partially Public + Single Sign-On Admin API**:
   - `GET /v1/public/stats?path=/notes/...`: Public read-only view counts and reading time averages.
   - `GET /v1/stats/overview?days=7`: Full administrative analytics breakdown, authenticated using your existing `admin.etin.dev` Bearer token (verified against `api.etin.dev/v1/admin/verify` with a 5-minute cache, or via `ANALYTICS_API_KEY`).

## Running Locally

```bash
export ANALYTICS_DB_DSN='postgres://website:etin@localhost/website?sslmode=disable'
export ANALYTICS_AUTH_API_URL='https://api.etin.dev'
go run ./cmd/analytics
```

## Running Tests

```bash
go test -v ./...
```