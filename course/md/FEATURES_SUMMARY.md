# Course Service - Complete Features Summary

## 🎯 Features Overview

### 1. ✅ **CRUD Operations**

- GET `/courses` - ดึงรายการ course ทั้งหมด
- GET `/courses/:id` - ดึง course เดี่ยว
- POST `/courses` - สร้าง course ใหม่
- PUT `/courses/:id` - อัพเดท course
- DELETE `/courses/:id` - ลบ course

### 2. ✅ **Circuit Breaker Pattern**

- ใช้ `gobreaker` library
- แยก circuit breaker สำหรับ read และ write
- Configuration:
  - Failure threshold: 5 requests with 20% failure rate
  - Timeout: 30 seconds
  - Recovery: Half-open state with 3 test requests
- Endpoint: GET `/metrics/circuit-breaker` - ดูสถานะ

### 3. ✅ **Metrics & Statistics**

- บันทึก metrics ทุก request อัตโนมัติ
- ข้อมูลที่เก็บ:
  - Total requests
  - Success rate
  - Average response time
  - Error rate
  - Circuit breaker trips
- Endpoints:
  - GET `/metrics/recent` - ดู metrics ล่าสุด 100 รายการ
  - GET `/metrics/aggregate` - ดูสถิติรวม 24 ชั่วโมง
  - GET `/metrics/endpoint/:endpoint` - ดูสถิติของ endpoint เฉพาะ

### 4. ✅ **Read-Write Separation (CQRS)**

- แยก database connections:
  - Read Connection → PostgreSQL Replica (Read operations)
  - Write Connection → PostgreSQL Master (Write operations)
- Circuit breakers แยกสำหรับ read/write
- Environment variables:
  - `DB_READ_HOST` - Read database host
  - `DB_WRITE_HOST` - Write database host

### 5. ✅ **Docker Support**

- Multi-stage Dockerfile (build stage + run stage)
- Docker Compose setup:
  - PostgreSQL Master (port 5432)
  - PostgreSQL Replica (port 5433)
  - Course Service (port 8000)
- Health checks
- Volume persistence

### 6. ✅ **Testing**

- Unit tests (9 tests)
  - GET operations
  - POST operations
  - PUT operations
  - DELETE operations
  - Error cases (404, 400)
- API testing scripts:
  - `test-api.sh` (Bash)
  - `test-api.ps1` (PowerShell)
- Test coverage: All CRUD operations + metrics

## 📊 Database Schema

### `course` table

```sql
- course_id      INTEGER PRIMARY KEY
- subject        VARCHAR(255)
- credit         INTEGER
- section        TEXT[]
- day_of_week    VARCHAR(50)
- start_time     TIME
- end_time       TIME
- capacity       INTEGER
- state          VARCHAR(50)
- current_student TEXT[]
- prerequisite   TEXT[]
```

### `request_metrics` table

```sql
- id             SERIAL PRIMARY KEY
- timestamp      TIMESTAMP
- endpoint       VARCHAR(255)
- method         VARCHAR(10)
- status_code    INTEGER
- response_time_ms FLOAT
- circuit_breaker_state VARCHAR(20)
- error_message  TEXT
```

## 🏗️ Architecture

```
Client
  │
  ├─ READ Requests  ──> Read Circuit Breaker ──> PostgreSQL Replica
  │
  └─ WRITE Requests ──> Write Circuit Breaker ──> PostgreSQL Master
                                                       │
                                                       ├─ Replication
                                                       └──> PostgreSQL Replica
```

## 🚀 Quick Start

### Local Development

```bash
# Run with Go
go run .

# Run tests
go test -v
```

### Docker

```bash
# Start all services
docker-compose up -d

# Run API tests
powershell -File test-api.ps1

# View logs
docker-compose logs -f

# Stop services
docker-compose down
```

## 📁 Project Structure

```
course/
├── main.go                      # Main application
├── metrics.go                   # Metrics logging
├── middleware.go                # Metrics middleware
├── main_test.go                 # Unit tests
├── Dockerfile                   # Docker build instructions
├── docker-compose.yml           # Docker orchestration
├── Makefile                     # Build automation
├── test-api.sh                  # API tests (Bash)
├── test-api.ps1                 # API tests (PowerShell)
├── migrations/
│   └── create_metrics_table.sql # Database migration
└── docs/
    ├── METRICS_README.md        # Metrics documentation
    ├── READ_WRITE_SEPARATION.md # CQRS documentation
    ├── ARCHITECTURE_DIAGRAM.md  # Architecture diagrams
    ├── DOCKER_GUIDE.md          # Docker guide
    └── TEST_UPDATES.md          # Test documentation
```

