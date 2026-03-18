# Metrics System Documentation

ระบบบันทึก Metrics/Statistics สำหรับ Course Service

## Database Tables

### 1. request_metrics

เก็บข้อมูลของแต่ละ request

- `id`: Primary key
- `timestamp`: เวลาที่ทำ request
- `endpoint`: URL path ที่เรียก
- `method`: HTTP method (GET, POST, PUT, DELETE)
- `status_code`: HTTP status code ที่ return
- `response_time_ms`: เวลาตอบสนอง (milliseconds)
- `circuit_breaker_state`: สถานะของ circuit breaker
- `error_message`: ข้อความ error (ถ้ามี)

### 2. aggregate_metrics

เก็บข้อมูลสถิติรวม (ยังไม่ได้ใช้ในเวอร์ชันนี้ แต่พร้อมสำหรับขยายต่อ)

## API Endpoints

### 1. ดูข้อมูล metrics ล่าสุด

```
GET /metrics/recent
```

**Response:**

```json
{
  "count": 100,
  "metrics": [
    {
      "Timestamp": "2026-03-19T10:30:00Z",
      "Endpoint": "/courses",
      "Method": "GET",
      "StatusCode": 200,
      "ResponseTimeMs": 45.5,
      "CircuitBreakerState": "closed",
      "ErrorMessage": ""
    }
  ]
}
```

### 2. ดูข้อมูลสถิติรวม (24 ชั่วโมง)

```
GET /metrics/aggregate
```

**Response:**

```json
{
  "start_time": "2026-03-18T10:00:00Z",
  "end_time": "2026-03-19T10:00:00Z",
  "count": 5,
  "metrics": [
    {
      "TimeWindow": "2026-03-19T10:00:00Z",
      "Endpoint": "/courses",
      "TotalRequests": 150,
      "SuccessfulRequests": 145,
      "FailedRequests": 5,
      "SuccessRate": 96.67,
      "AverageResponseTimeMs": 52.3,
      "ErrorRate": 3.33,
      "CircuitBreakerTrips": 0
    }
  ]
}
```

### 3. ดูสถิติของ endpoint เฉพาะ

```
GET /metrics/endpoint/:endpoint
```

**ตัวอย่าง:**

```
GET /metrics/endpoint/courses
```

**Response:**

```json
{
  "Endpoint": "/courses",
  "TotalRequests": 150,
  "SuccessfulRequests": 145,
  "FailedRequests": 5,
  "SuccessRate": 96.67,
  "AverageResponseTimeMs": 52.3,
  "ErrorRate": 3.33,
  "CircuitBreakerTrips": 0
}
```

### 4. ดูสถานะ circuit breaker

```
GET /metrics/circuit-breaker
```

**Response:**

```json
{
  "state": "closed",
  "name": "Database-Operations"
}
```

## Features

### 1. Automatic Logging

- ทุก request จะถูกบันทึกอัตโนมัติผ่าน middleware
- บันทึกแบบ async เพื่อไม่กระทบ performance

### 2. Metrics Tracking

- **Total Requests**: จำนวน request ทั้งหมด
- **Success Rate**: เปอร์เซ็นต์ความสำเร็จ
- **Average Response Time**: เวลาตอบสนองเฉลี่ย
- **Error Rate**: เปอร์เซ็นต์ข้อผิดพลาด
- **Circuit Breaker Status**: ติดตามสถานะ circuit breaker

### 3. Time-based Analysis

- ดูข้อมูลแบบ real-time (recent metrics)
- วิเคราะห์แบบ time window (hourly aggregation)
- กรองตาม endpoint

## การใช้งาน

### 1. ติดตั้งตาราง

```bash
psql -U postgres -d register -f migrations/create_metrics_table.sql
```

### 2. รัน service

```bash
go run .
```

### 3. ทดสอบ

```bash
# ทดสอบ request
curl http://localhost:8000/courses

# ดู metrics
curl http://localhost:8000/metrics/recent
curl http://localhost:8000/metrics/aggregate
curl http://localhost:8000/metrics/endpoint/courses
curl http://localhost:8000/metrics/circuit-breaker
```

## Performance Considerations

1. **Async Logging**: การบันทึก metrics ทำแบบ async เพื่อไม่กระทบ performance ของ main request
2. **Database Indexes**: มี indexes บน timestamp และ endpoint สำหรับ query ที่เร็วขึ้น
3. **Batch Processing**: สามารถขยายเป็น batch processing ได้ในอนาคต

## Future Enhancements

1. Dashboard UI สำหรับแสดง metrics
2. Alert system เมื่อ metrics ผิดปกติ
3. Export ข้อมูลเป็น Prometheus format
4. Data retention policy (ลบข้อมูลเก่าอัตโนมัติ)
