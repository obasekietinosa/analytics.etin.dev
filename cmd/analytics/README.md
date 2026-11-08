# `cmd/analytics`

This package wires together the HTTP server for `analytics.etin.dev`:

- `main.go`: bootstraps configuration, PostgreSQL connection pool, automatic schema migration, daily rollup worker, and HTTP server.
- `tracker.js`: embedded browser collector served at `GET /tracker.js`.
- `routes.go`: declares the routing table and CORS/logging middleware stack.
- `handlers.go`: handles beacon ingestion (`POST /v1/collect`, `POST /v1/engagement`, `POST /v1/events`), public stats (`GET /v1/public/stats`), and admin overview stats (`GET /v1/stats/overview`).
- `auth.go`: verifies admin Bearer tokens via `api.etin.dev` (`GET /v1/admin/verify`) with a 5-minute in-memory cache (plus optional `ANALYTICS_API_KEY` fallback).
