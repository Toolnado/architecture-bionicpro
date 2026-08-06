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

func (s *Store) Days(ctx context.Context, username string, from, to time.Time) ([]Day, *Client, error) {
	rows, err := s.conn.Query(ctx, `
		SELECT report_date,
		       prosthesis_serial,
		       model,
		       readings_total,
		       gestures_recognized,
		       recognition_rate,
		       avg_latency_ms,
		       p95_latency_ms,
		       max_latency_ms,
		       min_battery_level,
		       avg_battery_level,
		       avg_signal_quality,
		       error_events,
		       full_name,
		       city,
		       contract_number
		FROM report_daily FINAL
		WHERE username = ? AND report_date BETWEEN ? AND ?
		ORDER BY report_date, prosthesis_serial`,
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
