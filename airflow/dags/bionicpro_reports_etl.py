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
    description="Telemetry to ClickHouse, report mart is filled by materialized view",
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

        with clickhouse() as ch:
            for table in ("telemetry_raw", "report_daily_v2"):
                ch.command(
                    f"DELETE FROM {table} WHERE report_date BETWEEN {{start:Date}} AND {{end:Date}}",
                    parameters={"start": start, "end": end},
                    settings={"mutations_sync": 2},
                )

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
    def update_watermark(window: dict[str, str], loaded: int) -> str:
        end = date.fromisoformat(window["end"])
        if date.fromisoformat(window["start"]) > end:
            return "nothing to advance"

        with clickhouse() as ch:
            ch.insert(
                "etl_state",
                [[JOB_NAME, end, pendulum.now("UTC").naive()]],
                column_names=["job", "covered_until", "updated_at"],
            )
            mart_rows = ch.query(
                "SELECT count() FROM report_daily_v2 WHERE report_date BETWEEN {start:Date} AND {end:Date}",
                parameters={"start": date.fromisoformat(window["start"]), "end": end},
            ).result_rows[0][0]

        return f"covered_until={end.isoformat()}, telemetry rows={loaded}, mart rows={mart_rows}"

    window = resolve_window()
    telemetry = load_telemetry(window)
    update_watermark(window, telemetry)


bionicpro_reports_etl()
