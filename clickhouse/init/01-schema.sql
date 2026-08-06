CREATE DATABASE IF NOT EXISTS bionicpro;

CREATE TABLE IF NOT EXISTS bionicpro.telemetry_raw
(
    prosthesis_serial String,
    recorded_at       DateTime,
    report_date       Date,
    gesture           String,
    recognized        UInt8,
    latency_ms        UInt32,
    battery_level     Float32,
    signal_quality    Float32,
    error_code        String
)
ENGINE = ReplacingMergeTree
PARTITION BY toYYYYMM(report_date)
ORDER BY (prosthesis_serial, recorded_at);

CREATE TABLE IF NOT EXISTS bionicpro.etl_state
(
    job           String,
    covered_until Date,
    updated_at    DateTime
)
ENGINE = ReplacingMergeTree(updated_at)
ORDER BY job;
