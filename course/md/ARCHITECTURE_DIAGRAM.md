# Course Service Architecture Diagram

## Overall Architecture with Read-Write Separation

```
┌─────────────────────────────────────────────────────────────────┐
│                         Client Requests                         │
└────────────────────────────┬────────────────────────────────────┘
                             │
                             v
┌─────────────────────────────────────────────────────────────────┐
│                      Gin HTTP Server                            │
│                      (Port 8000)                                │
└────────────────────────────┬────────────────────────────────────┘
                             │
                             v
┌─────────────────────────────────────────────────────────────────┐
│                   Metrics Middleware                            │
│              (Log all requests to database)                     │
└────────────────────────────┬────────────────────────────────────┘
                             │
                ┌────────────┴────────────┐
                │                         │
                v                         v
        ┌───────────────┐        ┌───────────────┐
        │  READ Routes  │        │ WRITE Routes  │
        │               │        │               │
        │ GET /courses  │        │ POST /courses │
        │ GET /courses/:id       │ PUT /courses/:id
        │ GET /metrics/*│        │ DELETE /courses/:id
        └───────┬───────┘        └───────┬───────┘
                │                         │
                v                         v
        ┌───────────────┐        ┌───────────────┐
        │ Read Circuit  │        │ Write Circuit │
        │   Breaker     │        │   Breaker     │
        │               │        │               │
        │ Threshold: 5  │        │ Threshold: 5  │
        │ Timeout: 30s  │        │ Timeout: 30s  │
        └───────┬───────┘        └───────┬───────┘
                │                         │
                v                         v
        ┌───────────────┐        ┌───────────────┐
        │ Read Database │        │ Write Database│
        │  Connection   │        │  Connection   │
        │               │        │               │
        │ (Replica)     │        │ (Master)      │
        └───────┬───────┘        └───────┬───────┘
                │                         │
                v                         v
        ┌───────────────┐        ┌───────────────┐
        │   PostgreSQL  │◄───────│   PostgreSQL  │
        │     Replica   │ Repl.  │     Master    │
        │               │        │               │
        │   (Read DB)   │        │  (Write DB)   │
        └───────────────┘        └───────────────┘
```

## Request Flow Examples

### GET Request Flow (Read Operation)

```
Client
  │
  ├─ GET /courses
  │
  v
Gin Router
  │
  ├─ Metrics Middleware (records request start time)
  │
  v
Read Circuit Breaker
  │
  ├─ Check: Is circuit CLOSED?
  │  ├─ YES → Allow request
  │  └─ NO → Return 503 Service Unavailable
  │
  v
Read Database Connection
  │
  ├─ Execute: SELECT * FROM course
  │
  v
PostgreSQL Replica
  │
  ├─ Query data
  │
  v
Response
  │
  ├─ 200 OK + JSON data
  │
  v
Metrics Middleware
  │
  ├─ Calculate response time
  ├─ Log to request_metrics table (async, using Write Connection)
  │
  v
Client receives response
```

### POST Request Flow (Write Operation)

```
Client
  │
  ├─ POST /courses (with JSON body)
  │
  v
Gin Router
  │
  ├─ Metrics Middleware (records request start time)
  │
  v
Request Validation
  │
  ├─ Validate JSON body
  │
  v
Write Circuit Breaker
  │
  ├─ Check: Is circuit CLOSED?
  │  ├─ YES → Allow request
  │  └─ NO → Return 503 Service Unavailable
  │
  v
Write Database Connection
  │
  ├─ Execute: INSERT INTO course VALUES (...)
  │
  v
PostgreSQL Master
  │
  ├─ Insert data
  ├─ Replicate to Replica (async)
  │
  v
Response
  │
  ├─ 201 Created
  │
  v
Metrics Middleware
  │
  ├─ Calculate response time
  ├─ Log to request_metrics table (async, using Write Connection)
  │
  v
Client receives response
```

## Circuit Breaker State Diagram

