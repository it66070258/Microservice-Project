package main

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/lib/pq"
	_ "github.com/lib/pq"
	amqp "github.com/rabbitmq/amqp091-go"
	"github.com/sony/gobreaker"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

func registerConsul(serviceName string, port int) {
	go func() {
		time.Sleep(5 * time.Second)
		reg := map[string]interface{}{
			"ID":      serviceName,
			"Name":    serviceName,
			"Address": serviceName,
			"Port":    port,
			"Check": map[string]interface{}{
				"HTTP":     fmt.Sprintf("http://%s:%d/metrics", serviceName, port),
				"Interval": "10s",
				"Timeout":  "5s",
			},
		}

		payload, _ := json.Marshal(reg)
		req, _ := http.NewRequest("PUT", "http://consul:8500/v1/agent/service/register", bytes.NewBuffer(payload))
		req.Header.Set("Content-Type", "application/json")

		client := &http.Client{}
		resp, err := client.Do(req)
		if err != nil {
			log.Println("Consul Registration Failed:", err)
			return
		}
		defer resp.Body.Close()
		log.Println("Consul Registration Success for", serviceName)
	}()
}

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

type EnrollmentRequest struct {
	StudentID int   `json:"student_id" binding:"required"`
	CourseIDs []int `json:"course_ids" binding:"required"`
}

type EnrollmentResponse struct {
	Success bool   `json:"success"`
	Message string `json:"message"`
	Error   string `json:"error,omitempty"`
}

type CourseDB struct {
	ID              int
	Credit          int
	Capacity        int
	CurrentStudents []string
	Prerequisite    []string
	DayOfWeek       string
	StartTime       time.Time
	EndTime         time.Time
	State           string
}

type DBConnections struct {
	ReadConn  *sql.DB
	WriteConn *sql.DB
}

func connectToReadDB() *sql.DB {
	host := os.Getenv("DB_READ_HOST")
	if host == "" {
		host = "localhost"
	}
	connStr := fmt.Sprintf("user=postgres password=1234 host=%s port=5432 dbname=register sslmode=disable", host)
	db, err := sql.Open("postgres", connStr)
	if err != nil {
		log.Fatal("Unable to connect to READ database:", err)
	}
	if err := db.Ping(); err != nil {
		log.Fatal("READ database ping failed:", err)
	}
	fmt.Println("Successfully connected to PostgreSQL READ database (Enrollment)!")
	return db
}

func connectToWriteDB() *sql.DB {
	host := os.Getenv("DB_WRITE_HOST")
	if host == "" {
		host = "localhost"
	}
	connStr := fmt.Sprintf("user=postgres password=1234 host=%s port=5432 dbname=register sslmode=disable", host)
	db, err := sql.Open("postgres", connStr)
	if err != nil {
		log.Fatal("Unable to connect to WRITE database:", err)
	}
	if err := db.Ping(); err != nil {
		log.Fatal("WRITE database ping failed:", err)
	}
	fmt.Println("Successfully connected to PostgreSQL WRITE database (Enrollment)!")
	return db
}

func connectToRabbitMQ() (*amqp.Connection, *amqp.Channel) {
	rabbitURL := os.Getenv("RABBITMQ_URL")
	if rabbitURL == "" {
		rabbitURL = "amqp://guest:guest@localhost:5672/"
	}

	var rabbitConn *amqp.Connection
	var err error
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

	rabbitChannel, err := rabbitConn.Channel()
	if err != nil {
		log.Fatal("RabbitMQ Channel Error:", err)
	}

	log.Println("Successfully connected to RabbitMQ (Enrollment Service)")
	return rabbitConn, rabbitChannel
}

