-- สร้างตาราง request_metrics สำหรับเก็บข้อมูลสถิติของแต่ละ request
CREATE TABLE IF NOT EXISTS request_metrics (
    id SERIAL PRIMARY KEY,
    timestamp TIMESTAMP NOT NULL DEFAULT NOW(),
    endpoint VARCHAR(255) NOT NULL,
    method VARCHAR(10) NOT NULL,
    status_code INTEGER NOT NULL,
    response_time_ms FLOAT NOT NULL,
    circuit_breaker_state VARCHAR(20),
    error_message TEXT,
    created_at TIMESTAMP DEFAULT NOW()
);

-- สร้างตาราง aggregate_metrics สำหรับเก็บข้อมูลสถิติรวม (aggregated)
CREATE TABLE IF NOT EXISTS aggregate_metrics (
    id SERIAL PRIMARY KEY,
    time_window TIMESTAMP NOT NULL,
    endpoint VARCHAR(255) NOT NULL,
    total_requests INTEGER NOT NULL DEFAULT 0,
    successful_requests INTEGER NOT NULL DEFAULT 0,
    failed_requests INTEGER NOT NULL DEFAULT 0,
    success_rate FLOAT NOT NULL DEFAULT 0,
    average_response_time_ms FLOAT NOT NULL DEFAULT 0,
    error_rate FLOAT NOT NULL DEFAULT 0,
    circuit_breaker_trips INTEGER NOT NULL DEFAULT 0,
    created_at TIMESTAMP DEFAULT NOW(),
    UNIQUE(time_window, endpoint)
);

-- สร้าง index สำหรับ query ที่ใช้บ่อย
CREATE INDEX IF NOT EXISTS idx_request_metrics_timestamp ON request_metrics(timestamp);
CREATE INDEX IF NOT EXISTS idx_request_metrics_endpoint ON request_metrics(endpoint);
CREATE INDEX IF NOT EXISTS idx_aggregate_metrics_time_window ON aggregate_metrics(time_window);
CREATE INDEX IF NOT EXISTS idx_aggregate_metrics_endpoint ON aggregate_metrics(endpoint);
