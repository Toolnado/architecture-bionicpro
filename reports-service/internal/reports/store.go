package reports

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/ClickHouse/clickhouse-go/v2"
	"github.com/ClickHouse/clickhouse-go/v2/lib/driver"
)

const martJob = "report_daily"

var ErrNoCoverage = errors.New("mart has no processed data yet")

type Store struct {
	conn driver.Conn
}

type Day struct {
	Date               time.Time `json:"date"`
	ProsthesisSerial   string    `json:"prosthesisSerial"`
	Model              string    `json:"model"`
	ReadingsTotal      uint64    `json:"readingsTotal"`
	GesturesRecognized uint64    `json:"gesturesRecognized"`
	RecognitionRate    float64   `json:"recognitionRate"`
	AvgLatencyMs       float64   `json:"avgLatencyMs"`
	P95LatencyMs       float64   `json:"p95LatencyMs"`
	MaxLatencyMs       uint32    `json:"maxLatencyMs"`
	MinBatteryLevel    float64   `json:"minBatteryLevel"`
	AvgBatteryLevel    float64   `json:"avgBatteryLevel"`
	AvgSignalQuality   float64   `json:"avgSignalQuality"`
	ErrorEvents        uint64    `json:"errorEvents"`
}

type Client struct {
	FullName       string `json:"fullName"`
	City           string `json:"city"`
	ContractNumber string `json:"contractNumber"`
}

func Connect(ctx context.Context, addr, database, user, password string) (*Store, error) {
	conn, err := clickhouse.Open(&clickhouse.Options{
		Addr: []string{addr},
		Auth: clickhouse.Auth{Database: database, Username: user, Password: password},
	})
	if err != nil {
		return nil, fmt.Errorf("clickhouse open: %w", err)
	}
	if err := conn.Ping(ctx); err != nil {
		return nil, fmt.Errorf("clickhouse ping: %w", err)
	}
	return &Store{conn: conn}, nil
}

func (s *Store) Close() error { return s.conn.Close() }

func (s *Store) CoveredUntil(ctx context.Context) (time.Time, error) {
	var covered time.Time

	row := s.conn.QueryRow(ctx,
		"SELECT covered_until FROM etl_state FINAL WHERE job = ?", martJob)
	if err := row.Scan(&covered); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return time.Time{}, ErrNoCoverage
		}
		return time.Time{}, fmt.Errorf("read watermark: %w", err)
	}
	return covered, nil
}

func (s *Store) DimensionVersion(ctx context.Context, username string) (time.Time, error) {
	var version time.Time

	row := s.conn.QueryRow(ctx, `
		SELECT max(ingested_at)
		FROM
		(
		    SELECT ingested_at
		    FROM cdc_crm_clients FINAL
		    WHERE username = ?

		    UNION ALL

		    SELECT p.ingested_at
		    FROM cdc_crm_prostheses AS p FINAL
		    INNER JOIN
		    (
		        SELECT client_id FROM cdc_crm_clients FINAL WHERE username = ?
		    ) AS c USING (client_id)
		)`, username, username)
	if err := row.Scan(&version); err != nil {
		return time.Time{}, fmt.Errorf("read dimension version: %w", err)
	}
	return version, nil
}

func (s *Store) Days(ctx context.Context, username string, from, to time.Time) ([]Day, *Client, error) {
	rows, err := s.conn.Query(ctx, `
		SELECT m.report_date,
		       m.prosthesis_serial,
		       p.model,
		       countMerge(m.readings_total)                          AS readings_total,
		       countIfMerge(m.gestures_recognized)                   AS gestures_recognized,
		       round(gestures_recognized / readings_total, 4)        AS recognition_rate,
		       round(avgMerge(m.avg_latency_ms), 2)                  AS avg_latency_ms,
		       round(quantileMerge(0.95)(m.p95_latency_ms), 2)       AS p95_latency_ms,
		       maxMerge(m.max_latency_ms)                            AS max_latency_ms,
		       round(toFloat64(minMerge(m.min_battery_level)), 2)     AS min_battery_level,
		       round(avgMerge(m.avg_battery_level), 2)               AS avg_battery_level,
		       round(avgMerge(m.avg_signal_quality), 2)              AS avg_signal_quality,
		       countIfMerge(m.error_events)                          AS error_events,
		       c.full_name,
		       c.city,
		       c.contract_number
		FROM report_daily_v2 AS m
		INNER JOIN
		(
		    SELECT prosthesis_serial, model
		    FROM cdc_crm_prostheses FINAL
		    WHERE is_deleted = 0
		) AS p USING (prosthesis_serial)
		INNER JOIN
		(
		    SELECT username, full_name, city, contract_number
		    FROM cdc_crm_clients FINAL
		    WHERE is_deleted = 0
		) AS c USING (username)
		WHERE m.username = ? AND m.report_date BETWEEN ? AND ?
		GROUP BY m.report_date, m.prosthesis_serial, p.model, c.full_name, c.city, c.contract_number
		ORDER BY m.report_date, m.prosthesis_serial`,
		username, from, to)
	if err != nil {
		return nil, nil, fmt.Errorf("query mart: %w", err)
	}
	defer rows.Close()

	var (
		days   []Day
		client *Client
	)
	for rows.Next() {
		var (
			day                            Day
			fullName, city, contractNumber string
		)
		if err := rows.Scan(
			&day.Date, &day.ProsthesisSerial, &day.Model,
			&day.ReadingsTotal, &day.GesturesRecognized, &day.RecognitionRate,
			&day.AvgLatencyMs, &day.P95LatencyMs, &day.MaxLatencyMs,
			&day.MinBatteryLevel, &day.AvgBatteryLevel, &day.AvgSignalQuality,
			&day.ErrorEvents, &fullName, &city, &contractNumber,
		); err != nil {
			return nil, nil, fmt.Errorf("scan mart row: %w", err)
		}
		if client == nil {
			client = &Client{FullName: fullName, City: city, ContractNumber: contractNumber}
		}
		days = append(days, day)
	}
	if err := rows.Err(); err != nil {
		return nil, nil, fmt.Errorf("read mart rows: %w", err)
	}

	return days, client, nil
}