// sendRPCRequestToCourse ส่ง RPC request ไปยัง course service และรอ response
func sendRPCRequestToCourse(rabbitChannel *amqp.Channel, req EnrollmentRequest, timeout time.Duration) (*EnrollmentResponse, error) {
	// สร้าง reply queue แบบชั่วคราว
	replyQueue, err := rabbitChannel.QueueDeclare(
		"",    // name (empty = auto-generated)
		false, // durable
		true,  // auto-delete
		true,  // exclusive
		false, // no-wait
		nil,   // args
	)
	if err != nil {
		return nil, fmt.Errorf("failed to declare reply queue: %v", err)
	}

	// รับข้อความจาก reply queue
	msgs, err := rabbitChannel.Consume(
		replyQueue.Name, // queue
		"",              // consumer
		true,            // auto-ack
		false,           // exclusive
		false,           // no-local
		false,           // no-wait
		nil,             // args
	)
	if err != nil {
		return nil, fmt.Errorf("failed to register consumer: %v", err)
	}

	// สร้าง correlation ID
	correlationID := fmt.Sprintf("%d-%d", req.StudentID, time.Now().UnixNano())

	// เตรียมข้อความ
	body, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %v", err)
	}

	// ส่ง request
	err = rabbitChannel.PublishWithContext(context.Background(),
		"",                          // exchange
		"course_enrollment_request", // routing key
		false,                       // mandatory
		false,                       // immediate
		amqp.Publishing{
			ContentType:   "application/json",
			CorrelationId: correlationID,
			ReplyTo:       replyQueue.Name,
			Body:          body,
		})
	if err != nil {
		return nil, fmt.Errorf("failed to publish request: %v", err)
	}

	log.Printf("RPC: Sent request to course service (CorrelationID: %s)", correlationID)

	// รอ response พร้อม timeout
	select {
	case d := <-msgs:
		if d.CorrelationId == correlationID {
			var response EnrollmentResponse
			err := json.Unmarshal(d.Body, &response)
			if err != nil {
				return nil, fmt.Errorf("failed to unmarshal response: %v", err)
			}
			log.Printf("RPC: Received response from course service: Success=%v", response.Success)
			return &response, nil
		}
		return nil, fmt.Errorf("received response with wrong correlation ID")
	case <-time.After(timeout):
		return nil, fmt.Errorf("timeout waiting for response from course service")
	}
}

func SetupRouter(dbConns *DBConnections, rabbitChannel *amqp.Channel) *gin.Engine {
	r := gin.Default()

	r.Use(PrometheusMiddleware())
	r.GET("/metrics", gin.WrapH(promhttp.Handler()))

	readSettings := gobreaker.Settings{
		Name:        "Database-Read-Operations",
		MaxRequests: 3,
		Interval:    time.Minute,
		Timeout:     30 * time.Second,
		ReadyToTrip: func(counts gobreaker.Counts) bool {
			failureRatio := float64(counts.TotalFailures) / float64(counts.Requests)
			return counts.Requests >= 5 && failureRatio >= 0.2
		},
	}
	readCircuitBreaker := gobreaker.NewCircuitBreaker(readSettings)

	r.POST("/enroll", func(c *gin.Context) {
		var req EnrollmentRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}

		// ตรวจสอบว่าสามารถลงทะเบียนได้หรือไม่
		_, err := readCircuitBreaker.Execute(func() (interface{}, error) {
			return canEnroll(dbConns.ReadConn, req.StudentID, req.CourseIDs)
		})

		if err != nil {
			if err == gobreaker.ErrOpenState {
				c.JSON(http.StatusServiceUnavailable, gin.H{"error": "ระบบขัดข้องชั่วคราว (Circuit Breaker Open)"})
				return
			}
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}

		// ลองส่ง request และ retry หากล้มเหลว
		maxRetries := 3
		var lastErr error

		for retry := 0; retry < maxRetries; retry++ {
			if retry > 0 {
				log.Printf("Retry %d/%d for student %d", retry, maxRetries, req.StudentID)
				time.Sleep(time.Duration(retry) * 2 * time.Second) // exponential backoff
			}

			// เริ่ม transaction
			tx, err := dbConns.WriteConn.Begin()
			if err != nil {
				lastErr = fmt.Errorf("failed to start transaction: %v", err)
				continue
			}

			// บันทึกข้อมูลลงทะเบียนใน enrollment table
			var exists bool
			err = tx.QueryRow("SELECT EXISTS(SELECT 1 FROM enrollment WHERE student_id = $1)", req.StudentID).Scan(&exists)
			if err != nil {
				tx.Rollback()
				lastErr = fmt.Errorf("failed to check enrollment existence: %v", err)
				continue
			}

			if exists {
				_, err = tx.Exec("UPDATE enrollment SET course_id = array_cat(course_id, $1) WHERE student_id = $2", pq.Array(req.CourseIDs), req.StudentID)
			} else {
				_, err = tx.Exec("INSERT INTO enrollment (student_id, course_id) VALUES ($1, $2)", req.StudentID, pq.Array(req.CourseIDs))
			}

			if err != nil {
				tx.Rollback()
				lastErr = fmt.Errorf("failed to insert enrollment: %v", err)
				continue
			}

			// ส่ง RPC request ไปยัง course service
			response, err := sendRPCRequestToCourse(rabbitChannel, req, 10*time.Second)

			if err != nil {
				// Rollback transaction เพราะ course service ไม่ตอบกลับ
				tx.Rollback()
				lastErr = fmt.Errorf("course service error: %v", err)
				log.Printf("Transaction rolled back: %v", err)
				continue
			}

			if !response.Success {
				// Rollback เพราะ course service ตอบว่าไม่สำเร็จ
				tx.Rollback()
				lastErr = fmt.Errorf("course service failed: %s", response.Error)
				log.Printf("Transaction rolled back: %s", response.Error)
				continue
			}

			// Commit transaction เพราะทุกอย่างสำเร็จ
			err = tx.Commit()
			if err != nil {
				lastErr = fmt.Errorf("failed to commit transaction: %v", err)
				continue
			}

			log.Printf("Successfully enrolled student %d in courses %v", req.StudentID, req.CourseIDs)
			c.JSON(http.StatusOK, gin.H{
				"message": "ลงทะเบียนสำเร็จ",
				"details": response.Message,
			})
			return
		}

		// หากลองทั้งหมดแล้วยังไม่สำเร็จ
		log.Printf("Failed to enroll student %d after %d retries: %v", req.StudentID, maxRetries, lastErr)
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": fmt.Sprintf("ไม่สามารถลงทะเบียนได้หลังจากลอง %d ครั้ง: %v", maxRetries, lastErr),
		})
	})

	return r
}

