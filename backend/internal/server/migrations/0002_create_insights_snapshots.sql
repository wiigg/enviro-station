CREATE TABLE IF NOT EXISTS insights_snapshots (
  snapshot_key TEXT PRIMARY KEY,
  insights JSONB NOT NULL,
  source TEXT NOT NULL,
  generated_at BIGINT NOT NULL,
  analyzed_samples INTEGER NOT NULL,
  analysis_limit INTEGER NOT NULL,
  trigger TEXT NOT NULL,
  updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
