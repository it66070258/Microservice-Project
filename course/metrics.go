package main

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5"
)

// RequestMetric เก็บข้อมูล metric ของแต่ละ request
type RequestMetric struct {
	Timestamp           time.Time
	Endpoint            string
	Method              string
	StatusCode          int
	ResponseTimeMs      float64
	CircuitBreakerState string
	ErrorMessage        string
}

// AggregateMetric เก็บข้อมูลสถิติรวม
type AggregateMetric struct {
	TimeWindow            time.Time
	Endpoint              string
	TotalRequests         int
	SuccessfulRequests    int
	FailedRequests        int
	SuccessRate           float64
	AverageResponseTimeMs float64
	ErrorRate             float64
	CircuitBreakerTrips   int
}

// MetricsLogger จัดการการบันทึก metrics
type MetricsLogger struct {
	conn *pgx.Conn
}

// NewMetricsLogger สร้าง MetricsLogger ใหม่
func NewMetricsLogger(conn *pgx.Conn) *MetricsLogger {
	return &MetricsLogger{conn: conn}
}

// LogRequest บันทึก request metric
func (ml *MetricsLogger) LogRequest(metric RequestMetric) error {
	_, err := ml.conn.Exec(context.Background(),
		`INSERT INTO request_metrics (timestamp, endpoint, method, status_code, response_time_ms, circuit_breaker_state, error_message)
		VALUES ($1, $2, $3, $4, $5, $6, $7)`,
		metric.Timestamp,
		metric.Endpoint,
		metric.Method,
		metric.StatusCode,
		metric.ResponseTimeMs,
		metric.CircuitBreakerState,
		metric.ErrorMessage,
	)
	return err
}

// GetAggregateMetrics ดึงข้อมูลสถิติรวมตาม time window
func (ml *MetricsLogger) GetAggregateMetrics(startTime, endTime time.Time) ([]AggregateMetric, error) {
	rows, err := ml.conn.Query(context.Background(),
		`SELECT
			DATE_TRUNC('hour', timestamp) as time_window,
			endpoint,
			COUNT(*) as total_requests,
			COUNT(*) FILTER (WHERE status_code < 400) as successful_requests,
			COUNT(*) FILTER (WHERE status_code >= 400) as failed_requests,
			ROUND((COUNT(*) FILTER (WHERE status_code < 400)::FLOAT / COUNT(*)::FLOAT * 100)::numeric, 2) as success_rate,
			ROUND(AVG(response_time_ms)::numeric, 2) as average_response_time_ms,
			ROUND((COUNT(*) FILTER (WHERE status_code >= 400)::FLOAT / COUNT(*)::FLOAT * 100)::numeric, 2) as error_rate,
			COUNT(*) FILTER (WHERE circuit_breaker_state = 'OPEN') as circuit_breaker_trips
		FROM request_metrics
		WHERE timestamp >= $1 AND timestamp <= $2
		GROUP BY time_window, endpoint
		ORDER BY time_window DESC, endpoint`,
		startTime,
		endTime,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var metrics []AggregateMetric
	for rows.Next() {
		var metric AggregateMetric
		err := rows.Scan(
			&metric.TimeWindow,
			&metric.Endpoint,
			&metric.TotalRequests,
			&metric.SuccessfulRequests,
			&metric.FailedRequests,
			&metric.SuccessRate,
			&metric.AverageResponseTimeMs,
			&metric.ErrorRate,
			&metric.CircuitBreakerTrips,
		)
		if err != nil {
			return nil, err
		}
		metrics = append(metrics, metric)
	}

	return metrics, nil
}

// GetRecentMetrics ดึงข้อมูล metrics ล่าสุด
func (ml *MetricsLogger) GetRecentMetrics(limit int) ([]RequestMetric, error) {
	rows, err := ml.conn.Query(context.Background(),
		`SELECT timestamp, endpoint, method, status_code, response_time_ms, circuit_breaker_state, error_message
		FROM request_metrics
		ORDER BY timestamp DESC
		LIMIT $1`,
		limit,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var metrics []RequestMetric
	for rows.Next() {
		var metric RequestMetric
		err := rows.Scan(
			&metric.Timestamp,
			&metric.Endpoint,
			&metric.Method,
			&metric.StatusCode,
			&metric.ResponseTimeMs,
			&metric.CircuitBreakerState,
			&metric.ErrorMessage,
		)
		if err != nil {
			return nil, err
		}
		metrics = append(metrics, metric)
	}

	return metrics, nil
}

// GetEndpointStats ดึงสถิติของ endpoint เฉพาะ
func (ml *MetricsLogger) GetEndpointStats(endpoint string, hours int) (*AggregateMetric, error) {
	startTime := time.Now().Add(-time.Duration(hours) * time.Hour)

	var metric AggregateMetric
	err := ml.conn.QueryRow(context.Background(),
		`SELECT
			$1::text as endpoint,
			COUNT(*) as total_requests,
			COUNT(*) FILTER (WHERE status_code < 400) as successful_requests,
			COUNT(*) FILTER (WHERE status_code >= 400) as failed_requests,
			ROUND((COUNT(*) FILTER (WHERE status_code < 400)::FLOAT / NULLIF(COUNT(*), 0)::FLOAT * 100)::numeric, 2) as success_rate,
			ROUND(AVG(response_time_ms)::numeric, 2) as average_response_time_ms,
			ROUND((COUNT(*) FILTER (WHERE status_code >= 400)::FLOAT / NULLIF(COUNT(*), 0)::FLOAT * 100)::numeric, 2) as error_rate,
			COUNT(*) FILTER (WHERE circuit_breaker_state = 'OPEN') as circuit_breaker_trips
		FROM request_metrics
		WHERE endpoint = $1 AND timestamp >= $2`,
		endpoint,
		startTime,
	).Scan(
		&metric.Endpoint,
		&metric.TotalRequests,
		&metric.SuccessfulRequests,
		&metric.FailedRequests,
		&metric.SuccessRate,
		&metric.AverageResponseTimeMs,
		&metric.ErrorRate,
		&metric.CircuitBreakerTrips,
	)

	if err != nil {
		return nil, err
	}

	return &metric, nil
}
