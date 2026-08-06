from __future__ import annotations

import os
from datetime import date, timedelta

import clickhouse_connect
import pendulum
import psycopg2
from airflow.decorators import dag, task

SOURCES_DSN = os.environ["SOURCES_DSN"]
CLICKHOUSE_HOST = os.environ.get("CLICKHOUSE_HOST", "clickhouse")
CLICKHOUSE_PORT = int(os.environ.get("CLICKHOUSE_PORT", "8123"))
CLICKHOUSE_USER = os.environ.get("CLICKHOUSE_USER", "default")
CLICKHOUSE_PASSWORD = os.environ.get("CLICKHOUSE_PASSWORD", "")
CLICKHOUSE_DB = os.environ.get("CLICKHOUSE_DB", "bionicpro")

JOB_NAME = "report_daily"
INITIAL_BACKFILL_DAYS = 60
INSERT_BATCH = 50_000


def clickhouse():
    return clickhouse_connect.get_client(
        host=CLICKHOUSE_HOST,
        port=CLICKHOUSE_PORT,
        username=CLICKHOUSE_USER,
        password=CLICKHOUSE_PASSWORD,
        database=CLICKHOUSE_DB,
    )


@dag(
    dag_id="bionicpro_reports_etl",
    description="CRM and telemetry to ClickHouse, daily report mart",
    schedule="0 2 * * *",
    start_date=pendulum.datetime(2024, 1, 1, tz="UTC"),
    catchup=False,
    max_active_runs=1,
    default_args={"retries": 1, "retry_delay": timedelta(minutes=2)},
    tags=["bionicpro", "reports", "etl"],
)
def bionicpro_reports_etl():
    @task
    def resolve_window() -> dict[str, str]:
        end = date.today() - timedelta(days=1)

        with clickhouse() as ch:
            rows = ch.query(
                "SELECT covered_until FROM etl_state FINAL WHERE job = {job:String}",
                parameters={"job": JOB_NAME},
            ).result_rows

        if rows:
            start = rows[0][0] + timedelta(days=1)
        else:
            start = end - timedelta(days=INITIAL_BACKFILL_DAYS)

        if start > end:
            start = end + timedelta(days=1)

        return {"start": start.isoformat(), "end": end.isoformat()}

    @task
    def load_crm_dimensions() -> int:
        with psycopg2.connect(SOURCES_DSN) as pg, pg.cursor() as cur:
            cur.execute(
                "SELECT client_id, username, full_name, city, contract_number FROM crm.clients"
            )
            clients = cur.fetchall()

            cur.execute(
                "SELECT prosthesis_serial, client_id, model, manufactured_at, warranty_until "
                "FROM crm.prostheses"
            )
            prostheses = cur.fetchall()

        with clickhouse() as ch:
            if clients:
                ch.insert(
                    "crm_clients",
                    clients,
                    column_names=["client_id", "username", "full_name", "city", "contract_number"],
                )
            if prostheses:
                ch.insert(
                    "crm_prostheses",
                    prostheses,
                    column_names=[
                        "prosthesis_serial",
                        "client_id",
                        "model",
                        "manufactured_at",
                        "warranty_until",
                    ],
                )

        return len(clients) + len(prostheses)

    @task
    def load_telemetry(window: dict[str, str]) -> int:
        start, end = date.fromisoformat(window["start"]), date.fromisoformat(window["end"])
        if start > end:
            return 0

        columns = [
            "prosthesis_serial",
            "recorded_at",
            "report_date",
            "gesture",
            "recognized",
            "latency_ms",
            "battery_level",
            "signal_quality",
            "error_code",
        ]

        loaded = 0
        with psycopg2.connect(SOURCES_DSN) as pg, pg.cursor(name="telemetry_stream") as cur:
            cur.itersize = INSERT_BATCH
            cur.execute(
                """
                SELECT prosthesis_serial,
                       recorded_at,
                       recorded_at::date AS report_date,
                       gesture,
                       recognized::int AS recognized,
                       latency_ms,
                       battery_level::float8 AS battery_level,
                       signal_quality::float8 AS signal_quality,
                       coalesce(error_code, '') AS error_code
                FROM telemetry.readings
                WHERE recorded_at >= %s AND recorded_at < %s
                ORDER BY recorded_at
                """,
                (start, end + timedelta(days=1)),
            )

            with clickhouse() as ch:
                while True:
                    batch = cur.fetchmany(INSERT_BATCH)
                    if not batch:
                        break
                    ch.insert("telemetry_raw", batch, column_names=columns)
                    loaded += len(batch)

        return loaded

    @task
    def build_report_mart(window: dict[str, str]) -> int:
        start, end = date.fromisoformat(window["start"]), date.fromisoformat(window["end"])
        if start > end:
            return 0

        with clickhouse() as ch:
            ch.command(
                """
                INSERT INTO report_daily
                SELECT
                    c.username,
                    c.client_id,
                    c.full_name,
                    c.city,
                    c.contract_number,
                    t.prosthesis_serial,
                    p.model,
                    t.report_date,
                    t.readings_total,
                    t.gestures_recognized,
                    round(t.gestures_recognized / t.readings_total, 4) AS recognition_rate,
                    t.avg_latency_ms,
                    t.p95_latency_ms,
                    t.max_latency_ms,
                    t.min_battery_level,
                    t.avg_battery_level,
                    t.avg_signal_quality,
                    t.error_events,
                    now() AS generated_at
                FROM
                (
                    SELECT
                        prosthesis_serial,
                        report_date,
                        count() AS readings_total,
                        countIf(recognized = 1) AS gestures_recognized,
                        round(avg(latency_ms), 2) AS avg_latency_ms,
                        round(quantile(0.95)(latency_ms), 2) AS p95_latency_ms,
                        max(latency_ms) AS max_latency_ms,
                        round(min(battery_level), 2) AS min_battery_level,
                        round(avg(battery_level), 2) AS avg_battery_level,
                        round(avg(signal_quality), 2) AS avg_signal_quality,
                        countIf(error_code != '') AS error_events
                    FROM telemetry_raw FINAL
                    WHERE report_date BETWEEN {start:Date} AND {end:Date}
                    GROUP BY prosthesis_serial, report_date
                ) AS t
                INNER JOIN crm_prostheses AS p FINAL USING (prosthesis_serial)
                INNER JOIN crm_clients AS c FINAL USING (client_id)
                """,
                parameters={"start": start, "end": end},
            )

            return ch.query(
                "SELECT count() FROM report_daily FINAL "
                "WHERE report_date BETWEEN {start:Date} AND {end:Date}",
                parameters={"start": start, "end": end},
            ).result_rows[0][0]

    @task
    def update_watermark(window: dict[str, str], rows: int) -> str:
        end = date.fromisoformat(window["end"])
        if date.fromisoformat(window["start"]) > end:
            return "nothing to advance"

        with clickhouse() as ch:
            ch.insert(
                "etl_state",
                [[JOB_NAME, end, pendulum.now("UTC").naive()]],
                column_names=["job", "covered_until", "updated_at"],
            )

        return f"covered_until={end.isoformat()}, mart rows={rows}"

    window = resolve_window()
    dimensions = load_crm_dimensions()
    telemetry = load_telemetry(window)
    mart = build_report_mart(window)

    dimensions >> mart
    telemetry >> mart
    update_watermark(window, mart)


bionicpro_reports_etl()
