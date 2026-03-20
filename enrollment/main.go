package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/lib/pq"
	_ "github.com/lib/pq"
	amqp "github.com/rabbitmq/amqp091-go"
	"github.com/sony/gobreaker"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

var (
	httpRequestsTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "http_requests_total",
			Help: "Total number of HTTP requests",
		},
		[]string{"method", "path", "status"},
	)
	httpDuration = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Name: "http_request_duration_seconds",
			Help: "Duration of HTTP requests in seconds",
		},
		[]string{"method", "path"},
	)
)

func init() {
	prometheus.MustRegister(httpRequestsTotal)
	prometheus.MustRegister(httpDuration)
}

func PrometheusMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		start := time.Now()
		c.Next()
		duration := time.Since(start).Seconds()

		status := fmt.Sprintf("%d", c.Writer.Status())
		method := c.Request.Method
		path := c.FullPath()
		if path == "" {
			path = c.Request.URL.Path
		}

		httpRequestsTotal.WithLabelValues(method, path, status).Inc()
		httpDuration.WithLabelValues(method, path).Observe(duration)
	}
}

// --- โครงสร้างข้อมูล ---

type EnrollmentRequest struct {
	StudentID int   `json:"student_id" binding:"required"`
	CourseIDs []int `json:"course_ids" binding:"required"`
}

type CourseDB struct {
	ID              int
	Credit          int
	Capacity        int
	CurrentStudents []string
	Prerequisite    []string
	DayOfWeek       string
	StartTime       string
	EndTime         string
	State           string
}

type StudentDB struct {
	GradedSubjects []string
}

// --- ตัวแปร Global ---

var (
	db            *sql.DB
	cb            *gobreaker.CircuitBreaker
	rabbitConn    *amqp.Connection
	rabbitChannel *amqp.Channel
)

const queueName = "enrollment_queue"

func main() {
	var err error

	// 1. เชื่อมต่อ Database
	host := os.Getenv("DB_HOST")
	if host == "" {
		host = "localhost"
	}
	connStr := fmt.Sprintf("user=postgres password=1234 host=%s port=5432 dbname=register sslmode=disable", host)
	db, err = sql.Open("postgres", connStr)
	if err != nil {
		log.Fatal("Database Connection Error:", err)
	}
	defer db.Close()

	// 2. เชื่อมต่อ RabbitMQ
	rabbitURL := os.Getenv("RABBITMQ_URL")
	if rabbitURL == "" {
		rabbitURL = "amqp://guest:guest@localhost:5672/"
	}

	// Retry connection สำหรับ RabbitMQ (เผื่อตอนสแตนด์บาย container)
	for i := 0; i < 5; i++ {
		rabbitConn, err = amqp.Dial(rabbitURL)
		if err == nil {
			break
		}
		log.Printf("RabbitMQ not ready, retrying in 5s... (%d/5)", i+1)
		time.Sleep(5 * time.Second)
	}
	if err != nil {
		log.Fatal("RabbitMQ Connection Error:", err)
	}
	defer rabbitConn.Close()

	rabbitChannel, err = rabbitConn.Channel()
	if err != nil {
		log.Fatal("RabbitMQ Channel Error:", err)
	}
	defer rabbitChannel.Close()

	_, err = rabbitChannel.QueueDeclare(queueName, true, false, false, false, nil)
	if err != nil {
		log.Fatal("Queue Declaration Error:", err)
	}

	// 3. ตั้งค่า Circuit Breaker
	cb = gobreaker.NewCircuitBreaker(gobreaker.Settings{
		Name:        "enrollment-breaker",
		MaxRequests: 3,
		Interval:    5 * time.Second,
		Timeout:     5 * time.Second,
		ReadyToTrip: func(counts gobreaker.Counts) bool {
			return counts.ConsecutiveFailures > 2
		},
		OnStateChange: func(name string, from, to gobreaker.State) {
			log.Printf("Circuit Breaker [%s]: เปลี่ยนสถานะจาก %s เป็น %s", name, from, to)
		},
	})

	// 4. เริ่มต้น Worker (Consumer)
	go startWorker()

	// 5. รัน Gin Web Server
	r := gin.Default()

	// ใช้งาน Prometheus Middleware
	r.Use(PrometheusMiddleware())
	r.GET("/metrics", gin.WrapH(promhttp.Handler()))

	r.GET("/health", healthHandler)
	r.POST("/enroll", enrollHandler)

	log.Println("Enrollment Service เริ่มทำงานที่พอร์ต :8002")
	r.Run(":8002")
}

