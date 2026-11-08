# `internal`

Packages in `internal/` hold the core privacy engine and PostgreSQL data layer for `analytics.etin.dev`:

- `internal/privacy`: GDPR-compliant daily-rotating salted visitor hashing, bot filtering, coarse User-Agent classification, and URL/referrer sanitization.
- `internal/data`: PostgreSQL schema management, mutable `pageviews` storage, `custom_events` storage, `daily_page_stats` rollups, and reporting queries.
