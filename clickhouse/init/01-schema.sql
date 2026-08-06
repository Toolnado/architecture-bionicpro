CREATE DATABASE IF NOT EXISTS bionicpro;

CREATE TABLE IF NOT EXISTS bionicpro.crm_clients
(
    client_id       UInt64,
    username        String,
    full_name       String,
    city            String,
    contract_number String,
    loaded_at       DateTime DEFAULT now()
)
ENGINE = ReplacingMergeTree(loaded_at)
ORDER BY client_id;

CREATE TABLE IF NOT EXISTS bionicpro.crm_prostheses
(
    prosthesis_serial String,
    client_id         UInt64,
    model             String,
    manufactured_at   Date,
    warranty_until    Date,
    loaded_at         DateTime DEFAULT now()
)
ENGINE = ReplacingMergeTree(loaded_at)
ORDER BY prosthesis_serial;

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

CREATE TABLE IF NOT EXISTS bionicpro.report_daily
(
    username            String,
    client_id           UInt64,
    full_name           String,
    city                String,
    contract_number     String,
    prosthesis_serial   String,
    model               String,
    report_date         Date,
    readings_total      UInt64,
    gestures_recognized UInt64,
    recognition_rate    Float64,
    avg_latency_ms      Float64,
    p95_latency_ms      Float64,
    max_latency_ms      UInt32,
    min_battery_level   Float64,
    avg_battery_level   Float64,
    avg_signal_quality  Float64,
    error_events        UInt64,
    generated_at        DateTime
)
ENGINE = ReplacingMergeTree(generated_at)
PARTITION BY toYYYYMM(report_date)
ORDER BY (username, report_date, prosthesis_serial);

CREATE TABLE IF NOT EXISTS bionicpro.etl_state
(
    job           String,
    covered_until Date,
    updated_at    DateTime
)
ENGINE = ReplacingMergeTree(updated_at)
ORDER BY job;