```
                    ┌──────────┐
                    │  CLOSED  │
                    │ (Normal) │
                    └────┬─────┘
                         │
         ┌───────────────┴───────────────┐
         │ 5+ requests with 20% failures │
         v                               │
    ┌─────────┐                         │
    │  OPEN   │                         │ Success
    │(Failing)│                         │
    └────┬────┘                         │
         │                              │
         │ Wait 30 seconds              │
         v                              │
    ┌──────────┐                        │
    │ HALF_OPEN│                        │
    │ (Testing)│                        │
    └────┬─────┘                        │
         │                              │
         ├─ 3 successes ────────────────┘
         │
         └─ 1 failure ──> Back to OPEN
```

## Database Schema

### course table (Read & Write)

```
┌────────────────┬──────────────┬────────────┐
│ Column         │ Type         │ Constraint │
├────────────────┼──────────────┼────────────┤
│ course_id      │ INTEGER      │ PK         │
│ subject        │ VARCHAR(255) │ NOT NULL   │
│ credit         │ INTEGER      │ NOT NULL   │
│ section        │ TEXT[]       │            │
│ day_of_week    │ VARCHAR(50)  │            │
│ start_time     │ TIME         │            │
│ end_time       │ TIME         │            │
│ capacity       │ INTEGER      │            │
│ state          │ VARCHAR(50)  │            │
│ current_student│ TEXT[]       │            │
│ prerequisite   │ TEXT[]       │            │
└────────────────┴──────────────┴────────────┘
```

### request_metrics table (Write only, Read for queries)

```
┌─────────────────────┬───────────┬────────────┐
│ Column              │ Type      │ Constraint │
├─────────────────────┼───────────┼────────────┤
│ id                  │ SERIAL    │ PK         │
│ timestamp           │ TIMESTAMP │ NOT NULL   │
│ endpoint            │ VARCHAR   │ NOT NULL   │
│ method              │ VARCHAR   │ NOT NULL   │
│ status_code         │ INTEGER   │ NOT NULL   │
│ response_time_ms    │ FLOAT     │ NOT NULL   │
│ circuit_breaker_state│ VARCHAR  │            │
│ error_message       │ TEXT      │            │
│ created_at          │ TIMESTAMP │ DEFAULT NOW│
└─────────────────────┴───────────┴────────────┘
```

## Deployment Scenarios

### Scenario 1: Development (Single Database)

```
┌──────────────────┐
│ Course Service   │
│                  │
│ Read: localhost  │
│ Write: localhost │
└────────┬─────────┘
         │
         v
┌──────────────────┐
│ PostgreSQL       │
│ localhost:5432   │
└──────────────────┘
```

### Scenario 2: Production (Master-Replica)

```
┌──────────────────┐
│ Course Service   │
│                  │
│ Read:  replica   │
│ Write: master    │
└────┬─────┬───────┘
     │     │
     │     └────────────────┐
     v                      v
┌──────────────┐     ┌──────────────┐
│ PostgreSQL   │     │ PostgreSQL   │
│ Replica      │◄────│ Master       │
│ (Read Only)  │Repl.│ (Read/Write) │
└──────────────┘     └──────────────┘
```

### Scenario 3: Production (Master + Multiple Replicas)

```
                ┌──────────────────┐
                │ Course Service   │
                │                  │
                │ Read:  replicas  │
                │ Write: master    │
                └────┬─────┬───────┘
                     │     │
        ┌────────────┤     └─────────────────┐
        │            │                       │
        v            v                       v
┌──────────┐  ┌──────────┐          ┌──────────────┐
│PostgreSQL│  │PostgreSQL│          │ PostgreSQL   │
│Replica 1 │  │Replica 2 │          │ Master       │
│(Read)    │  │(Read)    │◄─────────│ (Write)      │
└──────────┘  └──────────┘  Repl.   └──────────────┘
```

## Performance Benefits

### Without Read-Write Separation

```
All Requests → Master DB (100% load)
├─ Read:  80% of traffic
└─ Write: 20% of traffic

Result: Master is bottleneck
```

### With Read-Write Separation

```
Read Requests  (80%) → Replica (80% load)
Write Requests (20%) → Master  (20% load)

Result: Balanced load, better performance
```

## Scalability

```
Traffic Increase:
  100 req/s → 1000 req/s → 10000 req/s

Without Separation:
  Master: 100 → 1000 → 10000 (Overloaded!)

With Separation:
  Master:  20 → 200  → 2000  (OK)
  Replica: 80 → 800  → 8000  (Add more replicas)

  Can scale horizontally by adding replicas!
```
