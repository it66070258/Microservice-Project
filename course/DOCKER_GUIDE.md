# Docker Deployment Guide

คู่มือการใช้งาน Course Service ด้วย Docker

## Architecture

```
┌─────────────────────────────────────────┐
│          Docker Network                 │
│                                         │
│  ┌──────────────┐  ┌──────────────┐   │
│  │  PostgreSQL  │  │  PostgreSQL  │   │
│  │   Master     │  │   Replica    │   │
│  │  (Write DB)  │  │   (Read DB)  │   │
│  │  Port: 5432  │  │  Port: 5433  │   │
│  └──────┬───────┘  └──────┬───────┘   │
│         │                  │            │
│         │                  │            │
│  ┌──────┴──────────────────┴───────┐   │
│  │      Course Service             │   │
│  │      Port: 8000                 │   │
│  └─────────────────────────────────┘   │
│                                         │
└─────────────────────────────────────────┘
```

## Prerequisites

- Docker Desktop (Windows/Mac) or Docker Engine (Linux)
- Docker Compose

## Quick Start

### 1. Build และ Start Services

```bash
# Windows PowerShell
cd c:\Microservice-Project\course
docker-compose up -d

# หรือใช้ make (ถ้าติดตั้ง make แล้ว)
make docker-up
```

### 2. ตรวจสอบ Status

```bash
# เช็คว่า containers รันอยู่
docker-compose ps

# ดู logs
docker-compose logs -f
```

### 3. รอให้ Services พร้อม

รอประมาณ 10-15 วินาทีให้ PostgreSQL initialize เสร็จ

### 4. ทดสอบ API

```bash
# Windows PowerShell
powershell -File test-api.ps1

# Git Bash / WSL
bash test-api.sh

# หรือทดสอบด้วย curl
curl http://localhost:8000/courses
```

## Docker Commands

### Start Services

```bash
docker-compose up -d
```

### Stop Services

```bash
docker-compose down
```

### Restart Services

```bash
docker-compose restart
```

### View Logs

```bash
# ดู logs ทั้งหมด
docker-compose logs -f

# ดู logs เฉพาะ service
docker-compose logs -f course-service
docker-compose logs -f postgres-master
```

### Clean Up (ลบทุกอย่าง)

```bash
# ลบ containers และ volumes
docker-compose down -v

# ลบ images ด้วย
docker-compose down -v --rmi all
```

## Environment Variables

แก้ไขใน `docker-compose.yml`:

```yaml
course-service:
  environment:
    DB_READ_HOST: postgres-replica # Read database host
    DB_WRITE_HOST: postgres-master # Write database host
```

## Port Mappings

| Service            | Container Port | Host Port |
| ------------------ | -------------- | --------- |
| Course Service     | 8000           | 8000      |
| PostgreSQL Master  | 5432           | 5432      |
| PostgreSQL Replica | 5432           | 5433      |

## Testing

### 1. ใช้ Test Script

**Windows:**

```powershell
powershell -File test-api.ps1
```

**Linux/Mac/Git Bash:**

```bash
chmod +x test-api.sh
./test-api.sh
```

### 2. ทดสอบด้วย curl

```bash
# Get all courses
curl http://localhost:8000/courses

# Get single course
curl http://localhost:8000/courses/1

# Create course
curl -X POST http://localhost:8000/courses \
  -H "Content-Type: application/json" \
  -d '{
    "course_id": 999,
    "subject": "Test Course",
    "credit": 3,
    "section": ["1"],
    "day_of_week": "Monday",
    "start_time": "09:00:00",
    "end_time": "12:00:00",
    "capacity": 30,
    "state": "open"
  }'

# Update course
curl -X PUT http://localhost:8000/courses/999 \
  -H "Content-Type: application/json" \
  -d '{"state": "closed"}'

# Delete course
curl -X DELETE http://localhost:8000/courses/999

# Check circuit breaker
curl http://localhost:8000/metrics/circuit-breaker

# Get metrics
curl http://localhost:8000/metrics/recent
curl http://localhost:8000/metrics/aggregate
curl http://localhost:8000/metrics/endpoint/courses
```

