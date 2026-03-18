# Read-Write Separation (CQRS Pattern) Documentation

ระบบแยก Read และ Write Database Connections สำหรับ Course Service

## Architecture Overview

```
Client Requests
       |
       v
   Gin Router
       |
       +-- GET Requests  -----> Read Connection  -----> Read Database (Replica)
       |                        + Circuit Breaker
       |
       +-- POST/PUT/DELETE ----> Write Connection -----> Write Database (Master)
                                 + Circuit Breaker
```

## Database Connections

### 1. Read Connection (READ REPLICA)

**ใช้สำหรับ:**

- `GET /courses` - ดึงรายการ course ทั้งหมด
- `GET /courses/:id` - ดึง course เดี่ยว
- `GET /metrics/*` - ดูข้อมูล metrics ทั้งหมด

**Configuration:**

- Environment Variable: `DB_READ_HOST`
- Default: `localhost`
- Circuit Breaker: `Database-Read-Operations`

### 2. Write Connection (MASTER)

**ใช้สำหรับ:**

- `POST /courses` - สร้าง course ใหม่
- `PUT /courses/:id` - อัพเดท course
- `DELETE /courses/:id` - ลบ course
- Metrics Logging (background write)

**Configuration:**

- Environment Variable: `DB_WRITE_HOST`
- Default: `localhost`
- Circuit Breaker: `Database-Write-Operations`

## Environment Variables

### Development (Single Database)

```bash
# ไม่ต้อง set ค่าอะไร ระบบจะใช้ localhost เป็น default
go run .
```

### Production (Separate Read/Write Databases)

```bash
# Set แยก read และ write hosts
export DB_READ_HOST=read-replica.example.com
export DB_WRITE_HOST=master.example.com
go run .
```

### Docker Compose Configuration

```yaml
services:
  course-service:
    environment:
      - DB_READ_HOST=postgres-replica
      - DB_WRITE_HOST=postgres-master
```

## Circuit Breakers

### Read Circuit Breaker

- **Name:** Database-Read-Operations
- **Threshold:** 5 requests with 20% failure rate
- **Timeout:** 30 seconds
- **Max Requests (Half-Open):** 3

### Write Circuit Breaker

- **Name:** Database-Write-Operations
- **Threshold:** 5 requests with 20% failure rate
- **Timeout:** 30 seconds
- **Max Requests (Half-Open):** 3

## Benefits

### 1. **Scalability**

- Read replicas สามารถ scale horizontally ได้
- ลด load บน master database
- เพิ่มประสิทธิภาพการ query

### 2. **High Availability**

- ถ้า read replica ล้ม write ยังทำงานได้
- ถ้า master ล้ม read ยังทำงานได้
- Circuit breakers ป้องกัน cascading failures

### 3. **Performance**

- Read operations ไม่กระทบ write operations
- ลดการ lock บน master database
- แยก load ตามประเภทการใช้งาน

### 4. **Maintainability**

- สามารถ maintenance read replica โดยไม่กระทบ write
- ง่ายต่อการ backup จาก replica
- ลด downtime

## API Endpoints Summary

### Read Operations (Using Read Connection)

```bash
# ดึงข้อมูล courses
GET /courses
GET /courses/:id

# ดูข้อมูล metrics
GET /metrics/recent
GET /metrics/aggregate
GET /metrics/endpoint/:endpoint
GET /metrics/circuit-breaker
```

### Write Operations (Using Write Connection)

```bash
# จัดการข้อมูล courses
POST   /courses
PUT    /courses/:id
DELETE /courses/:id
```

## Testing

### 1. ทดสอบ Read Operations

```bash
# ใช้ read connection
curl http://localhost:8000/courses
curl http://localhost:8000/courses/1
curl http://localhost:8000/metrics/recent
```

### 2. ทดสอบ Write Operations

```bash
# ใช้ write connection
curl -X POST http://localhost:8000/courses \
  -H "Content-Type: application/json" \
  -d '{...}'

curl -X PUT http://localhost:8000/courses/1 \
  -H "Content-Type: application/json" \
  -d '{...}'

curl -X DELETE http://localhost:8000/courses/1
```

### 3. เช็คสถานะ Circuit Breakers

```bash
curl http://localhost:8000/metrics/circuit-breaker

# Response:
{
  "read_state": "closed",
  "write_state": "closed"
}
```

## Database Replication Setup (PostgreSQL)

### Master Configuration (postgresql.conf)

```conf
wal_level = replica
max_wal_senders = 3
wal_keep_size = 64
```

### Replica Configuration

```conf
hot_standby = on
```

### Create Replication User

```sql
-- On Master
CREATE USER replicator REPLICATION LOGIN PASSWORD 'password';
```

## Monitoring

### Key Metrics to Monitor

1. **Read Connection Status**
   - Connection pool usage
   - Query latency
   - Circuit breaker state

2. **Write Connection Status**
   - Connection pool usage
   - Transaction latency
   - Circuit breaker state

3. **Replication Lag**
   - Time difference between master and replica
   - Important for data consistency

## Future Enhancements

1. **Connection Pooling**
   - Implement pgxpool for better performance
   - Configure pool size based on load

2. **Load Balancing**
   - Multiple read replicas
   - Round-robin or least-connection algorithm

3. **Automatic Failover**
   - Detect master failure
   - Promote replica to master
   - Update application configuration

4. **Cache Layer**
   - Redis for frequently accessed data
   - Reduce load on read replicas

## Code Structure

```
course/
├── main.go                    # Main application with read/write separation
├── metrics.go                 # Metrics logging (uses write connection)
├── middleware.go              # Request metrics middleware
└── migrations/
    └── create_metrics_table.sql
```

## Notes

- **Data Consistency:** อาจมี replication lag ระหว่าง write และ read
- **Eventual Consistency:** Read operations อาจได้ข้อมูลที่ยังไม่ใหม่ล่าสุด
- **Write-After-Read:** ถ้าต้องการอ่านข้อมูลที่เพิ่ง write ควรอ่านจาก write connection
