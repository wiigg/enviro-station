-- migrate: no-transaction
ALTER TABLE sensor_readings
ADD COLUMN IF NOT EXISTS device_id TEXT NOT NULL DEFAULT 'default';

CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_sensor_readings_device_timestamp_id
ON sensor_readings(device_id, timestamp, id DESC);

DELETE FROM sensor_readings AS stale
USING sensor_readings AS newer
WHERE stale.device_id = newer.device_id
  AND stale.timestamp = newer.timestamp
  AND stale.id < newer.id;

CREATE UNIQUE INDEX CONCURRENTLY IF NOT EXISTS idx_sensor_readings_device_timestamp
ON sensor_readings(device_id, timestamp);

DROP INDEX CONCURRENTLY IF EXISTS idx_sensor_readings_device_timestamp_id;