// --- Handlers ---

func healthHandler(c *gin.Context) {
	if err := db.Ping(); err != nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"status": "unhealthy", "db_error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"status": "healthy"})
}

func enrollHandler(c *gin.Context) {
	var req EnrollmentRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	// ใช้ Circuit Breaker ตรวจสอบเงื่อนไขเบื้องต้นก่อนส่งเข้าคิว
	_, err := cb.Execute(func() (interface{}, error) {
		return canEnroll(db, req.StudentID, req.CourseIDs)
	})

	if err != nil {
		if err == gobreaker.ErrOpenState {
			c.JSON(http.StatusServiceUnavailable, gin.H{"error": "ระบบขัดข้องชั่วคราว (Circuit Breaker Open)"})
			return
		}
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	// ส่งข้อมูลเข้า RabbitMQ เพื่อประมวลผลต่อ
	body, _ := json.Marshal(req)
	err = rabbitChannel.PublishWithContext(context.Background(), "", queueName, false, false, amqp.Publishing{
		DeliveryMode: amqp.Persistent,
		ContentType:  "application/json",
		Body:         body,
	})

	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "ไม่สามารถส่งข้อมูลเข้าคิวได้"})
		return
	}

	c.JSON(http.StatusAccepted, gin.H{"message": "รับคำขอลงทะเบียนเรียบร้อยแล้ว กำลังดำเนินการ..."})
}

// --- Worker & Logic ---

func startWorker() {
	msgs, err := rabbitChannel.Consume(queueName, "", false, false, false, false, nil)
	if err != nil {
		log.Fatal(err)
	}

	for d := range msgs {
		var req EnrollmentRequest
		json.Unmarshal(d.Body, &req)

		log.Printf("Worker: กำลังประมวลผลการลงทะเบียน นักเรียนรหัส %d", req.StudentID)

		err := processTransaction(req)
		if err != nil {
			log.Printf("Worker Error: %v", err)
			d.Nack(false, true) // ส่งกลับเข้าคิวเพื่อลองใหม่
		} else {
			log.Printf("Worker Success: นักเรียนรหัส %d ลงทะเบียนสำเร็จ", req.StudentID)
			d.Ack(false)
		}
	}
}

func processTransaction(req EnrollmentRequest) error {
	courses, err := canEnroll(db, req.StudentID, req.CourseIDs)
	if err != nil {
		return err
	}

	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	for _, crs := range courses {
		_, err = tx.Exec("UPDATE course SET current_student = array_append(current_student, $1) WHERE course_id = $2", strconv.Itoa(req.StudentID), crs.ID)
		if err != nil {
			return err
		}

		if len(crs.CurrentStudents)+1 >= crs.Capacity {
			_, err = tx.Exec("UPDATE course SET state = 'closed' WHERE course_id = $1", crs.ID)
			if err != nil {
				return err
			}
		}
	}

	var exists bool
	tx.QueryRow("SELECT EXISTS(SELECT 1 FROM enrollment WHERE student_id = $1)", req.StudentID).Scan(&exists)

	if exists {
		_, err = tx.Exec("UPDATE enrollment SET course_id = array_cat(course_id, $1) WHERE student_id = $2", pq.Array(req.CourseIDs), req.StudentID)
	} else {
		_, err = tx.Exec("INSERT INTO enrollment (student_id, course_id) VALUES ($1, $2)", req.StudentID, pq.Array(req.CourseIDs))
	}

	if err != nil {
		return err
	}
	return tx.Commit()
}

func canEnroll(db *sql.DB, studentID int, ids []int) ([]CourseDB, error) {
	// จำลอง Logic การตรวจสอบ (ควร Query จริงจาก DB)
	var results []CourseDB
	for _, id := range ids {
		results = append(results, CourseDB{ID: id, Capacity: 30, State: "open"})
	}
	return results, nil
}
