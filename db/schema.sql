-- SwarmControl TimescaleDB schema.
-- Run automatically by the timescaledb container on first start (mounted into
-- /docker-entrypoint-initdb.d). Idempotent so it can be re-applied by hand.

CREATE EXTENSION IF NOT EXISTS timescaledb;

-- ---------------------------------------------------------------------------
-- Raw telemetry hypertable (P4-SwarmControl.md section 5).
-- ---------------------------------------------------------------------------
CREATE TABLE IF NOT EXISTS telemetry (
  vehicle_id  TEXT             NOT NULL,
  ts          TIMESTAMPTZ      NOT NULL,
  lat         DOUBLE PRECISION,
  lon         DOUBLE PRECISION,
  heading     REAL,
  speed       REAL,
  battery_pct REAL,
  temp        REAL,
  status      TEXT
);

SELECT create_hypertable('telemetry', 'ts', if_not_exists => TRUE);

-- Fast "last N for vehicle X" range scans.
CREATE INDEX IF NOT EXISTS telemetry_vehicle_ts_idx
  ON telemetry (vehicle_id, ts DESC);

-- ---------------------------------------------------------------------------
-- 1-minute continuous aggregate for fast dashboard/history queries.
-- Pre-downsampled rollups keep "last 24h" queries cheap at fleet scale.
-- ---------------------------------------------------------------------------
-- materialized_only=false enables real-time aggregation: queries union the
-- materialized buckets with the most recent (not-yet-rolled-up) raw data, so
-- "last N minutes" is correct immediately instead of lagging the refresh policy.
CREATE MATERIALIZED VIEW IF NOT EXISTS telemetry_1m
WITH (timescaledb.continuous, timescaledb.materialized_only = false) AS
SELECT
  vehicle_id,
  time_bucket('1 minute', ts) AS bucket,
  avg(lat)         AS avg_lat,
  avg(lon)         AS avg_lon,
  avg(speed)       AS avg_speed,
  avg(battery_pct) AS avg_battery,
  avg(temp)        AS avg_temp,
  min(battery_pct) AS min_battery,
  max(temp)        AS max_temp,
  count(*)         AS samples
FROM telemetry
GROUP BY vehicle_id, bucket
WITH NO DATA;

CREATE INDEX IF NOT EXISTS telemetry_1m_vehicle_bucket_idx
  ON telemetry_1m (vehicle_id, bucket DESC);

-- Keep the aggregate current on live data.
SELECT add_continuous_aggregate_policy('telemetry_1m',
  start_offset      => INTERVAL '3 hours',
  end_offset        => INTERVAL '1 minute',
  schedule_interval => INTERVAL '30 seconds',
  if_not_exists     => TRUE);

-- ---------------------------------------------------------------------------
-- Retention: drop raw telemetry after 7 days (aggregates persist longer),
-- keeping raw storage bounded.
-- ---------------------------------------------------------------------------
SELECT add_retention_policy('telemetry', INTERVAL '7 days', if_not_exists => TRUE);

-- ---------------------------------------------------------------------------
-- Command audit log (command service upserts lifecycle transitions here).
-- ---------------------------------------------------------------------------
CREATE TABLE IF NOT EXISTS command_log (
  id          TEXT PRIMARY KEY,
  vehicle_id  TEXT        NOT NULL,
  type        TEXT        NOT NULL,
  payload     JSONB,
  state       TEXT        NOT NULL,
  created_at  TIMESTAMPTZ NOT NULL,
  sent_at     TIMESTAMPTZ,
  acked_at    TIMESTAMPTZ,
  applied_at  TIMESTAMPTZ
);

CREATE INDEX IF NOT EXISTS command_log_vehicle_idx
  ON command_log (vehicle_id, created_at DESC);
