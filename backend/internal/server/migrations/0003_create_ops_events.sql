CREATE TABLE IF NOT EXISTS ops_events (
  id BIGSERIAL PRIMARY KEY,
  timestamp BIGINT NOT NULL,
  kind TEXT NOT NULL,
  title TEXT NOT NULL,
  detail TEXT NOT NULL,
  created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_ops_events_timestamp ON ops_events(timestamp DESC);