func canEnroll(db *sql.DB, studentID int, ids []int) ([]CourseDB, error) {
	if len(ids) == 0 {
		return nil, fmt.Errorf("ไม่มีรายวิชาที่ต้องลงทะเบียน")
	}

	// 1. ดึงข้อมูลนักเรียนเพื่อตรวจสอบวิชาที่ผ่านแล้ว (Prerequisite)
	var gradedSubjects pq.StringArray
	err := db.QueryRow("SELECT COALESCE(graded_subject, '{}'::text[]) FROM student WHERE student_id = $1", studentID).Scan(pq.Array(&gradedSubjects))
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, fmt.Errorf("ไม่พบข้อมูลนักเรียนรหัส %d ในระบบ", studentID)
		}
		return nil, fmt.Errorf("เกิดข้อผิดพลาดในการดึงข้อมูลนักเรียน: %v", err)
	}

	gradedMap := make(map[string]bool)
	for _, sub := range gradedSubjects {
		gradedMap[sub] = true
	}

	// 2. ดึงข้อมูลวิชาที่ร้องขอลงทะเบียนใหม่ (ตรวจสอบ Capacity / State / Prerequisite)
	var newCourses []CourseDB
	rows, err := db.Query(`SELECT course_id, credit, capacity, COALESCE(current_student, '{}'::text[]), COALESCE(prerequisite, '{}'::text[]), day_of_week, start_time, end_time, state 
		FROM course WHERE course_id = ANY($1)`, pq.Array(ids))
	if err != nil {
		return nil, fmt.Errorf("เกิดข้อผิดพลาดในการดึงข้อมูลรายวิชา: %v", err)
	}
	defer rows.Close()

	uniqueCheck := make(map[int]bool)
	totalNewCredit := 0

	for rows.Next() {
		var c CourseDB
		var currentStudents, prerequisites pq.StringArray
		if err := rows.Scan(&c.ID, &c.Credit, &c.Capacity, &currentStudents, &prerequisites, &c.DayOfWeek, &c.StartTime, &c.EndTime, &c.State); err != nil {
			return nil, fmt.Errorf("เกิดข้อผิดพลาดในการอ่านข้อมูลวิชา: %v", err)
		}
		c.CurrentStudents = currentStudents
		c.Prerequisite = prerequisites

		// ป้องกันการส่งรายวิชาเดิมเบิ้ลมาใน Request เดียวกัน
		if uniqueCheck[c.ID] {
			return nil, fmt.Errorf("ไม่อนุญาตให้ระบุวิชารหัส %d ซ้ำกันในคำขอเดียว", c.ID)
		}
		uniqueCheck[c.ID] = true

		if c.State == "closed" {
			return nil, fmt.Errorf("วิชารหัส %d ปิดรับลงทะเบียนแล้ว (State: Closed)", c.ID)
		}
		if len(c.CurrentStudents) >= c.Capacity {
			return nil, fmt.Errorf("วิชารหัส %d ที่นั่งเต็มแล้ว (%d/%d)", c.ID, len(c.CurrentStudents), c.Capacity)
		}
		for _, reqSub := range c.Prerequisite {
			if !gradedMap[reqSub] {
				return nil, fmt.Errorf("นักเรียนยังไม่ผ่านวิชาบังคับก่อนหน้า (%s) สำหรับวิชารหัส %d", reqSub, c.ID)
			}
		}

		newCourses = append(newCourses, c)
		totalNewCredit += c.Credit
	}

	if len(newCourses) != len(ids) {
		return nil, fmt.Errorf("มีรายวิชาที่ระบุไม่ถูกต้องหรือไม่มีอยู่จริงในระบบ")
	}

	// 3. ดึงประวัติที่ลงไปแล้วของนักเรียน เพื่อเช็คหน่วยกิตรวม, เวลาชน, ป้องกันการลงวิชาเดิมซ้ำ
	var existingCourseIDsInt64 []int64
	err = db.QueryRow("SELECT COALESCE(course_id, '{}'::int[]) FROM enrollment WHERE student_id = $1", studentID).Scan(pq.Array(&existingCourseIDsInt64))
	if err != nil && err != sql.ErrNoRows {
		return nil, fmt.Errorf("เกิดข้อผิดพลาดในการดึงประวัติการลงทะเบียน: %v", err)
	}

	var existingCourses []CourseDB
	totalExistingCredit := 0

	if len(existingCourseIDsInt64) > 0 {
		eRows, err := db.Query(`SELECT course_id, credit, day_of_week, start_time, end_time 
			FROM course WHERE course_id = ANY($1)`, pq.Array(existingCourseIDsInt64))
		if err != nil {
			return nil, fmt.Errorf("เกิดข้อผิดพลาดในการดึงข้อมูลวิชาที่เคยลง: %v", err)
		}
		defer eRows.Close()

		for eRows.Next() {
			var c CourseDB
			if err := eRows.Scan(&c.ID, &c.Credit, &c.DayOfWeek, &c.StartTime, &c.EndTime); err != nil {
				return nil, fmt.Errorf("เกิดข้อผิดพลาดในการอ่านข้อมูลวิชาที่เคยลง: %v", err)
			}

			// เช็คว่าวิชาที่ขอใหม่ ไปซ้ำกับวิชาที่เคยมีในตารางแล้วหรือไม่
			if uniqueCheck[c.ID] {
				return nil, fmt.Errorf("วิชารหัส %d เคยได้รับการลงทะเบียนและบันทึกไว้ในระบบแล้ว", c.ID)
			}

			existingCourses = append(existingCourses, c)
			totalExistingCredit += c.Credit
		}
	}

	// 4. ตรวจสอบเงื่อนไขลงทะเบียนเกิน 21 หน่วยกิต
	if totalNewCredit+totalExistingCredit > 21 {
		return nil, fmt.Errorf("หน่วยกิตการลงทะเบียนรวมเกิน 21 (ปัจจุบันมี %d หน่วยกิต, ขอเพิ่มใหม่ %d หน่วยกิต)", totalExistingCredit, totalNewCredit)
	}

	// 5. ตรวจสอบการทับซ้อนของตารางเรียน (Schedule Overlap) ระหว่างวิชาเดิมและวิชาใหม่
	allClasses := append(existingCourses, newCourses...)
	for i := 0; i < len(allClasses); i++ {
		for j := i + 1; j < len(allClasses); j++ {
			c1 := allClasses[i]
			c2 := allClasses[j]

			if c1.DayOfWeek != "" && c1.DayOfWeek == c2.DayOfWeek {
				// แปลงเวลาให้เป็นรูปแบบ HH:MM:SS เพื่อการเปรียบเทียบ string ปกติ (ปลอดภัยสำหรับเวลา 24 ชั่วโมง)
				s1 := c1.StartTime.Format("15:04:05")
				e1 := c1.EndTime.Format("15:04:05")
				s2 := c2.StartTime.Format("15:04:05")
				e2 := c2.EndTime.Format("15:04:05")

				// เงื่อนไขเวลาครอบเกี่ยวกัน (Start1 < End2 และ End1 > Start2)
				if s1 < e2 && e1 > s2 {
					return nil, fmt.Errorf("เวลาเรียนทับซ้อนกันวัน %s: วิชารหัส %d (%s-%s) ชนกับ วิชารหัส %d (%s-%s)",
						c1.DayOfWeek, c1.ID, s1, e1, c2.ID, s2, e2)
				}
			}
		}
	}

	return newCourses, nil
}

func main() {
	registerConsul("enrollment-service", 8002)

	readConn := connectToReadDB()
	writeConn := connectToWriteDB()
	defer readConn.Close()
	defer writeConn.Close()

	dbConns := &DBConnections{
		ReadConn:  readConn,
		WriteConn: writeConn,
	}

	rabbitConn, rabbitChannel := connectToRabbitMQ()
	defer rabbitConn.Close()
	defer rabbitChannel.Close()

	r := SetupRouter(dbConns, rabbitChannel)

	log.Println("Enrollment Service เริ่มทำงานที่พอร์ต :8002")
	r.Run(":8002")
}