### 3. ทดสอบ Go Unit Tests

```bash
# รัน tests ภายใน container
docker-compose exec course-service go test -v

# หรือรัน tests บนเครื่อง local
go test -v
```

## Database Access

### เชื่อมต่อ PostgreSQL Master (Write)

```bash
# ใช้ psql
docker exec -it course-postgres-master psql -U postgres -d register

# หรือจากเครื่อง local
psql -U postgres -h localhost -p 5432 -d register
```

### เชื่อมต่อ PostgreSQL Replica (Read)

```bash
# ใช้ psql
docker exec -it course-postgres-replica psql -U postgres -d register

# หรือจากเครื่อง local
psql -U postgres -h localhost -p 5433 -d register
```

### Query ตัวอย่าง

```sql
-- ดูข้อมูล courses
SELECT * FROM course;

-- ดู metrics
SELECT * FROM request_metrics ORDER BY timestamp DESC LIMIT 10;

-- ดูสถิติ
SELECT
    endpoint,
    COUNT(*) as total_requests,
    AVG(response_time_ms) as avg_response_time
FROM request_metrics
GROUP BY endpoint;
```

## Troubleshooting

### Container ไม่ start

```bash
# ดู logs
docker-compose logs course-service

# ลอง restart
docker-compose restart

# ลองลบแล้ว start ใหม่
docker-compose down -v
docker-compose up -d
```

### Database connection error

```bash
# เช็คว่า PostgreSQL รันอยู่
docker-compose ps

# เช็ค health status
docker inspect course-postgres-master | grep -A 5 Health

# รอให้ database พร้อม
docker-compose logs postgres-master
```

### Port already in use

```bash
# หา process ที่ใช้ port 8000
netstat -ano | findstr :8000

# หรือเปลี่ยน port ใน docker-compose.yml
ports:
  - "8001:8000"  # เปลี่ยนจาก 8000 เป็น 8001
```

## Development Workflow

### 1. แก้ไขโค้ด

```bash
# แก้ไขไฟล์ .go
```

### 2. Rebuild Image

```bash
docker-compose build course-service
```

### 3. Restart Service

```bash
docker-compose up -d course-service
```

### 4. ทดสอบ

```bash
powershell -File test-api.ps1
```

## Production Considerations

### 1. ใช้ Production Database

แก้ไข `docker-compose.yml`:

```yaml
course-service:
  environment:
    DB_READ_HOST: your-read-db.example.com
    DB_WRITE_HOST: your-write-db.example.com
```

### 2. ตั้งค่า Resource Limits

```yaml
course-service:
  deploy:
    resources:
      limits:
        cpus: "1"
        memory: 512M
      reservations:
        cpus: "0.5"
        memory: 256M
```

### 3. Health Checks

```yaml
course-service:
  healthcheck:
    test: ["CMD", "curl", "-f", "http://localhost:8000/metrics/circuit-breaker"]
    interval: 30s
    timeout: 10s
    retries: 3
```

### 4. Logging

```yaml
course-service:
  logging:
    driver: "json-file"
    options:
      max-size: "10m"
      max-file: "3"
```

## Monitoring

### View Container Stats

```bash
docker stats
```

### View Metrics via API

```bash
# Circuit breaker status
curl http://localhost:8000/metrics/circuit-breaker

# Recent requests
curl http://localhost:8000/metrics/recent

# Aggregate statistics
curl http://localhost:8000/metrics/aggregate
```

## Backup and Restore

### Backup Database

```bash
docker exec course-postgres-master pg_dump -U postgres register > backup.sql
```

### Restore Database

```bash
docker exec -i course-postgres-master psql -U postgres register < backup.sql
```

## Additional Resources

- [Docker Compose Documentation](https://docs.docker.com/compose/)
- [PostgreSQL Docker Image](https://hub.docker.com/_/postgres)
- Project Documentation: `README.md`
- API Documentation: `METRICS_README.md`
- Architecture: `ARCHITECTURE_DIAGRAM.md`
