CREATE TABLE IF NOT EXISTS bionicpro.kafka_crm_clients
(
    client_id       Int64,
    username        String,
    full_name       String,
    city            String,
    contract_number String,
    `__deleted`     String
)
ENGINE = Kafka
SETTINGS kafka_broker_list = 'kafka:9092',
         kafka_topic_list = 'crm.crm.clients',
         kafka_group_name = 'clickhouse_crm_clients',
         kafka_format = 'JSONEachRow',
         kafka_num_consumers = 1,
         kafka_skip_broken_messages = 100,
         input_format_skip_unknown_fields = 1;

CREATE TABLE IF NOT EXISTS bionicpro.cdc_crm_clients
(
    client_id       UInt64,
    username        String,
    full_name       String,
    city            String,
    contract_number String,
    is_deleted      UInt8,
    ingested_at     DateTime
)
ENGINE = ReplacingMergeTree(ingested_at)
ORDER BY client_id;

CREATE MATERIALIZED VIEW IF NOT EXISTS bionicpro.mv_cdc_crm_clients
TO bionicpro.cdc_crm_clients AS
SELECT
    toUInt64(client_id)      AS client_id,
    username                 AS username,
    full_name                AS full_name,
    city                     AS city,
    contract_number          AS contract_number,
    `__deleted` = 'true'     AS is_deleted,
    now()                    AS ingested_at
FROM bionicpro.kafka_crm_clients;

CREATE TABLE IF NOT EXISTS bionicpro.kafka_crm_prostheses
(
    prosthesis_serial String,
    client_id         Int64,
    model             String,
    manufactured_at   Int32,
    warranty_until    Int32,
    `__deleted`       String
)
ENGINE = Kafka
SETTINGS kafka_broker_list = 'kafka:9092',
         kafka_topic_list = 'crm.crm.prostheses',
         kafka_group_name = 'clickhouse_crm_prostheses',
         kafka_format = 'JSONEachRow',
         kafka_num_consumers = 1,
         kafka_skip_broken_messages = 100,
         input_format_skip_unknown_fields = 1;

CREATE TABLE IF NOT EXISTS bionicpro.cdc_crm_prostheses
(
    prosthesis_serial String,
    client_id         UInt64,
    model             String,
    manufactured_at   Date,
    warranty_until    Date,
    is_deleted        UInt8,
    ingested_at       DateTime
)
ENGINE = ReplacingMergeTree(ingested_at)
ORDER BY prosthesis_serial;

CREATE MATERIALIZED VIEW IF NOT EXISTS bionicpro.mv_cdc_crm_prostheses
TO bionicpro.cdc_crm_prostheses AS
SELECT
    prosthesis_serial        AS prosthesis_serial,
    toUInt64(client_id)      AS client_id,
    model                    AS model,
    toDate(manufactured_at)  AS manufactured_at,
    toDate(warranty_until)   AS warranty_until,
    `__deleted` = 'true'     AS is_deleted,
    now()                    AS ingested_at
FROM bionicpro.kafka_crm_prostheses;

CREATE TABLE IF NOT EXISTS bionicpro.report_daily_v2
(
    username            String,
    report_date         Date,
    prosthesis_serial   String,
    readings_total      AggregateFunction(count),
    gestures_recognized AggregateFunction(countIf, UInt8),
    avg_latency_ms      AggregateFunction(avg, UInt32),
    p95_latency_ms      AggregateFunction(quantile(0.95), UInt32),
    max_latency_ms      AggregateFunction(max, UInt32),
    min_battery_level   AggregateFunction(min, Float32),
    avg_battery_level   AggregateFunction(avg, Float32),
    avg_signal_quality  AggregateFunction(avg, Float32),
    error_events        AggregateFunction(countIf, UInt8)
)
ENGINE = AggregatingMergeTree
PARTITION BY toYYYYMM(report_date)
ORDER BY (username, report_date, prosthesis_serial);

CREATE MATERIALIZED VIEW IF NOT EXISTS bionicpro.mv_report_daily
TO bionicpro.report_daily_v2 AS
SELECT
    c.username                          AS username,
    t.report_date                       AS report_date,
    t.prosthesis_serial                 AS prosthesis_serial,
    countState()                        AS readings_total,
    countIfState(t.recognized = 1)      AS gestures_recognized,
    avgState(t.latency_ms)              AS avg_latency_ms,
    quantileState(0.95)(t.latency_ms)   AS p95_latency_ms,
    maxState(t.latency_ms)              AS max_latency_ms,
    minState(t.battery_level)           AS min_battery_level,
    avgState(t.battery_level)           AS avg_battery_level,
    avgState(t.signal_quality)          AS avg_signal_quality,
    countIfState(t.error_code != '')    AS error_events
FROM bionicpro.telemetry_raw AS t
INNER JOIN
(
    SELECT prosthesis_serial, client_id
    FROM bionicpro.cdc_crm_prostheses FINAL
    WHERE is_deleted = 0
) AS p ON p.prosthesis_serial = t.prosthesis_serial
INNER JOIN
(
    SELECT client_id, username
    FROM bionicpro.cdc_crm_clients FINAL
    WHERE is_deleted = 0
) AS c ON c.client_id = p.client_id
GROUP BY c.username, t.report_date, t.prosthesis_serial;