## 🔧 Configuration

### Environment Variables

- `DB_HOST` - Database host (legacy, single connection)
- `DB_READ_HOST` - Read database host (default: localhost)
- `DB_WRITE_HOST` - Write database host (default: localhost)

### Circuit Breaker Settings

```go
// Read Circuit Breaker
Name: "Database-Read-Operations"
MaxRequests: 3
Interval: 1 minute
Timeout: 30 seconds
ReadyToTrip: 5 requests with 20% failure rate

// Write Circuit Breaker
Name: "Database-Write-Operations"
MaxRequests: 3
Interval: 1 minute
Timeout: 30 seconds
ReadyToTrip: 5 requests with 20% failure rate
```

## 📊 Performance Benefits

### Without Read-Write Separation

```
All Requests → Master DB (100% load)
- Bottleneck on master
- Cannot scale horizontally
```

### With Read-Write Separation

```
Read (80%)  → Replica  (distributed load)
Write (20%) → Master   (focused load)
- Balanced load
- Horizontal scaling
- Better performance
```

## 🧪 Test Results

```
✅ TestGetCourses_Success       - PASS
✅ TestGetCourse_Success        - PASS
✅ TestGetCourse_NotFound       - PASS
✅ TestCreateCourse_Success     - PASS
✅ TestCreateCourse_BadRequest  - PASS
✅ TestUpdateCourse_Success     - PASS
✅ TestUpdateCourse_NotFound    - PASS
✅ TestDeleteCourse_Success     - PASS
✅ TestDeleteCourse_NotFound    - PASS

PASS - ok Microservice-Project 0.357s
```

## 📈 Scalability

### Current Setup

- 1 Master (Write)
- 1 Replica (Read)
- 1 Course Service

### Future Scaling

- 1 Master (Write)
- N Replicas (Read) - scale horizontally
- N Course Services - scale horizontally with load balancer

## 🔐 Security Considerations

- ✅ Input validation (Gin binding)
- ✅ SQL injection prevention (parameterized queries)
- ✅ Error handling with circuit breaker
- ❌ Authentication (TODO: implement JWT/OAuth)
- ❌ Rate limiting (TODO: implement)
- ❌ HTTPS (TODO: add TLS certificates)

## 🎯 Future Enhancements

1. **Authentication & Authorization**
   - JWT tokens
   - Role-based access control

2. **Caching Layer**
   - Redis for frequently accessed data
   - Cache invalidation strategy

3. **Message Queue**
   - RabbitMQ for async operations
   - Event-driven architecture

4. **Monitoring & Observability**
   - Prometheus metrics export
   - Grafana dashboards
   - Distributed tracing (Jaeger)

5. **API Gateway**
   - Kong or Nginx
   - Rate limiting
   - API versioning

6. **Load Balancing**
   - Multiple service instances
   - Health checks
   - Auto-scaling

## 📝 API Documentation

### Base URL

```
http://localhost:8000
```

### Endpoints

#### Courses

- `GET /courses` - Get all courses
- `GET /courses/:id` - Get course by ID
- `POST /courses` - Create new course
- `PUT /courses/:id` - Update course
- `DELETE /courses/:id` - Delete course

#### Metrics

- `GET /metrics/recent` - Recent metrics (100 records)
- `GET /metrics/aggregate` - Aggregate statistics (24 hours)
- `GET /metrics/endpoint/:endpoint` - Endpoint statistics
- `GET /metrics/circuit-breaker` - Circuit breaker status

## 🏆 Best Practices Implemented

- ✅ Clean code structure
- ✅ Error handling
- ✅ Unit testing
- ✅ Docker containerization
- ✅ Environment-based configuration
- ✅ Logging and monitoring
- ✅ Circuit breaker pattern
- ✅ CQRS pattern
- ✅ Database connection pooling
- ✅ Graceful shutdown support

## 📚 Documentation

- `METRICS_README.md` - Metrics system documentation
- `READ_WRITE_SEPARATION.md` - CQRS pattern explanation
- `ARCHITECTURE_DIAGRAM.md` - Architecture diagrams and flows
- `DOCKER_GUIDE.md` - Docker deployment guide
- `TEST_UPDATES.md` - Test documentation

## 🤝 Contributing

1. Clone repository
2. Create feature branch
3. Make changes
4. Run tests: `go test -v`
5. Build: `go build`
6. Test with Docker: `docker-compose up`
7. Submit pull request

## 📄 License

This project is part of Microservice-Project course.
