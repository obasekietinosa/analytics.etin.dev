CREATE TABLE IF NOT EXISTS pageviews (
  id uuid PRIMARY KEY,
  site_id varchar(100) NOT NULL DEFAULT 'etin.dev',
  path varchar(512) NOT NULL,
  visitor_hash char(64) NOT NULL,
  visitor_id varchar(64),
  is_returning boolean NOT NULL DEFAULT false,
  referrer_domain varchar(255) NOT NULL DEFAULT 'Direct',
  utm_source varchar(128),
  utm_medium varchar(128),
  utm_campaign varchar(128),
  device_type varchar(32) NOT NULL DEFAULT 'desktop',
  browser varchar(64) NOT NULL DEFAULT 'Other',
  os varchar(64) NOT NULL DEFAULT 'Other',
  country varchar(8) NOT NULL DEFAULT 'UN',
  duration_seconds integer NOT NULL DEFAULT 0,
  max_scroll_depth integer NOT NULL DEFAULT 0,
  created_at timestamptz NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_pageviews_site_created ON pageviews (site_id, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_pageviews_site_path ON pageviews (site_id, path);
CREATE INDEX IF NOT EXISTS idx_pageviews_visitor_id ON pageviews (site_id, visitor_id) WHERE visitor_id IS NOT NULL;

CREATE TABLE IF NOT EXISTS custom_events (
  id bigserial PRIMARY KEY,
  site_id varchar(100) NOT NULL DEFAULT 'etin.dev',
  pageview_id uuid,
  visitor_hash char(64) NOT NULL,
  event_name varchar(128) NOT NULL,
  path varchar(512) NOT NULL,
  target_url text,
  metadata jsonb,
  created_at timestamptz NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_custom_events_site_created ON custom_events (site_id, created_at DESC);

CREATE TABLE IF NOT EXISTS daily_page_stats (
  site_id varchar(100) NOT NULL DEFAULT 'etin.dev',
  day date NOT NULL,
  path varchar(512) NOT NULL,
  views bigint NOT NULL DEFAULT 0,
  unique_visitors bigint NOT NULL DEFAULT 0,
  returning_visitors bigint NOT NULL DEFAULT 0,
  total_duration_seconds bigint NOT NULL DEFAULT 0,
  total_scroll_depth bigint NOT NULL DEFAULT 0,
  PRIMARY KEY (site_id, day, path)
);
