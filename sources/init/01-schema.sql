CREATE SCHEMA crm;
CREATE SCHEMA telemetry;

CREATE TABLE crm.clients (
    client_id       bigserial PRIMARY KEY,
    username        text NOT NULL UNIQUE,
    full_name       text NOT NULL,
    city            text NOT NULL,
    contract_number text NOT NULL,
    created_at      timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE crm.prostheses (
    prosthesis_serial text PRIMARY KEY,
    client_id         bigint NOT NULL REFERENCES crm.clients (client_id),
    model             text NOT NULL,
    manufactured_at   date NOT NULL,
    warranty_until    date NOT NULL
);

CREATE TABLE telemetry.readings (
    reading_id        bigserial PRIMARY KEY,
    prosthesis_serial text NOT NULL,
    recorded_at       timestamptz NOT NULL,
    gesture           text NOT NULL,
    recognized        boolean NOT NULL,
    latency_ms        integer NOT NULL,
    battery_level     numeric(5, 2) NOT NULL,
    signal_quality    numeric(5, 2) NOT NULL,
    error_code        text
);

CREATE INDEX readings_recorded_at_idx ON telemetry.readings (recorded_at);
CREATE INDEX readings_serial_recorded_at_idx ON telemetry.readings (prosthesis_serial, recorded_at);
